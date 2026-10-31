package store_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestConcurrentWritersNoBusy is the Section 4.8 load test: writers
// on all four lanes plus parallel readers. Internal contention must
// never surface as SQLITE_BUSY (single writer, BEGIN IMMEDIATE), and
// interactive writes must wait at most about one group commit.
func TestConcurrentWritersNoBusy(t *testing.T) {
	db := openBare(t, func(o *store.Options) {
		o.GroupCommitWait = 5 * time.Millisecond
		o.GroupCommitMaxOps = 200
	})
	ctx := context.Background()

	const writersPerLane = 4
	const perWriter = 50
	var writers sync.WaitGroup
	var readers sync.WaitGroup
	errs := make(chan error, 4*writersPerLane*perWriter)
	var interactiveNanos atomic.Int64
	var interactiveCount atomic.Int64

	lanes := []store.Priority{store.PriorityInteractive, store.PriorityJournal, store.PriorityNormal, store.PriorityBackground}
	for _, lane := range lanes {
		for w := 0; w < writersPerLane; w++ {
			writers.Add(1)
			go func(lane store.Priority, w int) {
				defer writers.Done()
				for i := 0; i < perWriter; i++ {
					start := time.Now()
					err := db.Write(ctx, lane, func(_ context.Context, ex store.Execer) error {
						_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES(?,?)",
							fmt.Sprintf("%d/%d/%d", lane, w, i), i)
						return err
					})
					if lane == store.PriorityInteractive {
						interactiveNanos.Add(time.Since(start).Nanoseconds())
						interactiveCount.Add(1)
					}
					errs <- err
				}
			}(lane, w)
		}
	}
	// Parallel readers, including the reserved interactive lane.
	stopReaders := make(chan struct{})
	var readErrs atomic.Int64
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func(r int) {
			defer readers.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				var err error
				if r == 0 {
					err = db.ReadInteractive(ctx, func(_ context.Context, q store.Queryer) error {
						rows, err := q.Query(ctx, "SELECT k FROM kv LIMIT 5")
						if err != nil {
							return err
						}
						defer rows.Close()
						for rows.Next() {
							var k string
							if err := rows.Scan(&k); err != nil {
								return err
							}
						}
						return rows.Err()
					})
				} else {
					err = db.Read(ctx, func(_ context.Context, q store.Queryer) error {
						var n int
						return q.QueryRow(ctx, "SELECT COUNT(*) FROM kv").Scan(&n)
					})
				}
				if err != nil {
					readErrs.Add(1)
					return
				}
			}
		}(r)
	}
	writers.Wait()
	close(stopReaders)
	readers.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("write error: %v", err)
		}
	}
	if readErrs.Load() != 0 {
		t.Fatalf("reader errors: %d", readErrs.Load())
	}
	want := 4 * writersPerLane * perWriter
	if got := countRows(t, db, "kv"); got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	stats := db.Stats()
	if stats.BusyErrors != 0 || stats.BusyRetries != 0 {
		t.Fatalf("internal busy surfaced: %+v", stats)
	}
	avg := time.Duration(interactiveNanos.Load() / interactiveCount.Load())
	t.Logf("interactive avg write latency under load: %v (%d batches)", avg, stats.Batches)
	if avg > 500*time.Millisecond {
		t.Fatalf("interactive writes too slow under load: %v", avg)
	}
	for _, err := range []error{store.ErrBusy} {
		_ = err
	}
}

// TestLongReaderDoesNotBlockWriters keeps one read transaction open
// (simulating a long scan) while writes flow: WAL readers never block
// the single writer (Section 4.3).
func TestLongReaderDoesNotBlockWriters(t *testing.T) {
	db := openBare(t, nil)
	ctx := context.Background()
	release := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		readDone <- db.Read(ctx, func(_ context.Context, q store.Queryer) error {
			rows, err := q.Query(ctx, "SELECT k FROM kv")
			if err != nil {
				return err
			}
			defer rows.Close()
			<-release
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond)
	done := make(chan error, 1)
	go func() {
		done <- db.Write(ctx, store.PriorityInteractive, func(_ context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('long-read',1)")
			return err
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("write blocked behind a long reader")
	}
	close(release)
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, db, "kv"); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

// TestInteractiveLaneReserved proves the interactive lane serves even
// when the shared pool is saturated by background readers.
func TestInteractiveLaneReserved(t *testing.T) {
	db := openBare(t, func(o *store.Options) { o.ReadPoolSize = 2 })
	ctx := context.Background()
	hold := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = db.Read(ctx, func(_ context.Context, q store.Queryer) error {
				<-hold
				var n int
				return q.QueryRow(ctx, "SELECT COUNT(*) FROM kv").Scan(&n)
			})
		}()
	}
	time.Sleep(200 * time.Millisecond) // let readers occupy the pool
	done := make(chan error, 1)
	go func() {
		done <- db.ReadInteractive(ctx, func(_ context.Context, q store.Queryer) error {
			var n int
			return q.QueryRow(ctx, "SELECT COUNT(*) FROM kv").Scan(&n)
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("interactive lane starved by background readers")
	}
	close(hold)
	wg.Wait()
}

func TestNoBusyStringInErrors(t *testing.T) {
	// Guard: porter errors must be typed sentinels, never raw
	// "SQLITE_BUSY" strings leaking to callers.
	for _, err := range []error{store.ErrBusy, store.ErrDiskFull, store.ErrWriteCanceled} {
		if strings.Contains(err.Error(), "SQLITE_BUSY") {
			t.Fatalf("sentinel leaks backend detail: %v", err)
		}
	}
}
