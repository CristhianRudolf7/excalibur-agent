//go:build darwin

package cpupool

import (
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinLowerCurrentThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { _ = unix.Setpriority(3, 0, 0) }()
	if err := lowerCurrentThread(); err != nil {
		t.Fatal(err)
	}
	background, err := unix.Getpriority(3, 0)
	if err != nil || background != 1 {
		t.Fatalf("worker background policy = %d, %v", background, err)
	}
}
