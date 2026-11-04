//go:build darwin

package platform

import (
	"bufio"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDarwinProcessGroupConfigure(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	DarwinProcessGroup{}.Configure(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("Configure must set Setpgid")
	}
	// Existing SysProcAttr is honoured, not replaced.
	cmd2 := exec.Command("sleep", "30")
	cmd2.SysProcAttr = &syscall.SysProcAttr{Setctty: false}
	DarwinProcessGroup{}.Configure(cmd2)
	if !cmd2.SysProcAttr.Setpgid {
		t.Fatal("Configure must preserve existing SysProcAttr")
	}
}

func TestDarwinProcessGroupKill(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	DarwinProcessGroup{}.Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	done := make(chan error, 1)
	safego.Go("darwin-group-wait", nil, func() { done <- cmd.Wait() })

	if err := (DarwinProcessGroup{}).Kill(pid); err != nil {
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

func TestDarwinProcessGroupKillDead(t *testing.T) {
	// Killing a reaped, non-existent pid reports success.
	if err := (DarwinProcessGroup{}).Kill(1 << 30); err != nil {
		t.Fatalf("Kill on missing pid: %v", err)
	}
	if err := (DarwinProcessGroup{}).Kill(0); err == nil {
		t.Fatal("Kill(0) must fail")
	}
	if err := (DarwinProcessGroup{}).Kill(-5); err == nil {
		t.Fatal("Kill(-5) must fail")
	}
}

// The leader exits on TERM while a grandchild ignores it. Kill must
// still escalate against the remaining group.
func TestDarwinProcessGroupGrandchild(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", `sh -c 'trap "" TERM; echo $$; exec sleep 30' & wait`)
	DarwinProcessGroup{}.Configure(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL); _ = cmd.Wait() }()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	if child == os.Getpid() {
		t.Fatal("invalid child pid")
	}
	if err := (DarwinProcessGroup{}).Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(child)).Output()
		if err != nil || strings.TrimSpace(string(raw)) == "" || strings.HasPrefix(strings.TrimSpace(string(raw)), "Z") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("grandchild survived group kill")
}

func TestDarwinGroupExitSnapshot(t *testing.T) {
	proc := func(pid, group int32, state int8) unix.KinfoProc {
		var p unix.KinfoProc
		p.Proc.P_pid, p.Eproc.Pgid, p.Proc.P_stat = pid, group, state
		return p
	}
	for _, tc := range []struct {
		name   string
		procs  []unix.KinfoProc
		exited bool
	}{
		{"reaped", nil, true},
		{"zombie leader", []unix.KinfoProc{proc(42, 42, 5)}, true},
		{"zombie descendants", []unix.KinfoProc{proc(43, 42, 5)}, true},
		{"live leader", []unix.KinfoProc{proc(42, 42, 3)}, false},
		{"live descendant after leader exits", []unix.KinfoProc{proc(42, 42, 5), proc(43, 42, 3)}, false},
		{"unrelated process", []unix.KinfoProc{proc(44, 44, 3)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := darwinGroupHasExited(tc.procs, 42); got != tc.exited {
				t.Fatalf("exited=%v, want %v", got, tc.exited)
			}
		})
	}
}

func TestDarwinGroupKillBeforeWait(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	DarwinProcessGroup{}.Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if err := (DarwinProcessGroup{}).Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := (DarwinProcessGroup{}).Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("missing child exit status")
	}
	if err := (DarwinProcessGroup{}).Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
}
