package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestDiskFullPausesBackground fills the file to its page budget
// (real SQLITE_FULL), then asserts the Section 4.3 policy: the
// episode is typed ErrDiskFull, background writes pause, and
// recovery clears the episode.
func TestDiskFullPausesBackground(t *testing.T) {
	var notified atomic.Int64
	db := openBare(t, func(o *store.Options) {
		o.OnDiskFull = func() { notified.Add(1) }
	})
	ctx := context.Background()

	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "PRAGMA max_page_count=30")
		return err
	})
	// Grow past the budget with real payload.
	fullSeen := false
	for i := 0; i < 2000 && !fullSeen; i++ {
		err := db.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES(?,zeroblob(2000))", fmt.Sprintf("pad-%d", i))
			return err
		})
		if err != nil {
			if errors.Is(err, store.ErrDiskFull) {
				fullSeen = true
			} else {
				t.Fatalf("fill err = %v", err)
			}
		}
	}
	if !fullSeen {
		t.Skip("page budget did not trip SQLITE_FULL on this backend")
	}
	if notified.Load() != 1 {
		t.Fatalf("disk-full notifications = %d, want 1", notified.Load())
	}
	// Background lane pauses fast without executing.
	var ran atomic.Bool
	err := db.Write(ctx, store.PriorityBackground, func(context.Context, store.Execer) error {
		ran.Store(true)
		return nil
	})
	if !errors.Is(err, store.ErrDiskFull) {
		t.Fatalf("background err = %v, want ErrDiskFull", err)
	}
	if ran.Load() {
		t.Fatal("background write ran during disk-full episode")
	}
	// Recovery: raise the budget, write, and the background lane
	// resumes.
	mustWrite(t, db, store.PriorityInteractive, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "PRAGMA max_page_count=100000")
		return err
	})
	mustWrite(t, db, store.PriorityBackground, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('after-recovery',1)")
		return err
	})
	if got := countRows(t, db, "kv"); got == 0 {
		t.Fatal("no rows after recovery")
	}
}
