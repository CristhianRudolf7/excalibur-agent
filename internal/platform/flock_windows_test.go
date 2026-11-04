package platform

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func TestWindowsFileLockSerializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "lock")
	release, err := (WindowsFileLock{}).Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	type result struct {
		release Release
		err     error
	}
	done := make(chan result, 1)
	safego.Go("windows-lock-test", nil, func() { r, err := (WindowsFileLock{}).Acquire(path); done <- result{r, err} })
	select {
	case r := <-done:
		if r.release != nil {
			r.release()
		}
		t.Fatalf("second acquire did not block: %v", r.err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if err := r.release(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second acquire stuck")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsFileLockCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, err := (WindowsFileLock{}).Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := (WindowsFileLock{}).AcquireContext(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	r, err := (WindowsFileLock{}).AcquireContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	r()
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := (WindowsFileLock{}).AcquireContext(canceled, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
