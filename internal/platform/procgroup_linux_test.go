//go:build linux

package platform

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLinuxProcessGroupConfigure(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	LinuxProcessGroup{}.Configure(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("Configure must set Setpgid")
	}
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("Configure must arm Pdeathsig, got %v", cmd.SysProcAttr.Pdeathsig)
	}
	// Existing SysProcAttr is honoured, not replaced.
	cmd2 := exec.Command("sleep", "30")
	cmd2.SysProcAttr = &syscall.SysProcAttr{Setctty: false}
	LinuxProcessGroup{}.Configure(cmd2)
	if !cmd2.SysProcAttr.Setpgid {
		t.Fatal("Configure must preserve existing SysProcAttr")
	}
}

func TestLinuxProcessGroupKill(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	LinuxProcessGroup{}.Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	if err := (LinuxProcessGroup{}).Kill(pid); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after Kill")
	}
	if err := unix.Kill(pid, 0); err != unix.ESRCH {
		t.Fatalf("pid %d still alive: %v", pid, err)
	}
}

func TestLinuxProcessGroupKillDead(t *testing.T) {
	// Killing a reaped, non-existent pid reports success.
	if err := (LinuxProcessGroup{}).Kill(1 << 30); err != nil {
		t.Fatalf("Kill on missing pid: %v", err)
	}
	if err := (LinuxProcessGroup{}).Kill(0); err == nil {
		t.Fatal("Kill(0) must fail")
	}
	if err := (LinuxProcessGroup{}).Kill(-5); err == nil {
		t.Fatal("Kill(-5) must fail")
	}
}
