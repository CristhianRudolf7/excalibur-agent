//go:build linux

package vault

import "golang.org/x/sys/unix"

// lockMemory pins the DEK page with mlock when the system allows it.
// Failure is best-effort and never fatal (Section 20.1).
func lockMemory(b []byte) {
	if len(b) == 0 {
		return
	}
	_ = unix.Mlock(b)
}

// unlockMemory releases a previous mlock.
func unlockMemory(b []byte) {
	if len(b) == 0 {
		return
	}
	_ = unix.Munlock(b)
}
