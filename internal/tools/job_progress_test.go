package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunningProgressAndDuplicateJobs(t *testing.T) {
	tool := execTestTool(t)
	defer closeSessions(tool.Sessions)
	dir := t.TempDir()
	first := callTool(t, tool, map[string]any{"command": "printf ready; sleep 8", "workdir": dir, "yield_ms": 250})
	s := tool.Sessions.List("")[0]
	for _, want := range []string{"STILL RUNNING", string(s.ID), "elapsed:", "last_output_age:", "output_bytes: 5"} {
		if !strings.Contains(first.Output, want) {
			t.Fatalf("missing %s: %s", want, first.Output)
		}
	}
	ioTool := &ExecIOTool{Sessions: tool.Sessions}
	for _, action := range []string{"read", "wait_for"} {
		res := callTool(t, ioTool, map[string]any{"action": action, "session": string(s.ID), "pattern": "never", "timeout_s": 0.25})
		if !strings.Contains(res.Output, "STILL RUNNING") || !strings.Contains(res.Output, string(s.ID)) {
			t.Fatal(res.Output)
		}
	}
	second := callTool(t, tool, map[string]any{"command": "printf ready; sleep 8", "workdir": dir, "yield_ms": 250})
	if !strings.HasPrefix(second.Output, "exit: ") {
		t.Fatal("duplicate warning displaced the exit header", second.Output)
	}
	if strings.Count(second.Output, "warning: command duplicates") != 1 {
		t.Fatal(second.Output)
	}
	// Same outputs with different command lines still receive a warning.
	callTool(t, tool, map[string]any{"command": "sleep 8 > shared.txt", "workdir": dir, "yield_ms": 250})
	duplicate := callTool(t, tool, map[string]any{"command": "printf other > shared.txt", "workdir": dir})
	if !strings.Contains(duplicate.Output, "warning: command duplicates") {
		t.Fatal(duplicate.Output)
	}
	t.Setenv("EXCALIBUR_LIVE_JOB_DUPLICATES", "off")
	if warning := tool.Sessions.duplicateJobWarning("sleep 8 > shared.txt", dir); warning != "" {
		t.Fatal(warning)
	}
	t.Setenv("EXCALIBUR_LIVE_JOB_PROGRESS", "off")
	if s.runningProgress() != "" {
		t.Fatal("disabled progress present")
	}
}

func TestJobOutputPaths(t *testing.T) {
	for _, cmd := range []string{"cc main.c -o 'result.bin'", "tool --output=result.bin", "tool > result.bin"} {
		if !jobOutputPaths(cmd, "/work")["/work/result.bin"] {
			t.Fatal(cmd)
		}
	}
	s := &Session{ID: "sess-1", Started: time.Now().Add(-time.Second)}
	if !strings.Contains(s.runningProgress(), "no output yet") {
		t.Fatal(s.runningProgress())
	}
}

func TestSessionCompletionReportsCompilerExit(t *testing.T) {
	tool := execTestTool(t)
	defer closeSessions(tool.Sessions)
	dir := t.TempDir()
	// Hold the command until the initial receipt is observed. A fixed sleep can
	// finish before the yield timer is scheduled under race instrumentation.
	first := callTool(t, tool, map[string]any{"command": "while [ ! -f release ]; do sleep 0.01; done; exit 7", "workdir": dir, "yield_ms": 250})
	if !strings.Contains(first.Output, "STILL RUNNING") {
		t.Fatal(first.Output)
	}
	session := tool.Sessions.List("")[0]
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	<-session.done
	io := &ExecIOTool{Sessions: tool.Sessions}
	res := callTool(t, io, map[string]any{"action": "read", "session": string(session.ID)})
	if res.ExitCode == nil || *res.ExitCode != 7 || !strings.Contains(res.Output, "exit: 7") {
		t.Fatal(res)
	}
}
