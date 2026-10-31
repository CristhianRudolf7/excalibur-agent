//go:build linux

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

// LinuxProcessGroup runs every tool command in its own process group so
// the whole group, including grandchildren, can be killed in one call
// (Sections 3.2, 13.4). Configure also arms Pdeathsig so children do
// not survive the daemon.
type LinuxProcessGroup struct{}

// Compile-time interface check.
var _ ProcessGroup = LinuxProcessGroup{}

// Configure implements platform.ProcessGroup: it detaches cmd into a
// new process group (Setpgid) and asks the kernel to SIGTERM the child
// if the daemon dies (Pdeathsig). An existing SysProcAttr is honoured
// and only the two fields are forced on.
func (LinuxProcessGroup) Configure(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}

// Kill implements platform.ProcessGroup: it sends SIGTERM to the whole
// group (-pgid), waits up to 2s for the leader to exit, then escalates
// to SIGKILL. A group that is already gone reports success.
func (LinuxProcessGroup) Kill(pid int) error {
	if pid <= 0 {
		return errors.New("platform: Kill requires a positive pid")
	}
	if err := unix.Kill(-pid, syscall.SIGTERM); err != nil {
		if err == unix.ESRCH {
			return nil
		}
		return err
	}
	deadline := time.Now().Add(killGracePeriod)
	for {
		if err := unix.Kill(pid, 0); err != nil {
			// ESRCH: leader gone. EPERM: still alive but un-signalable;
			// keep waiting for the grace period, then SIGKILL.
			if err == unix.ESRCH {
				return nil
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := unix.Kill(-pid, syscall.SIGKILL); err != nil {
		if err == unix.ESRCH {
			return nil
		}
		return err
	}
	return nil
}
