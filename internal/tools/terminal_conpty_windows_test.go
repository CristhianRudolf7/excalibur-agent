package tools

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func TestWindowsTTYSessionCmdEcho(t *testing.T) {
	// Select cmd even on runners with Git Bash or PowerShell on PATH.
	t.Setenv("SHELL", "")
	t.Setenv("PATH", filepath.Dir(defaultShell()))
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	m := NewSessionManager(TerminalDeps{PTY: platform.WindowsPTY{}, Group: platform.WindowsProcessGroup{}})
	s, _, _ := m.Start("conpty-test", "cmd /d /c echo hi", "", true, 1000, nil)
	defer m.Close(s.ID)
	select {
	case <-s.done:
	case <-time.After(15 * time.Second):
		t.Fatal("tty session did not finish")
	}
	out, _ := s.ReadSince(0)
	code, _ := s.Exit()
	if code != 0 || !strings.Contains(out, "hi") {
		t.Fatalf("tty session exit=%d output=%q", code, out)
	}
}

func TestWindowsConPTYCallerOwnsJobAndResume(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "launch")
	c := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_PROCESS_TEST": "root", "EXCALIBUR_WINDOWS_PROCESS_GATE": gate}, nil)
	if err := os.WriteFile(gate, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	prepareSessionProcess(c)
	pty, err := (platform.WindowsPTY{}).Start(c, platform.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer pty.Close()
	safego.Go("conpty-test-drain", nil, func() { _, _ = io.Copy(io.Discard, pty) })
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(gate + ".child"); !os.IsNotExist(err) {
		t.Fatal("ConPTY resumed before tools attached its job")
	}
	release, err := attachSessionProcess(c.Process)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	child := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(gate + ".child"); err == nil {
			child, err = strconv.Atoi(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("grandchild did not start after tools resumed the session")
	}
	if err := signalGroup(c.Process.Pid, true); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (processAlive(child) || processAlive(c.Process.Pid)) {
		time.Sleep(10 * time.Millisecond)
	}
	if processAlive(child) || processAlive(c.Process.Pid) {
		t.Fatal("tools job left a ConPTY process or grandchild alive")
	}
	_ = pty.Wait()
}

func TestWindowsConPTYVerbatimCmdLine(t *testing.T) {
	shell := defaultShell()
	c := exec.Command(shell, "ignored")
	// A quoted executable path and shell metacharacters need cmd's own parser.
	script := `echo "two words" & echo second-command`
	prepareShellCommand(c, shell, script)
	pty, err := (platform.WindowsPTY{}).Start(c, platform.Winsize{Rows: 24, Cols: 160})
	if err != nil {
		t.Fatal(err)
	}
	defer pty.Close()
	done := make(chan error, 1)
	safego.Go("conpty-test-cmdline", nil, func() {
		out, err := io.ReadAll(pty)
		if err == nil && (!strings.Contains(string(out), `"two words"`) || !strings.Contains(string(out), "second-command")) {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			err = pty.Wait()
		}
		done <- err
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("verbatim command did not finish")
	}
}
