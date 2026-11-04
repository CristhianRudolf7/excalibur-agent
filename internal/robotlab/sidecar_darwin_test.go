//go:build darwin

package robotlab

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinSidecarGroupAndPriority(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	group := sidecarProcessGroup()
	group.Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid != cmd.Process.Pid {
		t.Fatalf("sidecar pgid=%d: %v", pgid, err)
	}
	if err := lowerSidecarPriority(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if info.Proc.P_nice < 15 {
		t.Fatalf("sidecar nice=%d, want at least 15", info.Proc.P_nice)
	}
	if err := group.Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("sidecar survived group kill")
	}
}
