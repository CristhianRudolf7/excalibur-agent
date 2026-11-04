//go:build darwin

package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestDarwinDeadlineWithoutGNUTimeout(t *testing.T) {
	for _, mode := range []string{"pipe", "detached", "pty"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SHELL", "/bin/bash")
			t.Setenv("PATH", "/usr/bin:/bin")
			t.Setenv(AutoProvisionEnv, "off")
			t.Setenv(DetachBackgroundSwitch, "1")
			m := NewSessionManager(TerminalDeps{PTY: platform.DarwinPTY{}})
			t.Cleanup(func() { closeSessions(m) })
			// Both the shell and its child ignore TERM, requiring the KILL
			// escalation to end the group and close the child's output pipe.
			command := `trap '' TERM; /bin/sh -c 'trap "" TERM; printf "child-ready\n"; while :; do /bin/sleep 30; done' & wait`
			raw, err := json.Marshal(map[string]any{"command": command, "yield_ms": 250, "background": mode == "detached", "tty": mode == "pty"})
			if err != nil {
				t.Fatal(err)
			}
			tool := &ExecTool{Sessions: m}
			res, err := tool.Invoke(WithCommandDeadline(context.Background(), 500*time.Millisecond), Invocation{Arguments: raw})
			if err != nil || !strings.Contains(res.Output, "child-ready") {
				t.Fatalf("start: %+v, %v", res, err)
			}
			sessions := m.List("default")
			if len(sessions) != 1 {
				t.Fatalf("sessions: %+v", sessions)
			}
			s, _ := m.Get(sessions[0].ID)
			if s.LogPath != "" {
				t.Cleanup(func() { _ = os.Remove(s.LogPath) })
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			if out, _, exited, err := m.WaitFor(ctx, s.ID, "", 6*time.Second, 0); err != nil || !exited || strings.Contains(out, "command not found") {
				t.Fatalf("deadline: exited=%v, %v, %q", exited, err, out)
			}
			if code, exited := s.Exit(); !exited || code != 137 {
				t.Fatalf("TERM-resistant group must be killed: code=%d exited=%v", code, exited)
			}
		})
	}
}

func TestDarwinDeadlineAllowsNaturalExit(t *testing.T) {
	tool := execTestTool(t)
	t.Cleanup(func() { closeSessions(tool.Sessions) })
	res, err := tool.Invoke(WithCommandDeadline(context.Background(), time.Second), Invocation{Arguments: []byte(`{"command":"printf done"}`)})
	if err != nil || res.Status != StatusOK || res.ExitCode == nil || *res.ExitCode != 0 || !strings.HasSuffix(strings.TrimSpace(res.Output), "done") {
		t.Fatalf("natural exit: %+v, %v", res, err)
	}
}
