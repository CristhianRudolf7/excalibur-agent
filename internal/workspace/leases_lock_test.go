package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// mutexLock is a portable stand-in for the advisory OS lock: two
// LeaseManagers (two daemons) share one, like two processes share a
// flock. The token channel makes waits cancel-safe: a wait that
// loses the race never holds the token, mirroring
// LinuxFileLock.AcquireContext (LOCK_NB polling).
type mutexLock struct {
	sem chan struct{}
}

func newMutexLock() *mutexLock {
	l := &mutexLock{sem: make(chan struct{}, 1)}
	l.sem <- struct{}{}
	return l
}

func (l *mutexLock) Acquire(_ string) (platform.Release, error) {
	<-l.sem
	return func() error { l.sem <- struct{}{}; return nil }, nil
}

// AcquireContext implements ctxLocker.
func (l *mutexLock) AcquireContext(ctx context.Context, _ string) (platform.Release, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.sem:
		return func() error { l.sem <- struct{}{}; return nil }, nil
	}
}

// Section 16.6 row 2 (in-RAM shape): two daemons over the same
// project coordinate through the shared advisory lock even though
// their lease tables are separate.
func TestLeasesCrossDaemonMutex(t *testing.T) {
	shared := newMutexLock()
	a := NewLeaseManager(WithLeaseTimings(200*time.Millisecond, time.Minute), WithFileLock(shared))
	b := NewLeaseManager(WithLeaseTimings(100*time.Millisecond, time.Minute), WithFileLock(shared))
	relA, err := a.Acquire("/proj/shared.go", "daemon-a-run-1", "editing")
	if err != nil {
		t.Fatal(err)
	}
	// Daemon B has no in-memory knowledge of A's lease; only the
	// shared lock stops it.
	start := time.Now()
	_, err = b.Acquire("/proj/shared.go", "daemon-b-run-9", "editing")
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("want *HeldError from the shared lock, got %v", err)
	}
	if time.Since(start) < 80*time.Millisecond {
		t.Fatal("B must block on the shared lock up to its budget")
	}
	relA()
	relB, err := b.Acquire("/proj/shared.go", "daemon-b-run-9", "editing")
	if err != nil {
		t.Fatalf("B must proceed once A releases: %v", err)
	}
	relB()
}
