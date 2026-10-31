//go:build linux

package workspace

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// Section 16.6 row 2 with the real advisory lock: two lease managers
// (two daemons) serialize on Linux flock. flock is consultative, so
// this only coordinates cooperating processes.
func TestLeasesCrossDaemonFlock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "shared.go.lock")
	fl := platform.LinuxFileLock{}
	a := NewLeaseManager(WithLeaseTimings(2*time.Second, time.Minute), WithFileLock(fl))
	b := NewLeaseManager(WithLeaseTimings(300*time.Millisecond, time.Minute), WithFileLock(fl))
	relA, err := a.Acquire(lockPath, "daemon-a", "editing")
	if err != nil {
		t.Fatal(err)
	}
	defer relA()
	done := make(chan error, 1)
	go func() {
		rel, err := b.Acquire(lockPath, "daemon-b", "editing")
		if err != nil {
			done <- err
			return
		}
		defer rel()
		done <- nil
	}()
	select {
	case err := <-done:
		t.Fatalf("B must block on flock, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	relA()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("B must proceed once A releases flock: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("B never acquired after A released")
	}
}
