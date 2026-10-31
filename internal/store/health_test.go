package store_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestNetworkFSForcesDelete proves the Section 4.3 degradation: on a
// network filesystem the journal runs in DELETE mode and the store
// reports it.
func TestNetworkFSForcesDelete(t *testing.T) {
	db := openBare(t, func(o *store.Options) { o.ForceNetworkFS = true })
	if !db.Degraded() {
		t.Fatal("expected degraded flag on forced network FS")
	}
	if db.JournalMode() != "DELETE" {
		t.Fatalf("journal mode = %s, want DELETE", db.JournalMode())
	}
	mustWrite(t, db, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES('a',1)")
		return err
	})
	if got := countRows(t, db, "kv"); got != 1 {
		t.Fatalf("rows = %d", got)
	}
}

// TestJournalModeOverride pins DELETE without the degraded flag.
func TestJournalModeOverride(t *testing.T) {
	db := openBare(t, func(o *store.Options) { o.ForceJournalMode = "DELETE" })
	if db.Degraded() {
		t.Fatal("override must not set the degraded flag")
	}
	if db.JournalMode() != "DELETE" {
		t.Fatalf("journal mode = %s", db.JournalMode())
	}
}

// TestSynchronousModes wires FULL for durable files and NORMAL for
// telemetry (Section 4.2).
func TestSynchronousModes(t *testing.T) {
	main := openSchema(t, nil)
	assertPragma(t, main, "synchronous", "FULL")
	tele := openTele(t)
	assertPragma(t, tele, "synchronous", "NORMAL")
}

func assertPragma(t *testing.T, db *store.DB, pragma, want string) {
	t.Helper()
	var got any
	_ = got
	var text string
	var num int
	isNum := pragma == "synchronous"
	if err := db.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		if isNum {
			return q.QueryRow(context.Background(), "PRAGMA "+pragma).Scan(&num)
		}
		return q.QueryRow(context.Background(), "PRAGMA "+pragma).Scan(&text)
	}); err != nil {
		t.Fatal(err)
	}
	if isNum {
		wantNum := map[string]int{"FULL": 2, "NORMAL": 1, "OFF": 0}[want]
		if num != wantNum {
			t.Fatalf("pragma %s = %d, want %s (%d)", pragma, num, want, wantNum)
		}
		return
	}
	if !strings.EqualFold(text, want) {
		t.Fatalf("pragma %s = %q, want %s", pragma, text, want)
	}
}

// TestReadPoolIsReadOnly proves query_only on the read lane: writes
// through Read fail instead of splitting the brain.
func TestReadPoolIsReadOnly(t *testing.T) {
	db := openBare(t, nil)
	tryWrite := func(read func(context.Context, store.ReadFunc) error) error {
		return read(context.Background(), func(ctx context.Context, q store.Queryer) error {
			rows, err := q.Query(ctx, "INSERT INTO kv(k,v) VALUES('x',1)")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
			}
			return rows.Err()
		})
	}
	if err := tryWrite(db.Read); err == nil {
		t.Fatal("write through the read pool succeeded")
	}
	if err := tryWrite(db.ReadInteractive); err == nil {
		t.Fatal("write through the interactive lane succeeded")
	}
}

// TestWALAlertAndCheckpoints exercises the 256 MiB watcher at a small
// threshold: a long reader pins the WAL (Section 4.3: long reads
// starve checkpoints), the crossing alerts exactly once, and TRUNCATE
// reclaims afterwards.
func TestWALAlertAndCheckpoints(t *testing.T) {
	var alerts atomic.Int64
	db := openBare(t, func(o *store.Options) {
		o.WALAlertBytes = 16 << 10 // 16 KiB: test-scale stand-in for 256 MiB
		o.OnWALAlert = func(int64) { alerts.Add(1) }
	})
	ctx := context.Background()

	// Pin the WAL with a long reader, then write through it.
	releaseReader := make(chan struct{})
	readerDone := make(chan error, 1)
	go func() {
		readerDone <- db.Read(ctx, func(_ context.Context, q store.Queryer) error {
			rows, err := q.Query(ctx, "SELECT k FROM kv")
			if err != nil {
				return err
			}
			defer rows.Close()
			<-releaseReader
			return nil
		})
	}()
	time.Sleep(200 * time.Millisecond) // let the reader pin its mark
	big := strings.Repeat("x", 2000)
	for i := 0; i < 100; i++ {
		mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, "INSERT INTO kv(k,v) VALUES(?,?)", fmt.Sprintf("k-%d-%s", i, big[:i%100+1]), i)
			return err
		})
	}
	size, over := db.CheckWAL()
	t.Logf("wal size=%d over=%v", size, over)
	if !over {
		t.Fatalf("wal did not outgrow the test threshold (size=%d)", size)
	}
	if alerts.Load() != 1 {
		t.Fatalf("alerts = %d, want exactly 1 edge-triggered", alerts.Load())
	}
	close(releaseReader)
	if err := <-readerDone; err != nil {
		t.Fatal(err)
	}
	before := db.WALBytes()
	if err := db.Checkpoint(ctx, "TRUNCATE"); err != nil {
		t.Fatal(err)
	}
	after := db.WALBytes()
	if after > before {
		t.Fatalf("truncate grew the WAL: %d -> %d", before, after)
	}
	t.Logf("wal after truncate: %d (was %d)", after, before)
	if err := db.Checkpoint(ctx, "BOGUS"); err == nil {
		t.Fatal("expected invalid checkpoint mode error")
	}
}
