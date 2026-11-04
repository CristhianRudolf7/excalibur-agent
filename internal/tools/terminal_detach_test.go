package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Detached background sessions (W10 run-end services) must carry a pid
// and a log file under /tmp, appear in Surviving, and keep the switch
// default ON.
func TestDetachedBackgroundSurvives(t *testing.T) {
	t.Setenv(DetachBackgroundSwitch, "on")
	if !DetachBackgroundEnabled() {
		t.Fatal("detach default must be ON")
	}
	m := NewSessionManager(TerminalDeps{})
	s, out, alive := m.StartDetached("e1", "echo detached-hello; sleep 5", "", nil)
	if !alive {
		t.Fatalf("detached must be alive after yield, out=%q", out)
	}
	if !s.Detached {
		t.Fatal("detached session must be marked detached")
	}
	if s.Pid <= 0 {
		t.Fatalf("detached pid must be positive, got %d", s.Pid)
	}
	if s.LogPath == "" || !strings.HasPrefix(s.LogPath, os.TempDir()) {
		t.Fatalf("detached log must sit under /tmp, got %q", s.LogPath)
	}
	if _, err := os.Stat(s.LogPath); err != nil {
		t.Fatalf("detached log must exist: %v", err)
	}
	surv := m.Surviving()
	if len(surv) != 1 || surv[0].Pid != s.Pid {
		t.Fatalf("Surviving must list the detached pid, got %+v", surv)
	}
	// Poll via the tailed log: the echo must become visible.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, _, _, err := m.WaitFor(ctx, s.ID, "detached-hello", 8*time.Second, 0)
	if err != nil {
		t.Fatalf("wait_for detached output: %v (out=%q)", err, got)
	}
	if !strings.Contains(got, "detached-hello") {
		t.Fatalf("detached tail must show output, got %q", got)
	}
	_ = m.Close(s.ID)
}

func TestDetachSwitchOffRestoresForeground(t *testing.T) {
	t.Setenv(DetachBackgroundSwitch, "0")
	if DetachBackgroundEnabled() {
		t.Fatal("explicit off must disable detach")
	}
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.StartDetached("e1", "echo hi", "", nil)
	if s.Detached {
		t.Fatal("switch off must fall back to foreground (not detached)")
	}
	_ = m.Close(s.ID)
}

func TestExecBackgroundUsesDetached(t *testing.T) {
	t.Setenv(DetachBackgroundSwitch, "on")
	m := NewSessionManager(TerminalDeps{})
	tool := &ExecTool{Sessions: m}
	raw, _ := json.Marshal(map[string]any{"command": "echo bg-hello; sleep 5", "background": true, "yield_ms": 20})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("background exec: %+v", res)
	}
	if !strings.Contains(res.Output, "alive: true") {
		t.Fatalf("background must stay alive, got %q", res.Output)
	}
	if !strings.Contains(res.Output, "detached service") || !strings.Contains(res.Output, "survives run end") {
		t.Fatalf("background output must name detached survival, got %q", res.Output)
	}
	for _, s := range m.List("") {
		_ = m.Close(s.ID)
	}
}

func TestFinishMentionsKeepRunning(t *testing.T) {
	d := (&FinishTool{}).Definition()
	if !strings.Contains(d.Description, "keep_running") {
		t.Fatalf("finish description must carry keep_running guidance, got %q", d.Description)
	}
	if !strings.Contains(strings.ToLower(d.Description), "killing") {
		t.Fatalf("finish description must teach that killing a required process is a failure, got %q", d.Description)
	}
}

// Darwin replaces only the HTTP fixture's slow hostname lookup. Other
// platforms retain the original command and the same survival assertions.
var detachedTestServerCommand = func(port int) string {
	return fmt.Sprintf("python3 -m http.server %d --bind 127.0.0.1", port)
}

// The run-end proof: a detached HTTP server started by a helper that
// immediately exits still answers afterwards. The helper is this test
// binary re-executed with a marker env var; it starts the detached
// service via StartDetached and exits without cleanup, simulating
// `excalibur exec` returning. Survival proves setsid + no Pdeathsig +
// /tmp log (W10).
func TestDetachedSurvivesParentExit(t *testing.T) {
	if os.Getenv("EXCALIBUR_DETACH_HELPER") == "1" {
		port, _ := strconv.Atoi(os.Getenv("EXCALIBUR_DETACH_PORT"))
		workdir := os.Getenv("EXCALIBUR_DETACH_WORKDIR")
		m := NewSessionManager(TerminalDeps{})
		_, _, alive := m.StartDetached("helper", detachedTestServerCommand(port), workdir, nil)
		if !alive {
			os.Exit(2)
		}
		// Give the server a moment to bind, then exit without
		// cleanup: the detached child must outlive us.
		time.Sleep(1500 * time.Millisecond)
		os.Exit(0)
	}
	if testing.Short() {
		t.Skip("parent-exit survival needs subprocesses")
	}
	t.Setenv(DetachBackgroundSwitch, "on")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	workdir := t.TempDir()
	prepareDetachedServerCleanup(t, workdir)
	cmd := exec.Command(os.Args[0], "-test.run=TestDetachedSurvivesParentExit", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"EXCALIBUR_DETACH_HELPER=1",
		"EXCALIBUR_DETACH_PORT="+strconv.Itoa(port),
		"EXCALIBUR_DETACH_WORKDIR="+workdir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper must exit 0, got %v: %s", err, string(out))
	}
	// The helper is gone; the detached server must still answer.
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("detached server on %d must survive helper exit: %v", port, lastErr)
	}
	// Cleanup: the log names the pid; best-effort kill by port owner.
	if out, err := exec.Command("sh", "-c", fmt.Sprintf("ss -ltnp 2>/dev/null | grep ':%d ' || true", port)).CombinedOutput(); err == nil {
		_ = out
	}
	// Kill via fuser/pkill fallback: find python http.server on port.
	_ = syscall.SIGTERM
	// Best effort: pkill the exact server command.
	_ = exec.Command("pkill", "-f", fmt.Sprintf("http.server %d", port)).Run()
	time.Sleep(300 * time.Millisecond)
}

func TestQuickBackgroundReturnsCompletedOutput(t *testing.T) {
	t.Setenv("EXCALIBUR_BACKGROUND_YIELD", "on")
	for _, code := range []int{0, 7} {
		m := NewSessionManager(TerminalDeps{})
		tool := &ExecTool{Sessions: m}
		raw, _ := json.Marshal(map[string]any{"command": fmt.Sprintf("sleep 0.35; printf 'quick output\\n'; exit %d", code), "background": true, "yield_ms": 1000})
		res, err := tool.Invoke(context.Background(), Invocation{Arguments: raw})
		if err != nil || !strings.Contains(res.Output, "quick output") || !strings.Contains(res.Output, fmt.Sprintf("exit: %d", code)) || !strings.Contains(res.Output, "alive: false") || strings.Contains(res.Output, "detached service") {
			t.Fatalf("completed background: %+v %v", res, err)
		}
		if res.ExitCode == nil || *res.ExitCode != code {
			t.Fatalf("exit status: %+v", res)
		}
	}
}

func TestQuickBackgroundDrainsLargeLog(t *testing.T) {
	t.Setenv("EXCALIBUR_BACKGROUND_YIELD", "on")
	m := NewSessionManager(TerminalDeps{})
	s, out, alive := m.StartDetached("test", "head -c 32768 /dev/zero | tr '\\0' x", "", nil, 1000)
	if alive || s.Detached || out != strings.Repeat("x", 32768) {
		t.Fatalf("incomplete output: alive=%v detached=%v bytes=%d", alive, s.Detached, len(out))
	}
	if code, done := s.Exit(); !done || code != 0 {
		t.Fatalf("exit: %d %v", code, done)
	}
}

func TestBackgroundCompletionWithConcurrentSessionSnapshots(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	stop, done := make(chan struct{}), make(chan struct{})
	safego.Go("test/session-snapshots", nil, func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
			_ = m.Surviving()
			_ = m.ServiceSessions()
		}
	})
	defer func() { close(stop); <-done }()
	for i := 0; i < 5; i++ {
		_, out, alive := m.StartDetached("test", "sleep 0.02; echo complete", "", nil, 1000)
		if alive || !strings.Contains(out, "complete") {
			t.Fatalf("completion: %q %v", out, alive)
		}
	}
}
