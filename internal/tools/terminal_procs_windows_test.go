package tools

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The child is a fresh copy of the test executable, so these tests need no
// installed shell, Python, or external process utilities.
func TestWindowsProcessHelper(t *testing.T) {
	mode := os.Getenv("EXCALIBUR_WINDOWS_PROCESS_TEST")
	if mode == "" {
		return
	}
	if mode == "exit-ctrlc" {
		os.Exit(int(uint32(0xC000013A)))
	}
	if mode == "detached-root" {
		gate := os.Getenv("EXCALIBUR_WINDOWS_PROCESS_GATE")
		log, err := os.Create(gate + ".log")
		if err != nil {
			os.Exit(2)
		}
		c := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
		c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_PROCESS_TEST": "leaf"}, nil)
		c.SysProcAttr = detachedSysProcAttr()
		c.Stdout = log
		c.Stderr = log
		if err := c.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		if err := os.WriteFile(gate+".child", []byte(strconv.Itoa(c.Process.Pid)), 0600); err != nil {
			os.Exit(4)
		}
		c.Process.Release()
		log.Close()
		os.Exit(0)
	}
	if mode == "leaf" {
		fmt.Println("detached output ready")
	}
	if mode == "root" {
		gate := os.Getenv("EXCALIBUR_WINDOWS_PROCESS_GATE")
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		c := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
		c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_PROCESS_TEST": "leaf"}, nil)
		if err := c.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(gate+".child", []byte(strconv.Itoa(c.Process.Pid)), 0600); err != nil {
			os.Exit(3)
		}
	}
	for {
		time.Sleep(time.Second)
	}
}

func startWindowsTestTree(t *testing.T) (*exec.Cmd, func(), int) {
	t.Helper()
	gate := filepath.Join(t.TempDir(), "launch")
	c := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_PROCESS_TEST": "root", "EXCALIBUR_WINDOWS_PROCESS_GATE": gate}, nil)
	if err := os.WriteFile(gate, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	c.SysProcAttr = sysProcAttr()
	prepareSessionProcess(c)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(gate + ".child"); err == nil {
		c.Process.Kill()
		c.Wait()
		t.Fatal("child ran before job assignment")
	}
	cleanup, err := attachSessionProcess(c.Process)
	if err != nil {
		c.Process.Kill()
		c.Wait()
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup(); c.Wait() })
	if err := os.WriteFile(gate, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(gate + ".child"); err == nil && len(raw) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			return c, cleanup, pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("grandchild did not start")
	return nil, nil, 0
}

func TestWindowsJobKillsGrandchild(t *testing.T) {
	for _, action := range []string{"kill", "close", "interrupt"} {
		t.Run(action, func(t *testing.T) {
			c, cleanup, child := startWindowsTestTree(t)
			tree := readProcessTable().tree(c.Process.Pid)
			found := false
			for _, p := range tree {
				if p.PID == child {
					found = true
				}
			}
			if !found {
				t.Fatalf("grandchild %d missing from process tree %v", child, tree)
			}
			switch action {
			case "close":
				cleanup()
			case "kill":
				if err := signalGroup(c.Process.Pid, true); err != nil {
					t.Fatal(err)
				}
			case "interrupt":
				if _, err := signalSessionProcess(c.Process, sigINT()); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) && (processAlive(child) || processAlive(c.Process.Pid)) {
				time.Sleep(10 * time.Millisecond)
			}
			if processAlive(child) || processAlive(c.Process.Pid) {
				t.Fatalf("%s left root or grandchild alive", action)
			}
			if _, err := signalSessionProcess(c.Process, sigKILL()); err != nil {
				t.Fatalf("already dead: %v", err)
			}
		})
	}
}
func TestWindowsExitCodesHaveNoSignal(t *testing.T) {
	for _, script := range []string{"exit /b 137", "exit /b 1"} {
		shell := defaultShell()
		c := exec.Command(shell, shellArgs(shell, script)...)
		prepareShellCommand(c, shell, script)
		err := c.Run()
		s := &Session{exited: true, exitErr: err}
		code, signal, done := sessionExit(s)
		if !done || signal != "" || code == 0 {
			t.Fatalf("%s: code=%d signal=%s done=%v", script, code, signal, done)
		}
	}
}

func TestWindowsDetachedSurvivesParent(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "detached")
	c := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_PROCESS_TEST": "detached-root", "EXCALIBUR_WINDOWS_PROCESS_GATE": gate}, nil)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("parent: %v: %s", err, out)
	}
	raw, err := os.ReadFile(gate + ".child")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer signalGroup(pid, true)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if out, err := os.ReadFile(gate + ".log"); err == nil && strings.Contains(string(out), "detached output ready") {
			if !processAlive(pid) {
				t.Fatal("detached process died with its parent")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("detached output did not reach the log")
}

func TestWindowsControlCExitCode(t *testing.T) {
	c := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_PROCESS_TEST": "exit-ctrlc"}, nil)
	s := &Session{exited: true, exitErr: c.Run()}
	code, signal, done := sessionExit(s)
	if !done || signal != "" || code != int(uint32(0xC000013A)) {
		t.Fatalf("code=%d signal=%q done=%v", code, signal, done)
	}
}

func TestWindowsCompletedSessionSignals(t *testing.T) {
	manager := NewSessionManager(TerminalDeps{})
	session, _, _ := manager.Start("windows-test", "echo complete", "", false, 1000, nil)
	defer manager.Close(session.ID)
	select {
	case <-session.done:
	case <-time.After(10 * time.Second):
		t.Fatal("session did not exit")
	}
	for _, signal := range []string{"interrupt", "terminate", "kill"} {
		if err := manager.Signal(session.ID, signal); err != nil {
			t.Fatalf("%s: %v", signal, err)
		}
	}
}

func TestWindowsDetachedSessionLog(t *testing.T) {
	manager := NewSessionManager(TerminalDeps{})
	session, _, _ := manager.StartDetached("windows-test", "echo detached-output", "", nil, 1000)
	defer manager.Close(session.ID)
	defer os.Remove(session.LogPath)
	select {
	case <-session.done:
	case <-time.After(10 * time.Second):
		t.Fatal("detached session did not exit")
	}
	code, _ := session.Exit()
	if code != 0 {
		out, _ := session.ReadSince(0)
		t.Fatalf("exit=%d: %s", code, out)
	}
	out, _ := session.ReadSince(0)
	if !strings.Contains(out, "detached-output") {
		t.Fatal(out)
	}
}

func TestWindowsProcessCommandLine(t *testing.T) {
	line := windowsProcessCommandLine(uint32(os.Getpid()))
	if !strings.Contains(strings.ToLower(line), strings.ToLower(filepath.Base(os.Args[0]))) {
		t.Fatalf("command line did not contain this executable: %q", line)
	}
}

func TestWindowsProcessTreeListeningTCP(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if !processTreeListeningTCP(os.Getpid()) {
		t.Fatal("listener ownership missing from process tree")
	}
}

func TestWindowsAutonomousPipeUsesSurvivingLog(t *testing.T) {
	restore := SetExecScope(ExecScope{Active: true})
	defer restore()
	t.Setenv("EXCALIBUR_SURVIVE_SESSIONS", "1")
	manager := NewSessionManager(TerminalDeps{})
	session, _, _ := manager.Start("windows-test", "echo surviving-output", "", false, 1000, nil)
	defer manager.Close(session.ID)
	defer os.Remove(session.LogPath)
	if !session.Survivable || session.LogPath == "" {
		t.Fatal("autonomous pipe did not detach into a log")
	}
	select {
	case <-session.done:
	case <-time.After(10 * time.Second):
		t.Fatal("surviving pipe did not exit")
	}
	out, _ := session.ReadSince(0)
	code, _ := session.Exit()
	if code != 0 || !strings.Contains(out, "surviving-output") {
		t.Fatalf("exit=%d: %s", code, out)
	}
}
