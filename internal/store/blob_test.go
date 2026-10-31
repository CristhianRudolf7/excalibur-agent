package store_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestBlobRoundTrip stores, stats, opens and refcounts one blob.
func TestBlobRoundTrip(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	blobs := db.Blob()

	payload := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 1000)
	digest, err := blobs.Put(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 {
		t.Fatalf("digest = %q", digest)
	}
	// Deterministic: same bytes, same digest.
	again, err := blobs.Put(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if again != digest {
		t.Fatal("content addressing is not deterministic")
	}
	info, err := blobs.Stat(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", info.Size, len(payload))
	}
	rc, err := blobs.Open(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatal("blob bytes corrupted")
	}
	if err := blobs.AddRef(ctx, digest); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Release(ctx, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Open(ctx, strings.Repeat("0", 64)); !errors.Is(err, store.ErrBlobNotFound) {
		t.Fatalf("missing open err = %v", err)
	}
}

// TestBlobGC collects only unreferenced blobs older than the cutoff
// (Section 4.2: daily GC of blobs unreferenced for over 24h).
func TestBlobGC(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	blobs := db.Blob()

	old, err := blobs.Put(ctx, strings.NewReader("old and lonely"))
	if err != nil {
		t.Fatal(err)
	}
	kept, err := blobs.Put(ctx, strings.NewReader("referenced"))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := blobs.Put(ctx, strings.NewReader("fresh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := blobs.AddRef(ctx, kept); err != nil {
		t.Fatal(err)
	}
	// Age the rows: only `old` qualifies.
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "UPDATE blob_refs SET created_at=? WHERE digest=?",
			time.Now().Add(-48*time.Hour).UnixMilli(), old)
		return err
	})

	freed, err := blobs.CollectGarbage(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if freed == 0 {
		t.Fatal("GC reclaimed nothing")
	}
	if _, err := blobs.Open(ctx, old); !errors.Is(err, store.ErrBlobNotFound) {
		t.Fatalf("old blob survived GC: %v", err)
	}
	for _, d := range []string{kept, fresh} {
		rc, err := blobs.Open(ctx, d)
		if err != nil {
			t.Fatalf("live blob %s lost: %v", d, err)
		}
		_ = rc.Close()
	}
	// Rows follow files: no row points at a missing blob.
	if err := db.Read(ctx, func(_ context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, "SELECT digest FROM blob_refs")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				return err
			}
			rc, err := blobs.Open(ctx, d)
			if err != nil {
				return err
			}
			_ = rc.Close()
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
}
