//go:build darwin

package platform

import (
	"errors"

	"golang.org/x/sys/unix"
)

// Darwin background policy lowers CPU, disk I/O and network priority.
// The constants are public Darwin sys/resource.h values, kept local
// because not every x/sys release exposes them.
const (
	DefaultToolNice   = 10
	prioDarwinThread  = 3
	prioDarwinProcess = 4
	prioDarwinBG      = 0x1000
)

// DarwinProcessPriority lowers tool processes and pinned background
// threads (Sections 3.2, 5.2, 13.4). macOS has no delegated cgroups.
type DarwinProcessPriority struct {
	// Nice overrides the default absolute nice value of 10.
	Nice int
}

var _ ProcessPriority = DarwinProcessPriority{}

// LowerProcess requests background scheduling, then sets an absolute nice
// value. Background policy is best-effort and can change the nice value,
// so apply the requested nice last. Never improve an inherited lower priority.
func (p DarwinProcessPriority) LowerProcess(pid int) error {
	if pid <= 0 {
		return errors.New("platform: LowerProcess requires a positive pid")
	}
	cur, err := EffectiveNice(pid)
	if err != nil {
		return err
	}
	want := p.targetNice(cur)
	_ = unix.Setpriority(prioDarwinProcess, pid, prioDarwinBG)
	cur, err = EffectiveNice(pid)
	if err != nil {
		return err
	}
	if want > cur {
		if err := unix.Setpriority(unix.PRIO_PROCESS, pid, want); err != nil {
			return err
		}
	}
	return nil
}

func (p DarwinProcessPriority) targetNice(cur int) int {
	want := p.Nice
	if want == 0 {
		want = DefaultToolNice
	}
	if want < 0 {
		return cur
	}
	if want > 20 {
		want = 20
	}
	return max(cur, want)
}

// LowerCurrentThread backgrounds only the calling OS thread. The CPU
// pool pins each worker before calling this method.
func (DarwinProcessPriority) LowerCurrentThread() error {
	err := unix.Setpriority(prioDarwinThread, 0, prioDarwinBG)
	// XNU removes an existing QoS policy while applying background mode
	// and reports EIDRM. Retry once after that transition.
	if errors.Is(err, unix.EIDRM) {
		return unix.Setpriority(prioDarwinThread, 0, prioDarwinBG)
	}
	return err
}

// EffectiveNice reads the libc nice value directly on Darwin; unlike
// the Linux syscall result, it is already in the -20..20 range.
func EffectiveNice(pid int) (int, error) { return unix.Getpriority(unix.PRIO_PROCESS, pid) }

// SetMemoryMax reports that macOS has no cgroup memory.max equivalent.
func (DarwinProcessPriority) SetMemoryMax(_ int, _ uint64) error { return errors.ErrUnsupported }

// Delegated is false because macOS has no cgroups.
func (DarwinProcessPriority) Delegated(_ int) bool { return false }
