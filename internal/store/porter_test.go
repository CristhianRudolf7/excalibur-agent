package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestGroupCommit batches concurrent writes: fewer batches than
// commands, every command applied exactly once (Section 4.3).
func TestGroupCommit(t *testing.T) {
	db := openBare(t, func(o *store.Options) {
		o.GroupCommitWait = 50 * time.Millisecond
		o.GroupCommitMaxOps = 200
	})
	ctx := context.Background()
	const writers = 8
	const perWriter = 25
	var baseline int64
	for _, n := range db.Stats().Executed {
		baseline += n
	}
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				errs <- db.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
					_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES(?,?)", keyOf(w, i), i)
					return err
				})
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := countRows(t, db, "kv"); got != writers*perWriter {
		t.Fatalf("rows = %d, want %d", got, writers*perWriter)
	}
	stats := db.Stats()
	if stats.Batches >= writers*perWriter {
		t.Fatalf("no batching: %d batches for %d commands", stats.Batches, writers*perWriter)
	}
	var executed int64
	for _, n := range stats.Executed {
		executed += n
	}
	if executed != writers*perWriter+baseline {
		t.Fatalf("executed = %d, want %d", executed, writers*perWriter+baseline)
	}
	t.Logf("group commit: %d commands in %d batches", writers*perWriter, stats.Batches)
}

func keyOf(w, i int) string {
	return string(rune('a'+w)) + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestSavepointIsolation fails only the violating command of a batch:
// the rest commits (Section 4.3).
func TestSavepointIsolation(t *testing.T) {
	db := openBare(t, func(o *store.Options) { o.GroupCommitWait = 50 * time.Millisecond })
	ctx := context.Background()
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('dup',1)")
		return err
	})

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

	results := make(chan error, 3)
	go func() {
		results <- db.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('good-a',1)")
			return err
		})
	}()
	go func() {
		results <- db.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('dup',2)")
			return err
		})
	}()
	go func() {
		results <- db.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('good-b',3)")
			return err
		})
	}()
	waitQueued(t, db, store.PriorityNormal)
	// All three must be queued before the batch runs; the blocker
	// holds the writer, so poll until the lane is empty of stragglers.
	deadline := time.Now().Add(10 * time.Second)
	for db.QueueLen(store.PriorityNormal) < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-blockerDone; err != nil {
		t.Fatal(err)
	}
	var constraintErrs, oks int
	timeout := time.After(10 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case err := <-results:
			if err == nil {
				oks++
			} else if strings.Contains(strings.ToLower(err.Error()), "constraint") ||
				strings.Contains(strings.ToLower(err.Error()), "unique") {
				constraintErrs++
			} else {
				t.Fatalf("unexpected error: %v", err)
			}
		case <-timeout:
			t.Fatal("batch did not complete")
		}
	}
	if oks != 2 || constraintErrs != 1 {
		t.Fatalf("oks=%d constraint=%d, want 2 and 1", oks, constraintErrs)
	}
	if got := countRows(t, db, "kv"); got != 3 {
		t.Fatalf("rows = %d, want 3 (seed + good-a + good-b)", got)
	}
}

// TestCanceledBeforeTake discards commands canceled while queued
// with ErrWriteCanceled and never runs them (Section 4.3).
func TestCanceledBeforeTake(t *testing.T) {
	db := openBare(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran atomic.Bool
	err := db.Write(ctx, store.PriorityNormal, func(context.Context, store.Execer) error {
		ran.Store(true)
		return nil
	})
	if !errors.Is(err, store.ErrWriteCanceled) {
		t.Fatalf("err = %v, want ErrWriteCanceled", err)
	}
	if ran.Load() {
		t.Fatal("canceled command ran")
	}
	if got := db.Stats().Canceled; got != 1 {
		t.Fatalf("canceled = %d, want 1", got)
	}
}

// TestBackpressure blocks the sender past a full lane until the
// context expires (Section 4.3: contrapresión).
func TestBackpressure(t *testing.T) {
	db := openBare(t, func(o *store.Options) {
		o.QueueDepth = 1
		o.GroupCommitWait = 50 * time.Millisecond
	})
	ctx := context.Background()
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	go func() {
		_ = db.Write(ctx, store.PriorityBackground, func(context.Context, store.Execer) error {
			once.Do(func() { close(started) })
			<-release
			return nil
		})
	}()
	<-started
	// One queued behind the blocker fills the lane (depth 1).
	queued := make(chan error, 1)
	go func() {
		queued <- db.Write(ctx, store.PriorityBackground, func(context.Context, store.Execer) error { return nil })
	}()

	deadline := time.Now().Add(10 * time.Second)
	for db.QueueLen(store.PriorityBackground) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if db.QueueLen(store.PriorityBackground) == 0 {
		t.Fatal("lane did not fill")
	}
	fast, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := db.Write(fast, store.PriorityBackground, func(context.Context, store.Execer) error { return nil })
	if !errors.Is(err, store.ErrWriteCanceled) {
		t.Fatalf("full lane err = %v, want ErrWriteCanceled", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("backpressure did not bound the wait")
	}
	close(release)
	if err := <-queued; err != nil {
		t.Fatal(err)
	}
}

// TestCloseDrains lets queued work finish before releasing handles.
func TestCloseDrains(t *testing.T) {
	ctx := context.Background()
	db := openBare(t, nil)
	var ran atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- db.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			time.Sleep(100 * time.Millisecond)
			ran.Store(true)
			_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('drain',1)")
			return err
		})
	}()
	time.Sleep(50 * time.Millisecond)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !ran.Load() {
		t.Fatal("queued write did not drain on Close")
	}
	if err := db.Write(ctx, store.PriorityNormal, func(context.Context, store.Execer) error { return nil }); err == nil {
		t.Fatal("write after Close must fail")
	}
}
