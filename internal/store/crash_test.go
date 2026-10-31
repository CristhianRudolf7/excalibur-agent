package store_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// TestCrashChildHelper is the kill -9 victim: it hammers one database
// with concurrent group-commit writes and crash-ordered blob puts
// until the parent kills it. It only runs as a re-execed child.
func TestCrashChildHelper(t *testing.T) {
	if os.Getenv("EXCALIBUR_STORE_CRASH_CHILD") == "" {
		t.Skip("child only")
	}
	ctx := context.Background()
	path := os.Getenv("EXCALIBUR_STORE_CRASH_PATH")
	opts := store.DefaultOptions()
	opts.BlobDir = os.Getenv("EXCALIBUR_STORE_CRASH_BLOBS")
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				prio := store.Priority(w % 4)
				_ = db.Write(ctx, prio, func(_ context.Context, ex store.Execer) error {
					_, err := ex.Exec(ctx, "INSERT INTO crash_kv(k,v) VALUES(?,?)",
						fmt.Sprintf("w%d-%d", w, i), i)
					return err
				})
			}
		}(w)
	}
	for i := 0; ; i++ {
		payload := bytes.Repeat([]byte(fmt.Sprintf("crash-blob-%d-", i)), 200)
		if _, err := db.Blob().Put(ctx, bytes.NewReader(payload)); err != nil {
			t.Fatal(err)
		}
		if i%50 == 0 {
			_ = db.Checkpoint(ctx, "PASSIVE")
		}
	}
}

// TestPowerCutBlobInvariant kills the writer mid-group-commit and
// mid-blob-write, then asserts the Section 4.2 ordering guarantee:
// committed work persists, quick_check passes, and no row points at
// a missing blob.
func TestPowerCutBlobInvariant(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	blobDir := filepath.Join(dir, "blobs")

	seedOpts := store.DefaultOptions()
	seedOpts.BlobDir = blobDir
	seed, err := store.Open(ctx, path, seedOpts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Write(ctx, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, "CREATE TABLE crash_kv(k TEXT, v INTEGER)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// One durable blob before the storm, to prove old data survives.
	before, err := seed.Blob().Put(ctx, bytes.NewReader([]byte("before the storm")))
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run", "TestCrashChildHelper", "-test.v")
	child.Env = append(os.Environ(),
		"EXCALIBUR_STORE_CRASH_CHILD=1",
		"EXCALIBUR_STORE_CRASH_PATH="+path,
		"EXCALIBUR_STORE_CRASH_BLOBS="+blobDir,
	)
	child.Stdout = nil
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // let group commits and blobs pile up
	if err := child.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()

	// Reopen: recovery replays the WAL, no restore needed.
	afterOpts := store.DefaultOptions()
	afterOpts.BlobDir = blobDir
	after, err := store.Open(ctx, path, afterOpts, migrations.Main())
	if err != nil {
		t.Fatalf("reopen after kill -9: %v", err)
	}
	defer after.Close()

	ok, err := after.QuickCheck(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("quick_check failed after kill -9")
	}
	// Pre-storm blob intact.
	rc, err := after.Blob().Open(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()

	// The invariant: every row resolves to a file.
	var missing int
	if err := after.Read(ctx, func(_ context.Context, q store.Queryer) error {
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
			f, err := after.Blob().Open(ctx, d)
			if err != nil {
				missing++
				continue
			}
			_ = f.Close()
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("%d rows point at missing blobs", missing)
	}
	var kv, blobs int
	_ = after.Read(ctx, func(_ context.Context, q store.Queryer) error {
		_ = q.QueryRow(ctx, "SELECT COUNT(*) FROM crash_kv").Scan(&kv)
		return q.QueryRow(ctx, "SELECT COUNT(*) FROM blob_refs").Scan(&blobs)
	})
	t.Logf("survived kill -9: %d rows, %d blob rows, quick_check ok", kv, blobs)
	if kv == 0 || blobs == 0 {
		t.Fatal("no concurrent work survived the kill; the storm never ran")
	}
}
