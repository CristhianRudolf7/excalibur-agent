package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
)

func TestExecPipeYieldAndRead(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, out, alive := m.Start("e1", "echo hello", "", false, 5000, nil)
	if !strings.Contains(out, "hello") {
		t.Fatalf("output: %q", out)
	}
	_ = s
	_ = alive
	// Wait for natural exit.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, _, exited, err := m.WaitFor(ctx, s.ID, "", 10*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !exited || !strings.Contains(got, "hello") {
		t.Fatalf("wait_for exit: %v %q", exited, got)
	}
}

func TestWaitForPatternSingleCall(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("e1", "echo ready-marker-123; sleep 30", "", false, 300, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	out, _, _, err := m.WaitFor(ctx, s.ID, "ready-marker-123", 8*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ready-marker-123") {
		t.Fatalf("pattern: %q", out)
	}
	if time.Since(start) > 7*time.Second {
		t.Fatal("wait_for must return as soon as the pattern appears, not at timeout")
	}
	_ = m.Close(s.ID)
	// Closing terminates the process: no orphans left running.
	if _, ok := m.Get(s.ID); ok {
		t.Fatal("closed session must be gone")
	}
}

func TestSignalAndClose(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, _, alive := m.Start("e1", "sleep 30", "", false, 1000, nil)
	if !alive {
		t.Skip("process exited too fast")
	}
	if err := m.Signal(s.ID, "terminate"); err != nil {
		t.Fatalf("signal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, exited, err := m.WaitFor(ctx, s.ID, "", 8*time.Second, 0); err != nil || !exited {
		t.Fatalf("terminate must end it: %v %v", exited, err)
	}
}

func TestPTYKeysWithFake(t *testing.T) {
	fpty := &platformfake.PTY{}
	m := NewSessionManager(TerminalDeps{PTY: fpty})
	s, _, _ := m.Start("e1", "fake-cmd", "", true, 500, nil)
	if fpty.Started != 1 {
		t.Fatalf("PTY.Start must be used, got %d", fpty.Started)
	}
	if err := m.Keys(s.ID, []string{"enter", "ctrl-c", "up", "a"}); err != nil {
		t.Fatalf("keys: %v", err)
	}
	if fpty.Session.In.Len() == 0 {
		t.Fatal("keys must reach the PTY")
	}
	if err := m.Keys(s.ID, []string{"nope-key"}); err == nil {
		t.Fatal("unknown key must fail")
	}
	// Plain pipe sessions reject keys.
	m2 := NewSessionManager(TerminalDeps{})
	p, _, _ := m2.Start("e1", "echo hi", "", false, 2000, nil)
	if err := m2.Keys(p.ID, []string{"enter"}); err == nil {
		t.Fatal("pipe session must reject keys")
	}
	_ = m.Close(s.ID)
}

func TestSessionEvictionProtectsRecent(t *testing.T) {
	var mu sync.Mutex
	var evicted []string
	m := NewSessionManager(TerminalDeps{OnEvict: func(id SessionID, _, _ string) {
		mu.Lock()
		defer mu.Unlock()
		evicted = append(evicted, string(id))
	}})
	owner := "e1"
	for i := 0; i < maxSessionsPerOwner+4; i++ {
		m.Start(owner, "echo evict-marker", "", false, 250, nil)
	}
	if len(m.List(owner)) > maxSessionsPerOwner {
		t.Fatalf("per-owner cap: %d", len(m.List(owner)))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(evicted) == 0 {
		t.Fatal("owner must be notified of closed sessions")
	}
	// Global cap path.
	mg := NewSessionManager(TerminalDeps{})
	for i := 0; i < maxSessionsGlobal+4; i++ {
		mg.Start("owner", "echo evict-marker", "", false, 250, nil)
	}
	if len(mg.List("")) > maxSessionsGlobal {
		t.Fatalf("global cap: %d", len(mg.List("")))
	}
	for _, s := range mg.List("") {
		_ = mg.Close(s.ID)
	}
	for _, s := range m.List("") {
		_ = m.Close(s.ID)
	}
}

func TestLiveTailBounded(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("e1", "for i in $(seq 1 100); do echo line-$i; done", "", false, 5000, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, _, _ = m.WaitFor(ctx, s.ID, "", 8*time.Second, 0)
	tail := s.LiveTail()
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
	if len(lines) > LiveTailLines {
		t.Fatalf("tail lines: %d", len(lines))
	}
	if len(tail) > LiveTailBytes {
		t.Fatalf("tail bytes: %d", len(tail))
	}
	if !strings.Contains(tail, "line-100") {
		t.Fatalf("tail must show latest: %q", tail)
	}
	_ = m.Close(s.ID)
}

func TestPipeEnvAndShellDetection(t *testing.T) {
	env := pipeEnv(nil)
	found := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			found[k] = v
		}
	}
	if found["PAGER"] != "cat" || found["GIT_PAGER"] != "cat" {
		t.Fatalf("pager env: %v", found)
	}
	// Non-interactive defaults: nothing may prompt on a headless
	// session (Section 13.5).
	for k, want := range map[string]string{
		"GIT_TERMINAL_PROMPT": "0", "DEBIAN_FRONTEND": "noninteractive",
	} {
		if found[k] != want {
			t.Fatalf("%s = %q, want %q", k, found[k], want)
		}
	}
	t.Setenv(NonInteractiveEnvSwitch, "0")
	// The go command may export GIT_TERMINAL_PROMPT to test processes; the
	// switch-off check is about what pipeEnv adds, not the inherited value.
	t.Setenv("GIT_TERMINAL_PROMPT", "")
	os.Unsetenv("GIT_TERMINAL_PROMPT")
	off := map[string]string{}
	for _, kv := range pipeEnv(nil) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			off[k] = v
		}
	}
	if _, ok := off["GIT_TERMINAL_PROMPT"]; ok {
		t.Fatal("switch off must restore plain pager-only env")
	}
	if off["PAGER"] != "cat" {
		t.Fatal("pager defaults stay even with the switch off")
	}
	if sh := DetectShell(); sh == "" {
		t.Fatal("shell detection must return something")
	}
	prof := DetectEnv()
	if prof.OS == "" || prof.DefaultShell == "" {
		t.Fatalf("env profile: %+v", prof)
	}
}

func TestExecToolLegacyTargetIgnored(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	tool := &ExecTool{Sessions: m}
	raw, _ := json.Marshal(map[string]string{"command": "echo hi", "target": "node:abc"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK || !strings.Contains(res.Output, "target ignored: commands run locally") {
		t.Fatalf("legacy target must be ignored locally: %+v", res)
	}
	if !strings.Contains(res.Output, "exit: 0") {
		t.Fatalf("exec must report the exit code up front: %q", res.Output)
	}
}

func TestExecIOListReadSignalClose(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	execTool := &ExecTool{Sessions: m}
	ioTool := &ExecIOTool{Sessions: m}
	raw, _ := json.Marshal(map[string]any{"command": "echo io-hello; sleep 20", "owner": "e9", "yield_ms": 500})
	res, _ := execTool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK || !strings.Contains(res.Output, "session:") {
		t.Fatalf("exec: %+v", res)
	}
	sid := string(m.List("e9")[0].ID)
	lraw, _ := json.Marshal(map[string]string{"action": "list", "owner": "e9"})
	lres, _ := ioTool.Invoke(context.Background(), Invocation{Arguments: lraw})
	if !strings.Contains(lres.Output, sid) || !strings.Contains(lres.Output, "tail:") {
		t.Fatalf("list with live tail: %q", lres.Output)
	}
	rraw, _ := json.Marshal(map[string]any{"action": "read", "session": sid, "offset": 0})
	rres, _ := ioTool.Invoke(context.Background(), Invocation{Arguments: rraw})
	if !strings.Contains(rres.Output, "io-hello") {
		t.Fatalf("read: %q", rres.Output)
	}
	wraw, _ := json.Marshal(map[string]any{"action": "wait_for", "session": sid, "pattern": "io-hello", "timeout_s": 5})
	wres, _ := ioTool.Invoke(context.Background(), Invocation{Arguments: wraw})
	if wres.Status != StatusOK {
		t.Fatalf("wait_for: %+v", wres)
	}
	sraw, _ := json.Marshal(map[string]any{"action": "signal", "session": sid, "signal": "terminate"})
	if sres, _ := ioTool.Invoke(context.Background(), Invocation{Arguments: sraw}); sres.Status != StatusOK {
		t.Fatalf("signal: %+v", sres)
	}
	craw, _ := json.Marshal(map[string]any{"action": "close", "session": sid})
	if cres, _ := ioTool.Invoke(context.Background(), Invocation{Arguments: craw}); cres.Status != StatusOK {
		t.Fatalf("close: %+v", cres)
	}
}

func TestDeadlineStopsSessionAndPreservesShellQuoting(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("EXCALIBUR_AUTOPROVISION", "off")
	m := NewSessionManager(TerminalDeps{})
	tool := &ExecTool{Sessions: m}
	ctx := WithCommandDeadline(context.Background(), 300*time.Millisecond)
	raw, _ := json.Marshal(map[string]any{"command": "[[ 1 == 1 ]] && printf '%s' \"quoted ' value\"; sleep 30", "yield_ms": 250})
	res, err := tool.Invoke(ctx, Invocation{Arguments: raw})
	if err != nil || !strings.Contains(res.Output, "quoted ' value") {
		t.Fatal(res, err)
	}
	sessions := m.List("default")
	if len(sessions) != 1 {
		t.Fatal(sessions)
	}
	s := sessions[0]
	defer m.Close(s.ID)
	wait, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, exited, err := m.WaitFor(wait, s.ID, "", 3*time.Second, 0)
	if err != nil || !exited {
		t.Fatalf("long command survived closing bound: %v %v", exited, err)
	}
}

func TestDeadlineCancellationReleasesInitialWait(t *testing.T) {
	t.Setenv("EXCALIBUR_AUTOPROVISION", "off")
	m := NewSessionManager(TerminalDeps{})
	tool := &ExecTool{Sessions: m}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := tool.Invoke(WithCommandDeadline(ctx, 0), Invocation{Arguments: []byte(`{"command":"sleep 30","yield_ms":30000}`)})
	elapsed := time.Since(start)
	for _, s := range m.List("default") {
		_ = m.Close(s.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("cancellation waited for the full yield: %s", res.Output)
	}
}
