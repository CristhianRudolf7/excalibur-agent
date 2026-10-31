package fake

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func TestFakeStoreWriteRead(t *testing.T) {
	s := NewFakeStore()
	var _ store.Store = s

	err := s.Write(context.Background(), store.PriorityInteractive, func(_ context.Context, ex store.Execer) error {
		if _, err := ex.Exec(context.Background(), "INSERT INTO t VALUES (?)", 1); err != nil {
			return err
		}
		rows, err := ex.Query(context.Background(), "SELECT a FROM t")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	qs := s.Execer.Recorded()
	if len(qs) != 2 || !strings.HasPrefix(qs[0].Query, "INSERT") {
		t.Fatalf("unexpected recorded queries: %+v", qs)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Write(ctx, store.PriorityBackground, func(context.Context, store.Execer) error { return nil }); err != store.ErrWriteCanceled {
		t.Fatalf("expected ErrWriteCanceled, got %v", err)
	}

	s.Execer.RowMap["SELECT 1"] = FakeRow{Values: []any{7}}
	if err := s.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		row := q.QueryRow(context.Background(), "SELECT 1")
		var n int
		return row.Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil || !s.Closed() {
		t.Fatal("close")
	}
}

func TestFakeBlobStore(t *testing.T) {
	s := NewFakeStore()
	ctx := context.Background()
	digest, err := s.Blob().Put(ctx, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Blob().Stat(ctx, digest)
	if err != nil || info.Size != 5 {
		t.Fatalf("stat: %+v %v", info, err)
	}
	rc, err := s.Blob().Open(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if err := s.Blob().AddRef(ctx, digest); err != nil {
		t.Fatal(err)
	}
	freed, err := s.Blob().CollectGarbage(ctx, 0)
	if err != nil || freed != 0 {
		t.Fatalf("referenced blob must survive GC: %d %v", freed, err)
	}
	if err := s.Blob().Release(ctx, digest); err != nil {
		t.Fatal(err)
	}
	freed, err = s.Blob().CollectGarbage(ctx, 0)
	if err != nil || freed != 5 {
		t.Fatalf("GC: %d %v", freed, err)
	}
	if _, err := s.Blob().Open(ctx, digest); err != store.ErrBlobNotFound {
		t.Fatalf("expected ErrBlobNotFound, got %v", err)
	}
}

func TestFakeMigrations(t *testing.T) {
	s := NewFakeStore()
	reg := s.Migrations()
	if reg.Latest() != 0 {
		t.Fatal("empty registry must report 0")
	}
	for v := 1; v <= 2; v++ {
		if err := reg.Register(store.Migration{Version: v, Name: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if reg.Latest() != 2 || len(reg.Ordered()) != 2 {
		t.Fatal("registry contents")
	}
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(store.Migration{Version: 2, Name: "dup"}); err == nil {
		t.Fatal("expected duplicate version error")
	}
	if err := reg.Register(store.Migration{Version: 4, Name: "gap"}); err == nil {
		t.Fatal("expected gap error")
	}
}
