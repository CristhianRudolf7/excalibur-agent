package store_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// openBare opens a database without schema for porter-level tests.
func openBare(t *testing.T, mut func(*store.Options)) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	if mut != nil {
		mut(&opts)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"), opts, store.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "CREATE TABLE IF NOT EXISTS kv(k TEXT PRIMARY KEY, v INTEGER)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return db
}

// openSchema opens a database with the full H2 schema.
func openSchema(t *testing.T, mut func(*store.Options)) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	if mut != nil {
		mut(&opts)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustWrite(t *testing.T, db *store.DB, p store.Priority, fn store.WriteFunc) {
	t.Helper()
	if err := db.Write(context.Background(), p, fn); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, db *store.DB, table string) int {
	t.Helper()
	var n int
	if err := db.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// openTele opens a telemetry database for purge/retention tests.
func openTele(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.TelemetryOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	tele, err := store.Open(ctx, filepath.Join(t.TempDir(), "tele.db"), opts, migrations.Telemetry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tele.Close() })
	return tele
}

// waitQueued polls until every listed lane holds at least one command.
func waitQueued(t *testing.T, db *store.DB, lanes ...store.Priority) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready := true
		for _, p := range lanes {
			if db.QueueLen(p) == 0 {
				ready = false
			}
		}
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("lanes did not queue in time")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestPriorityOrder locks the writer behind a blocker, queues one
// command per lane, and asserts the batch runs highest-priority
// first (Section 4.3 lane order).
func TestPriorityOrder(t *testing.T) {
	db := openBare(t, nil)
	ctx := context.Background()
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	blockerDone := make(chan error, 1)
	go func() {
		blockerDone <- db.Write(ctx, store.PriorityBackground, func(context.Context, store.Execer) error {
			once.Do(func() { close(started) })
			<-release
			return nil
		})
	}()
	<-started

	var mu sync.Mutex
	var order []store.Priority
	var wg sync.WaitGroup
	for _, p := range []store.Priority{store.PriorityBackground, store.PriorityNormal, store.PriorityJournal, store.PriorityInteractive} {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = db.Write(ctx, p, func(context.Context, store.Execer) error {
				mu.Lock()
				order = append(order, p)
				mu.Unlock()
				return nil
			})
		}()
	}
	waitQueued(t, db, store.PriorityBackground, store.PriorityNormal, store.PriorityJournal, store.PriorityInteractive)
	close(release)
	if err := <-blockerDone; err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	want := []store.Priority{store.PriorityInteractive, store.PriorityJournal, store.PriorityNormal, store.PriorityBackground}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
