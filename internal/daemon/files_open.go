package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

func (b *DaemonBackend) filesOpen(ctx context.Context, raw json.RawMessage) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var p api.FilesOpenParams
	if err := strictLibraryParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Path) == "" {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "path is required"}
	}
	var roots []string
	if s := b.Services; s != nil {
		if s.Projects != nil {
			if p.ProjectID != "" {
				proj, err := s.Projects.Get(types.ProjectID(p.ProjectID))
				if err != nil {
					return nil, &api.Error{Code: api.CodeUnknownTarget, Message: "unknown project"}
				}
				roots = append(roots, proj.Roots...)
			} else {
				for _, proj := range s.Projects.List() {
					roots = append(roots, proj.Roots...)
				}
			}
		}
		if p.ProjectID == "" && s.WorkspaceRoots != nil {
			roots = append(roots, s.WorkspaceRoots(ctx)...)
		}
	}
	path, root, err := resolveWorkspaceFile(p.Path, roots)
	if err != nil {
		return nil, err
	}
	// Reject special files before opening (a FIFO open could block indefinitely).
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "file must be regular and at most 64 MiB"}
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	// Keep containment enforced while opening, even if a symlink changes after
	// canonicalization. The returned descriptor, rather than a second path read,
	// supplies both inline content and the library original.
	f, err := os.OpenInRoot(root, rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > gateway.MaxBlobBytes {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "file must be regular and at most 64 MiB"}
	}
	data, err := io.ReadAll(io.LimitReader(f, gateway.MaxBlobBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > gateway.MaxBlobBytes {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "file exceeds 64 MiB"}
	}
	mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mt == "" {
		mt = http.DetectContentType(data)
	}
	out := api.FilesOpenResult{Path: path, MIME: mt, Kind: "binary", Size: int64(len(data))}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv", ".tsv", ".xlsx", ".xls", ".ods":
		out.Kind = "sheet"
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".odt", ".odp", ".epub":
		out.Kind = "document"
	default:
		if strings.HasPrefix(mt, "image/") {
			out.Kind = "image"
		} else if len(data) <= 2<<20 && workspace.IsText(data) {
			out.Kind = "text"
			content := string(data)
			out.Content = &content
		}
	}
	if out.Kind == "text" || out.Kind == "binary" {
		return out, nil
	}
	if b.Services == nil || b.Services.FileBlobs == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "file blob store unavailable"}
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err := b.Services.FileBlobs.Put(digest, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	out.BlobURL = "/blobs/" + digest
	if out.Kind == "image" {
		return out, nil
	}
	r := b.Services.Library
	params, _ := json.Marshal(api.LibraryIngestParams{BlobDigest: digest, Name: filepath.Base(path), MIME: mt})
	result, err := r.Op(ctx, api.MethodLibraryIngest, params)
	if err != nil {
		return nil, err
	}
	doc := result.(api.LibraryIngestResult).Document
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for doc.Status != "ready" && doc.Status != "partial" {
		if doc.Status == "failed" || doc.Status == "canceled" {
			return nil, &api.Error{Code: api.CodeConflict, Message: "document extraction failed: " + doc.Error}
		}
		select {
		case <-ctx.Done():
			return nil, libraryError(ctx.Err())
		case <-ticker.C:
		}
		doc, err = r.document(ctx, doc.ID)
		if err != nil {
			return nil, libraryError(err)
		}
	}
	out.DocumentID = doc.ID
	return out, nil
}

func resolveWorkspaceFile(path string, roots []string) (string, string, error) {
	for _, root := range roots {
		root, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		root, err = filepath.Abs(root)
		if err != nil {
			continue
		}
		candidate := path
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		candidate, err = filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, candidate)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return candidate, root, nil
		}
	}
	return "", "", &api.Error{Code: api.CodeInvalidParams, Message: "outside workspace"}
}
