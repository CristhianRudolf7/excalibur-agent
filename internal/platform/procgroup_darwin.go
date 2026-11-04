//go:build darwin

package platform

import (
	"errors"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// killGracePeriod is the Appendix B margin between SIGTERM and SIGKILL
// (Section 13.5): 2 seconds.
const killGracePeriod = 2 * time.Second

// DarwinProcessGroup runs every tool command in its own process group so
// the whole group, including grandchildren, can be killed in one call
// (Sections 3.2, 13.4). macOS has no Pdeathsig: graceful shutdown
// kills groups, but children can outlive a crashed daemon.
type DarwinProcessGroup struct{}

// Compile-time interface check.
var _ ProcessGroup = DarwinProcessGroup{}

// Configure implements platform.ProcessGroup: it detaches cmd into a
// new process group (Setpgid). An existing SysProcAttr is honoured;
// Darwin does not support the Linux parent-death signal.
func (DarwinProcessGroup) Configure(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Kill implements platform.ProcessGroup: it sends SIGTERM to the whole
// group (-pgid), waits up to 2s for the group to exit, then escalates
// to SIGKILL. A group that is already gone reports success.
func (DarwinProcessGroup) Kill(pid int) error {
	if pid <= 0 {
		return errors.New("platform: Kill requires a positive pid")
	}
	if err := (DarwinProcessGroup{}).Signal(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(killGracePeriod)
	for {
		if err := unix.Kill(-pid, 0); err != nil {
			if err == unix.ESRCH || (err == unix.EPERM && darwinGroupExited(pid)) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return (DarwinProcessGroup{}).Signal(pid, syscall.SIGKILL)
}

// Signal sends a signal to an owned process group. XNU may return EPERM
// for a zombie-only group. Confirm exit without waitpid, which would steal
// the exit status from exec.Cmd.Wait. Live, inaccessible members still fail.
func (DarwinProcessGroup) Signal(pid int, signal syscall.Signal) error {
	if pid <= 1 {
		return syscall.EINVAL
	}
	err := unix.Kill(-pid, signal)
	if err == unix.ESRCH || (err == unix.EPERM && darwinGroupExited(pid)) {
		return nil
	}
	return err
}

func darwinGroupExited(pid int) bool {
	err := unix.Kill(pid, 0)
	if err != nil && err != unix.ESRCH {
		return false
	}
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	return err == nil && darwinGroupHasExited(procs, pid)
}

func darwinGroupHasExited(procs []unix.KinfoProc, pid int) bool {
	// SZOMB from Darwin sys/proc.h; x/sys does not expose it.
	const zombie = 5
	for _, p := range procs {
		if (int(p.Proc.P_pid) == pid || int(p.Eproc.Pgid) == pid) && p.Proc.P_stat != zombie {
			return false
		}
	}
	return true
}
