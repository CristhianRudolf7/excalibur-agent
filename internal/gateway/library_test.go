package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func TestDaemonBlobStorageAndMIME(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"), store.Options{}, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := DefaultConfig()
	cfg.AllowRemote = true
	cfg.DaemonBlobs = StoreBlobs{Store: db.Blob()}
	cfg.BlobMIME = func(context.Context, string) (string, error) { return "application/pdf", nil }
	separate := NewMemoryBlobs()
	s := New(cfg, nil, separate, nil)
	data := []byte("%PDF-1.7 test original")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("PUT", "/blobs/"+digest, bytes.NewReader(data)))
	if w.Code != 201 {
		t.Fatalf("upload %d: %s", w.Code, w.Body.String())
	}
	if _, ok := separate.Stat(digest); ok {
		t.Fatal("upload used separate gateway store")
	}
	r, err := db.Blob().Open(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("daemon read %q %v", got, err)
	}
	for _, tc := range []struct{ suffix, want string }{{"", "application/pdf"}, {"?mime=text/plain", "text/plain"}, {"?mime=invalid", "application/octet-stream"}} {
		w = httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "/blobs/"+digest+tc.suffix, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != tc.want || !bytes.Equal(w.Body.Bytes(), data) {
			t.Fatalf("download %d %v %q", w.Code, w.Header(), w.Body.String())
		}
		if tc.want == "application/pdf" && w.Header().Get("Content-Disposition") != "inline" {
			t.Fatal("PDF not inline")
		}
	}
	wrong := fmt.Sprintf("%x", sha256.Sum256([]byte("wrong")))
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("PUT", "/blobs/"+wrong, bytes.NewReader(data)))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	if _, err := db.Blob().Stat(ctx, wrong); err == nil {
		t.Fatal("invalid digest persisted")
	}
}
