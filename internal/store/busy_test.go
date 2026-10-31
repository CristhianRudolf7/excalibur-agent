package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// TestExternalLockBusy holds the file from a second handle past the
// busy ceiling: the porter retries, then reports typed ErrBusy, with
// nothing applied half-way (Section 4.3).
func TestExternalLockBusy(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")

	open := func(mut func(*store.Options)) *store.DB {
		opts := store.DefaultOptions()
		opts.BlobDir = filepath.Join(dir, "blobs")
		if mut != nil {
			mut(&opts)
		}
		db, err := store.Open(ctx, path, opts, migrations.Main())
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	victim := open(func(o *store.Options) {
		o.BusyTimeout = 200 * time.Millisecond
		o.BusyRetries = 2
	})
	defer victim.Close()

	holder := open(nil)
	defer holder.Close()

	// The holder takes RESERVED and keeps it for 3s: longer than the
	// victim's whole busy budget (200ms x (1+2) plus backoff).
	holderAcquired := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- holder.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			if _, err := ex.Exec(ctx, "INSERT INTO principals(id,name) VALUES('holder','h')"); err != nil {
				return err
			}
			close(holderAcquired)
			<-releaseHolder
			return nil
		})
	}()
	select {
	case <-holderAcquired:
	case <-time.After(10 * time.Second):
		t.Fatal("holder did not acquire the write lock")
	}

	start := time.Now()
	err := victim.Write(ctx, store.PriorityInteractive, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "INSERT INTO principals(id,name) VALUES('victim-a','a')")
		return err
	})
	elapsed := time.Since(start)
	if !errors.Is(err, store.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("no busy budget waited: %v", elapsed)
	}
	stats := victim.Stats()
	if stats.BusyErrors != 1 {
		t.Fatalf("busy errors = %d, want 1", stats.BusyErrors)
	}
	if stats.BusyRetries != 2 {
		t.Fatalf("busy retries = %d, want 2", stats.BusyRetries)
	}

	// Nothing applied half-way: the failed batch left no row.
	if got := countRows(t, victim, "principals"); got != 0 {
		t.Fatalf("principals = %d, want 0 (holder uncommitted, victim failed)", got)
	}

	close(releaseHolder)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	// The victim works again once the holder leaves.
	mustWrite(t, victim, store.PriorityInteractive, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "INSERT INTO principals(id,name) VALUES('victim-b','b')")
		return err
	})
	if got := countRows(t, victim, "principals"); got != 2 {
		t.Fatalf("principals = %d, want 2", got)
	}
}
