package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MaxBlobBytes caps one blob upload: 64 MiB, enough for captures and
// tool outputs without letting one client exhaust the disk.
const MaxBlobBytes = 64 << 20

// BlobStore is a content-addressed blob store (Section 4.2): the
// digest (hex sha256) addresses the bytes. Writes are atomic
// (temp + fsync + rename) before any row references the blob.
type BlobStore interface {
	Put(digest string, r io.Reader) error
	Get(digest string) (io.ReadCloser, int64, error)
	Stat(digest string) (int64, bool)
}

// MemoryBlobs is an in-process BlobStore for tests.
type MemoryBlobs struct {
	mu   sync.Mutex
	data map[string][]byte
}

// NewMemoryBlobs returns an empty blob store.
func NewMemoryBlobs() *MemoryBlobs { return &MemoryBlobs{data: map[string][]byte{}} }

// Put implements BlobStore: the content must hash to digest.
func (m *MemoryBlobs) Put(digest string, r io.Reader) error {
	buf, err := io.ReadAll(io.LimitReader(r, MaxBlobBytes+1))
	if err != nil {
		return err
	}
	if len(buf) > MaxBlobBytes {
		return fmt.Errorf("gateway: blob exceeds %d bytes", MaxBlobBytes)
	}
	sum := sha256.Sum256(buf)
	if hex.EncodeToString(sum[:]) != strings.ToLower(digest) {
		return fmt.Errorf("gateway: digest mismatch")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[strings.ToLower(digest)] = buf
	return nil
}

// Get implements BlobStore.
func (m *MemoryBlobs) Get(digest string) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf, ok := m.data[strings.ToLower(digest)]
	if !ok {
		return nil, 0, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(string(buf))), int64(len(buf)), nil
}

// Stat implements BlobStore.
func (m *MemoryBlobs) Stat(digest string) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf, ok := m.data[strings.ToLower(digest)]
	return int64(len(buf)), ok
}

// DirBlobs is the filesystem BlobStore: blobs/dir with sharded
// sha256/ab/cd… layout, atomic temp + rename writes (Section 4.2).
type DirBlobs struct {
	dir string
}

// NewDirBlobs opens dir, creating it when missing.
func NewDirBlobs(dir string) (*DirBlobs, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gateway: mkdir blobs: %w", err)
	}
	return &DirBlobs{dir: dir}, nil
}

func validDigest(d string) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func (d *DirBlobs) path(digest string) string {
	digest = strings.ToLower(digest)
	return filepath.Join(d.dir, digest[:2], digest[2:4], digest)
}

// Put implements BlobStore with an atomic write and digest check.
func (d *DirBlobs) Put(digest string, r io.Reader) error {
	digest = strings.ToLower(digest)
	if !validDigest(digest) {
		return fmt.Errorf("gateway: bad digest %q", digest)
	}
	tmp, err := os.CreateTemp(d.dir, "blob-*")
	if err != nil {
		return fmt.Errorf("gateway: temp blob: %w", err)
	}
	tmpName := tmp.Name()
	h := sha256.New()
	n, err := io.Copy(tmp, io.TeeReader(io.LimitReader(r, MaxBlobBytes+1), h))
	cerr := tmp.Close()
	if err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("gateway: write blob: %w", err)
	}
	if cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("gateway: close blob: %w", cerr)
	}
	if n > MaxBlobBytes {
		_ = os.Remove(tmpName)
		return fmt.Errorf("gateway: blob exceeds %d bytes", MaxBlobBytes)
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		_ = os.Remove(tmpName)
		return fmt.Errorf("gateway: digest mismatch")
	}
	dst := d.path(digest)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("gateway: mkdir shard: %w", err)
	}
	// Atomic publish before any row may reference the blob.
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("gateway: publish blob: %w", err)
	}
	return nil
}

// Get implements BlobStore.
func (d *DirBlobs) Get(digest string) (io.ReadCloser, int64, error) {
	digest = strings.ToLower(digest)
	if !validDigest(digest) {
		return nil, 0, fmt.Errorf("gateway: bad digest %q", digest)
	}
	f, err := os.Open(d.path(digest))
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// Stat implements BlobStore.
func (d *DirBlobs) Stat(digest string) (int64, bool) {
	digest = strings.ToLower(digest)
	if !validDigest(digest) {
		return 0, false
	}
	st, err := os.Stat(d.path(digest))
	if err != nil {
		return 0, false
	}
	return st.Size(), true
}

// serveBlobs handles PUT /blobs/{digest} and GET /blobs/{digest}.
// Digest mismatches are 400; missing blobs are 404.
func (s *Server) serveBlobs(w http.ResponseWriter, r *http.Request) {
	digest := strings.TrimPrefix(r.URL.Path, "/blobs/")
	digest = strings.ToLower(digest)
	if !validDigest(digest) {
		http.Error(w, "bad digest", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut, http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, MaxBlobBytes+1)
		if err := s.blobs.Put(digest, r.Body); err != nil {
			if strings.Contains(err.Error(), "digest mismatch") {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if strings.Contains(err.Error(), "exceeds") {
				http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	case http.MethodGet, http.MethodHead:
		rc, size, err := s.blobs.Get(digest)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer func() { _ = rc.Close() }()
		contentType := r.URL.Query().Get("mime")
		if contentType == "" && s.cfg.BlobMIME != nil {
			contentType, _ = s.cfg.BlobMIME(r.Context(), digest)
		}
		mediaType, _, mimeErr := mime.ParseMediaType(contentType)
		if mimeErr != nil || !strings.Contains(mediaType, "/") {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if mediaType == "application/pdf" {
			w.Header().Set("Content-Disposition", "inline")
		}
		w.Header().Set("Content-Length", fmt.Sprint(size))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.Copy(w, rc)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// uploadBlob accepts binary content and returns its calculated reference.
// MIME is supplied again to voice.transcribe and to GET /blobs/{ref}?mime=...
func (s *Server) uploadBlob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBlobBytes))
	if err != nil {
		http.Error(w, "blob upload failed or exceeds limit", http.StatusRequestEntityTooLarge)
		return
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err := s.blobs.Put(digest, bytes.NewReader(data)); err != nil {
		http.Error(w, "blob storage failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"audio_blob": digest, "blob_digest": digest})
}
