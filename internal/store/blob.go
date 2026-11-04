package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"

	"github.com/klauspost/compress/zstd"
)

// optsBlobDir defaults the blob directory beside the database file.
func optsBlobDir(dbPath string, opts Options) string {
	if opts.BlobDir != "" {
		return opts.BlobDir
	}
	if opts.InMemory {
		dir, err := os.MkdirTemp("", "excalibur-blobs-")
		if err != nil {
			return filepath.Join(os.TempDir(), "excalibur-blobs")
		}
		return dir
	}
	return filepath.Join(filepath.Dir(dbPath), "blobs")
}

// blobStore is the Section 4.2 store: content-addressed
// (sha256/ab/cd...), zstd-compressed, crash-safe ordered (atomic
// temp + fsync + rename BEFORE any row may reference the blob),
// with counted references and GC of blobs unreferenced for over 24h.
//
// Ordering rule: Put persists the file first and records the
// blob_refs row afterwards; rows never point at missing blobs even
// across a power cut. Collection deletes the row first and the file
// afterwards; a crash between the two leaves a file without a row,
// which the next collection pass reaps.
type blobStore struct {
	dir string
	db  *DB
}

var _ BlobStore = (*blobStore)(nil)

func newBlobStore(dir string, db *DB) *blobStore {
	return &blobStore{dir: dir, db: db}
}

// blobPath maps digest hex to ab/cd...rest.
func (b *blobStore) blobPath(digest string) string {
	return filepath.Join(b.dir, digest[:2], digest[2:4], digest[4:])
}

// Put stores data and returns its hex digest. The file lands via
// temp + fsync + rename before Put returns, so a later row insert
// can never dangle; the blob_refs row is recorded through the
// porter in the same call.
func (b *blobStore) Put(ctx context.Context, r io.Reader) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])

	dst := b.blobPath(digest)
	if _, err := os.Stat(dst); err == nil {
		// Already stored; still ensure the row exists.
		_ = b.upsertRow(ctx, digest, int64(len(raw)), fileSize(dst))
		return digest, nil
	}
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		return "", err
	}
	if _, err := enc.Write(raw); err != nil {
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	compressed := buf.Bytes()

	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	tmp, err := tempFile(filepath.Dir(dst))
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(compressed); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := platform.SyncDir(filepath.Dir(dst)); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := platform.SyncDir(filepath.Dir(dst)); err != nil {
		return "", err
	}
	if err := b.upsertRow(ctx, digest, int64(len(raw)), int64(len(compressed))); err != nil {
		return "", err
	}
	return digest, nil
}

func (b *blobStore) upsertRow(ctx context.Context, digest string, size, stored int64) error {
	return b.db.Write(ctx, PriorityNormal, func(ctx context.Context, ex Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO blob_refs(digest,refcount,size,stored_size,created_at)
			VALUES(?,?,?,?,strftime('%s','now')*1000)
			ON CONFLICT(digest) DO UPDATE SET size=excluded.size, stored_size=excluded.stored_size`,
			digest, 0, size, stored)
		return err
	})
}

// fileSize stats a path, returning 0 on error.
func fileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

// Open returns the blob bytes for digest.
func (b *blobStore) Open(_ context.Context, digest string) (io.ReadCloser, error) {
	if !validDigest(digest) {
		return nil, fmt.Errorf("%w: %q", ErrBlobNotFound, digest)
	}
	f, err := os.Open(b.blobPath(digest))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrBlobNotFound, digest)
		}
		return nil, err
	}
	dec, err := zstd.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &decReadCloser{dec: dec, file: f}, nil
}

// Stat describes a stored blob (Size is the uncompressed size).
func (b *blobStore) Stat(ctx context.Context, digest string) (BlobInfo, error) {
	if !validDigest(digest) {
		return BlobInfo{}, fmt.Errorf("%w: %q", ErrBlobNotFound, digest)
	}
	var info BlobInfo
	err := b.db.Read(ctx, func(ctx context.Context, q Queryer) error {
		return q.QueryRow(ctx, "SELECT digest,size FROM blob_refs WHERE digest=?", digest).Scan(&info.Digest, &info.Size)
	})
	if err != nil {
		// Fall back to the filesystem: the row may postdate the
		// file after a crash between Put's rename and row insert.
		if _, serr := os.Stat(b.blobPath(digest)); serr != nil {
			return BlobInfo{}, fmt.Errorf("%w: %s", ErrBlobNotFound, digest)
		}
		return BlobInfo{}, fmt.Errorf("store: blob %s stored but unregistered", digest)
	}
	if _, err := os.Stat(b.blobPath(digest)); err != nil {
		return BlobInfo{}, fmt.Errorf("%w: %s", ErrBlobNotFound, digest)
	}
	return info, nil
}

// AddRef records one more reference to digest.
func (b *blobStore) AddRef(ctx context.Context, digest string) error {
	if !validDigest(digest) {
		return fmt.Errorf("%w: %q", ErrBlobNotFound, digest)
	}
	return b.db.Write(ctx, PriorityNormal, func(_ context.Context, ex Execer) error {
		res, err := ex.Exec(ctx, "UPDATE blob_refs SET refcount=refcount+1 WHERE digest=?", digest)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("%w: %s", ErrBlobNotFound, digest)
		}
		return nil
	})
}

// Release drops one reference to digest.
func (b *blobStore) Release(ctx context.Context, digest string) error {
	if !validDigest(digest) {
		return fmt.Errorf("%w: %q", ErrBlobNotFound, digest)
	}
	return b.db.Write(ctx, PriorityNormal, func(_ context.Context, ex Execer) error {
		res, err := ex.Exec(ctx, "UPDATE blob_refs SET refcount=CASE WHEN refcount>0 THEN refcount-1 ELSE 0 END WHERE digest=?", digest)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("%w: %s", ErrBlobNotFound, digest)
		}
		return nil
	})
}

// CollectGarbage deletes files whose row is missing or unreferenced
// and older than olderThan, row first then file so no row ever
// points at a missing blob. It reports reclaimed bytes (Section 4.2:
// daily collection of blobs unreferenced for over 24h).
func (b *blobStore) CollectGarbage(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).UnixMilli()
	var victims []struct {
		digest string
		size   int64
	}
	err := b.db.Write(ctx, PriorityBackground, func(_ context.Context, ex Execer) error {
		rows, err := ex.Query(ctx, "SELECT digest,size FROM blob_refs WHERE refcount<=0 AND created_at<?", cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		victims = nil
		for rows.Next() {
			var v struct {
				digest string
				size   int64
			}
			if err := rows.Scan(&v.digest, &v.size); err != nil {
				return err
			}
			victims = append(victims, v)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, v := range victims {
			if _, err := ex.Exec(ctx, "DELETE FROM blob_refs WHERE digest=?", v.digest); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	var freed int64
	for _, v := range victims {
		p := b.blobPath(v.digest)
		if st, serr := os.Stat(p); serr == nil {
			freed += st.Size()
		} else {
			freed += v.size
		}
		_ = os.Remove(p)
	}
	// Sweep orphans: files without any row (a crash may strand them
	// between Put's rename and its row insert, or a delete may have
	// landed between the row delete above and the file remove).
	if freed2, err := b.sweepOrphans(cutoff); err == nil {
		freed += freed2
	}
	_ = pruneEmptyDirs(b.dir)
	return freed, nil
}

// sweepOrphans removes files older than cutoff with no blob_refs row.
func (b *blobStore) sweepOrphans(cutoff int64) (int64, error) {
	known := map[string]bool{}
	err := b.db.Read(context.Background(), func(_ context.Context, q Queryer) error {
		rows, err := q.Query(context.Background(), "SELECT digest FROM blob_refs")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				return err
			}
			known[d] = true
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	var freed int64
	_ = filepath.Walk(b.dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.ModTime().UnixMilli() >= cutoff {
			return nil
		}
		rel, err := filepath.Rel(b.dir, p)
		if err != nil {
			return nil
		}
		digest := unpackDigestPath(rel)
		if digest == "" || known[digest] {
			return nil
		}
		freed += info.Size()
		_ = os.Remove(p)
		return nil
	})
	return freed, nil
}

// validDigest checks lowercase hex sha256 shape.
func validDigest(d string) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// unpackDigestPath reverses blobPath for the GC sweep.
func unpackDigestPath(rel string) string {
	parts := filepath.SplitList(rel)
	_ = parts
	segs := splitPath(rel)
	if len(segs) != 3 || len(segs[0]) != 2 || len(segs[1]) != 2 {
		return ""
	}
	d := segs[0] + segs[1] + segs[2]
	if !validDigest(d) {
		return ""
	}
	return d
}

func splitPath(p string) []string {
	var out []string
	for {
		dir, base := filepath.Split(p)
		if base != "" {
			out = append([]string{base}, out...)
		}
		if dir == "" || dir == "/" {
			break
		}
		p = filepath.Clean(dir)
		if p == "." {
			break
		}
	}
	return out
}

// tempFile creates a uniquely named temp file beside the target.
func tempFile(dir string) (*os.File, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return nil, err
	}
	for i := 0; i < 10; i++ {
		name := filepath.Join(dir, fmt.Sprintf(".tmp-%x", buf))
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return f, nil
		}
		if _, err := rand.Read(buf[:]); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("store: temp file: too many collisions")
}

// cleanTemps removes .tmp-* crash leftovers from interrupted Puts.
// Temp files are never referenced by rows, so deleting them on open
// is always safe.
func (b *blobStore) cleanTemps() {
	_ = filepath.Walk(b.dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasPrefix(filepath.Base(p), ".tmp-") {
			_ = os.Remove(p)
		}
		return nil
	})
}
func pruneEmptyDirs(root string) error {
	for _, a := range listDirs(root) {
		for _, b := range listDirs(a) {
			_ = os.Remove(b)
		}
		_ = os.Remove(a)
	}
	return nil
}

func listDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// decReadCloser chains the zstd decoder and its file.
type decReadCloser struct {
	dec  *zstd.Decoder
	file *os.File
}

func (d *decReadCloser) Read(p []byte) (int, error) { return d.dec.Read(p) }
func (d *decReadCloser) Close() error {
	d.dec.Close()
	return d.file.Close()
}
