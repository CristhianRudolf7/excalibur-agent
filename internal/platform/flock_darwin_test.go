//go:build darwin

package platform

import (
	"context"
	"errors"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDarwinFileLockExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "daemon.lock")
	rel, err := (DarwinFileLock{}).Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("lock file mode %o, want 0600", fi.Mode().Perm())
	}
	// A second holder blocks until release.
	acquired := make(chan Release, 1)
	safego.Go("darwin-flock-test", nil, func() {
		r, err := (DarwinFileLock{}).Acquire(path)
		if err != nil {
			return
		}
		acquired <- r
	})
	select {
	case <-acquired:
		t.Fatal("second Acquire must block while the lock is held")
	case <-time.After(200 * time.Millisecond):
	}
	if err := rel(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	select {
	case r2 := <-acquired:
		if err := r2(); err != nil {
			t.Fatalf("second Release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second Acquire did not proceed after release")
	}
	// Release is idempotent.
	if err := rel(); err != nil {
		t.Fatalf("second Release call: %v", err)
	}
}

func TestDarwinFileLockMutualExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counter.lock")
	var inside atomic.Int32
	var violations atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		safego.Go("darwin-flock-test", nil, func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				rel, err := (DarwinFileLock{}).Acquire(path)
				if err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				if inside.Add(1) != 1 {
					violations.Add(1)
				}
				inside.Add(-1)
				if err := rel(); err != nil {
					t.Errorf("Release: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	if violations.Load() != 0 {
		t.Fatalf("%d mutual-exclusion violations", violations.Load())
	}
}

func TestDarwinFileLockContextCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctx.lock")
	rel, err := (DarwinFileLock{}).Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = rel() }()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := (DarwinFileLock{}).AcquireContext(ctx, path); err == nil {
		t.Fatal("AcquireContext must fail when ctx expires")
	} else if time.Since(start) > 5*time.Second {
		t.Fatalf("AcquireContext ignored ctx: %v", err)
	}
	// After release, a cancellable acquire succeeds.
	if err := rel(); err != nil {
		t.Fatal(err)
	}
	rel, err = (DarwinFileLock{}).Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	rel2, err := (DarwinFileLock{}).AcquireContext(ctx2, filepath.Join(t.TempDir(), "other.lock"))
	if err != nil {
		t.Fatalf("AcquireContext on free lock: %v", err)
	}
	_ = rel()
	_ = rel2()
}

func TestDarwinFileLockProcess(t *testing.T) {
	if path := os.Getenv("EXCALIBUR_TEST_FLOCK"); path != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		rel, err := (DarwinFileLock{}).AcquireContext(ctx, path)
		if os.Getenv("EXCALIBUR_TEST_FLOCK_BLOCKED") == "1" {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("contender: %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			_ = rel()
		}
		return
	}
	path := filepath.Join(t.TempDir(), "cross-process.lock")
	rel, err := (DarwinFileLock{}).Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rel() }()
	run := func(blocked string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestDarwinFileLockProcess$")
		cmd.Env = append(os.Environ(), "EXCALIBUR_TEST_FLOCK="+path, "EXCALIBUR_TEST_FLOCK_BLOCKED="+blocked)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v: %s", err, out)
		}
	}
	run("1")
	if err := rel(); err != nil {
		t.Fatal(err)
	}
	run("0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (DarwinFileLock{}).AcquireContext(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
