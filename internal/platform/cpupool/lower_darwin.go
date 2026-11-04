//go:build darwin

package cpupool

import (
	"errors"
	"golang.org/x/sys/unix"
)

// lowerCurrentThread backgrounds this pinned worker's OS thread,
// reducing CPU, disk I/O and network priority (Section 3.2). Darwin's
// PRIO_DARWIN_THREAD always addresses the calling thread with who=0.
func lowerCurrentThread() error {
	const (
		prioDarwinThread = 3
		prioDarwinBG     = 0x1000
	)
	err := unix.Setpriority(prioDarwinThread, 0, prioDarwinBG)
	// XNU returns EIDRM when it removes a pre-existing QoS policy.
	// The next call confirms background mode after that transition.
	if errors.Is(err, unix.EIDRM) {
		return unix.Setpriority(prioDarwinThread, 0, prioDarwinBG)
	}
	return err
}
