//go:build darwin

package tools

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func darwinTestPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err == nil && pid > 0 {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child did not publish its pid")
	return 0
}

func darwinTestStopped(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err == unix.EIO || err == unix.ESRCH || err == unix.ENOENT || (err == nil && info.Proc.P_stat == 5) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant %d survived process group termination", pid)
}

func TestDarwinGroupSignalReachesGrandchild(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(strconv.FormatBool(force), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "grandchild.pid")
			cmd := exec.Command("/bin/sh", "-c", `/bin/sh -c 'sleep 30 & echo $! > "$PID_FILE"; wait' & wait`)
			cmd.Env = append(os.Environ(), "PID_FILE="+path)
			cmd.SysProcAttr = sysProcAttr()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = signalGroup(cmd.Process.Pid, true); _ = cmd.Wait() }()
			pid := darwinTestPID(t, path)
			defer unix.Kill(pid, unix.SIGKILL)
			info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
			if err != nil || int(info.Eproc.Pgid) != cmd.Process.Pid || int(info.Eproc.Ppid) == cmd.Process.Pid {
				t.Fatalf("expected a grandchild in own group: %+v %v", info, err)
			}
			if !groupAlive(cmd.Process.Pid) {
				t.Fatal("live group reported dead")
			}
			found := false
			for _, p := range readProcessTable().tree(cmd.Process.Pid) {
				found = found || p.PID == pid
			}
			if !found {
				t.Fatal("grandchild missing from process tree")
			}
			if err := signalGroup(cmd.Process.Pid, force); err != nil {
				t.Fatal(err)
			}
			darwinTestStopped(t, pid)
		})
	}
}

// The test executable acts as a disposable harness in its own process group.
func TestDarwinDetachHelper(t *testing.T) {
	path := os.Getenv("EXCALIBUR_DARWIN_DETACH_PID")
	if path == "" {
		return
	}
	child := exec.Command("/bin/sh", "-c", `echo $$ > "$EXCALIBUR_DARWIN_DETACH_PID"; exec /bin/sleep 30`)
	child.Env = os.Environ()
	child.SysProcAttr = detachedSysProcAttr()
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	time.Sleep(30 * time.Second)
	_ = child.Process.Kill()
	_ = child.Wait()
	os.Exit(0)
}

func TestDarwinDetachedSessionSurvivesParentGroupKill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detached.pid")
	parent := exec.Command(os.Args[0], "-test.run=^TestDarwinDetachHelper$")
	parent.Env = append(os.Environ(), "EXCALIBUR_DARWIN_DETACH_PID="+path)
	parent.SysProcAttr = sysProcAttr()
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = signalGroup(parent.Process.Pid, true); _ = parent.Wait() }()
	child := darwinTestPID(t, path)
	defer signalGroup(child, true)
	pgid, err := syscall.Getpgid(child)
	if err != nil || pgid != child {
		t.Fatalf("detached pgid=%d, child=%d: %v", pgid, child, err)
	}
	if err := signalGroup(parent.Process.Pid, true); err != nil {
		t.Fatal(err)
	}
	if err := parent.Wait(); err == nil {
		t.Fatal("parent harness survived kill")
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", child)
	if err != nil || info.Proc.P_stat == 5 || !groupAlive(child) {
		t.Fatalf("detached child lost with harness: %+v %v", info, err)
	}
	if processHoldsTerminal(child) {
		t.Fatal("detached child retained controlling terminal")
	}
}

func TestDarwinPTYHasControllingTerminal(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	master, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = signalGroup(cmd.Process.Pid, true); _ = cmd.Wait(); _ = master.Close() }()
	if !processHoldsTerminal(cmd.Process.Pid) {
		t.Fatal("PTY process lacks controlling terminal")
	}
}

func TestDarwinListenerSelectionExcludesOtherTrees(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if !processTreeListeningTCP(os.Getpid()) {
		t.Fatal("control listener missing")
	}
	cmd := exec.Command("/bin/sleep", "30")
	cmd.SysProcAttr = sysProcAttr()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = signalGroup(cmd.Process.Pid, true); _ = cmd.Wait() }()
	if processTreeListeningTCP(cmd.Process.Pid) {
		t.Fatal("non-listening tree matched another process's sockets")
	}
}
