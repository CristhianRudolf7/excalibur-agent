package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func earlyOutputRun(t *testing.T) (*Engine, *Run, string) {
	t.Helper()
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Workdir = t.TempDir()
	var err error
	run.fin.snapshot, err = tasks.TakeInputSnapshot(run.Workdir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	run.Request = "Write the required output to `" + filepath.Join(run.Workdir, "result.csv") + "`."
	run.Profile.WallSource = "explicit"
	run.Tracker.Limit.MaxSeconds = 100
	return e, run, filepath.Join(run.Workdir, "result.csv")
}

func TestEarlyDeliverablesGuidanceScopeAndSwitch(t *testing.T) {
	for _, tc := range []struct {
		name, enabled    string
		autonomous, want bool
	}{
		{"default", "", true, true}, {"disabled", "off", true, false}, {"assistant", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(earlyDeliverablesSwitch, tc.enabled)
			run := testRun(Profile{ExplicitFinish: tc.autonomous})
			b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: testSnapshotFunc()}
			_, compiled, err := b.BuildRequest(context.Background(), run, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(compiled.LayerText(ctxcompiler.LayerTask), earlyDeliverablesGuidance); got != tc.want {
				t.Fatalf("guidance present=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestEarlyDeliverablesHalfwayNoteOnce(t *testing.T) {
	e, run, output := earlyOutputRun(t)
	start := run.StartedAt
	e.earlyDeliverablesCheck(run, start.Add(49*time.Second))
	if len(run.VolatileNotes()) != 0 {
		t.Fatal(run.VolatileNotes())
	}
	e.earlyDeliverablesCheck(run, start.Add(50*time.Second))
	if notes := run.VolatileNotes(); len(notes) != 1 || notes[0] != earlyDeliverablesNote {
		t.Fatal(notes)
	}
	// Removing the note does not reset the one-time reminder.
	run.InjectControlNote("budget.early_deliverables", "")
	e.earlyDeliverablesCheck(run, start.Add(70*time.Second))
	if len(run.VolatileNotes()) != 0 {
		t.Fatal("halfway note repeated")
	}
	if err := os.WriteFile(output, []byte("value\n1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	e.earlyDeliverablesCheck(run, start.Add(80*time.Second))
	if len(run.VolatileNotes()) != 0 {
		t.Fatal(run.VolatileNotes())
	}
}

func TestEarlyDeliverablesPacingConditions(t *testing.T) {
	for _, name := range []string{"disabled", "assistant", "assumed", "uncapped", "present", "no-required-output", "empty-placeholder"} {
		t.Run(name, func(t *testing.T) {
			e, run, output := earlyOutputRun(t)
			switch name {
			case "disabled":
				t.Setenv(earlyDeliverablesSwitch, "0")
			case "assistant":
				run.Profile.ExplicitFinish = false
			case "assumed":
				run.Profile.WallSource = "default"
				t.Setenv(TimeReserveExplicitOnlySwitch, "0")
			case "uncapped":
				run.Tracker.Limit.MaxSeconds = 0
			case "present":
				if err := os.WriteFile(output, []byte("value\n1\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "empty-placeholder":
				if err := os.WriteFile(output, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "no-required-output":
				run.Request = "Explain how this project is organized."
			}
			e.earlyDeliverablesCheck(run, run.StartedAt.Add(60*time.Second))
			want := name == "empty-placeholder"
			if got := strings.Contains(strings.Join(run.VolatileNotes(), "\n"), earlyDeliverablesNote); got != want {
				t.Fatalf("note present=%v want=%v: %v", got, want, run.VolatileNotes())
			}
		})
	}
}

func TestEarlyDeliverablesStepInjectsControlNote(t *testing.T) {
	e, run, _ := earlyOutputRun(t)
	runner := scriptedRunner()
	runner.add("read_file", types.EffectReadOnly, types.IdemIdempotent)
	e.deps.Tools = runner
	e.deps.Models = &fakeEndpoint{responses: []providers.ChatResponse{toolResp(toolCall("read", "read_file", `{"path":"README.md"}`))}}
	_, js := testEngine(&fakeEndpoint{}, runner, passingGate())
	startRun(t, e, run, js)
	e.deps.Clock = func() time.Time { return run.StartedAt.Add(50 * time.Second) }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := e.step(ctx, run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(run.VolatileNotes(), "\n"), earlyDeliverablesNote) {
		t.Fatal(run.VolatileNotes())
	}
}

func addEarlySession(run *Run, command string) {
	raw, _ := json.Marshal(map[string]string{"command": command})
	run.History = append(run.History, ctxcompiler.ActivityEvent{Seq: 1, Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "producer", Name: "exec", Arguments: raw}}})
	run.History = append(run.History, historyEventForTool(2, providers.ToolCall{ID: "producer", Name: "exec", Arguments: raw},
		tools.Result{Status: tools.StatusOK, Output: "exit: running  session: producer-1  alive: true", Sessions: []tools.SessionReceipt{{ID: "producer-1", Alive: true}}}))
}

func TestEarlyProducerStopResultWarning(t *testing.T) {
	for _, tc := range []struct{ name, tool, args string }{
		{"signal", "exec_io", `{"action":"signal","session":"producer-1","signal":"terminate"}`},
		{"default-signal", "exec_io", `{"action":"signal","session":"producer-1"}`},
		{"interrupt", "exec_io", `{"action":"signal","session":"producer-1","signal":"interrupt"}`},
		{"close", "exec_io", `{"action":"close","session":"producer-1"}`},
		{"keys", "exec_io", `{"action":"keys","session":"producer-1","keys":["ctrl-c"]}`},
		{"write-interrupt", "exec_io", `{"action":"write","session":"producer-1","text":"\u0003"}`},
		{"kill", "exec", `{"command":"kill -TERM 1234"}`},
		{"kill-group", "exec", `{"command":"kill -9 -- -1234"}`},
		{"pkill", "exec", `{"command":"pkill -f 'python3 producer.py'"}`},
		{"killall", "exec", `{"command":"killall python3"}`},
		{"wrapped", "call_tool", `{"name":"exec_io","arguments":{"action":"signal","session":"producer-1","signal":"kill"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, run, _ := earlyOutputRun(t)
			script := filepath.Join(run.Workdir, "producer.py")
			if err := os.WriteFile(script, []byte("open('result.csv', 'w').write('value\\n1\\n')\n"), 0600); err != nil {
				t.Fatal(err)
			}
			addEarlySession(run, "python3 producer.py")
			e.deps.ActiveSessions = func() []tools.SessionInfo {
				return []tools.SessionInfo{{ID: "producer-1", PID: 1234, Alive: true, Command: "python3 producer.py"}}
			}
			runner := scriptedRunner()
			runner.add(tc.tool, types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "stop sent"})
			e.deps.Tools = runner
			res := e.runOne(context.Background(), run, toolCall("stop", tc.tool, tc.args))
			if res.Status != tools.StatusOK || !strings.Contains(res.Output, "warning: stopping session producer-1") || !strings.HasPrefix(res.Output, "stop sent\n") {
				t.Fatalf("result=%+v", res)
			}
			if runner.ncalls() != 1 {
				t.Fatal("stop was blocked")
			}
		})
	}
}

func TestEarlyProducerStopWarningConditions(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		want          bool
	}{
		{"direct-output", "pkill -f result.csv", true},
		{"nonstop-signal", "kill -CONT 1234", false},
		{"query", "kill -0 1234", false},
		{"other-pid", "kill 4321", false},
		{"quoted-text", "echo 'kill 1234'", false},
		{"unrelated", "pkill -f other.py", false},
		{"process-name-pattern", "pkill producer.py", false},
		{"wrapped-echo", "env echo kill 1234", false},
		{"user-signal", "kill -USR1 1234", false},
		{"dead", "kill 1234", false},
		{"other-run", "kill 1234", false},
		{"present", "kill 1234", false},
		{"disabled", "kill 1234", false},
		{"assistant", "kill 1234", false},
		{"no-producer-link", "kill 1234", false},
		{"named-script", "kill 1234", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, run, output := earlyOutputRun(t)
			producer := "python3 producer.py --output result.csv"
			if tc.name == "no-producer-link" || tc.name == "named-script" {
				producer = "python3 producer.py"
			}
			if tc.name == "named-script" {
				run.Request += " Run producer.py to create `" + output + "`."
			}
			addEarlySession(run, producer)
			session := tools.SessionInfo{ID: "producer-1", PID: 1234, Alive: tc.name != "dead", Command: producer}
			if tc.name == "other-run" {
				session.ID = "another-session"
			}
			e.deps.ActiveSessions = func() []tools.SessionInfo { return []tools.SessionInfo{session} }
			if tc.name == "present" {
				if err := os.WriteFile(output, []byte("value\n1\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "disabled" {
				t.Setenv(earlyDeliverablesSwitch, "0")
			}
			if tc.name == "assistant" {
				run.Profile.ExplicitFinish = false
			}
			raw, _ := json.Marshal(map[string]string{"command": tc.command})
			got := e.earlyProducerStopWarning(run, toolCall("stop", "exec", string(raw)))
			if (got != "") != tc.want {
				t.Fatalf("warning=%q want=%v", got, tc.want)
			}
		})
	}
}

func TestEarlyProducerWarningUsesHistoryWhenAuthorityUnavailable(t *testing.T) {
	e, run, _ := earlyOutputRun(t)
	addEarlySession(run, "python3 producer.py --output result.csv")
	warning := e.earlyProducerStopWarning(run, toolCall("stop", "exec_io", `{"action":"signal","session":"producer-1","signal":"kill"}`))
	if !strings.Contains(warning, "live producer") {
		t.Fatal(warning)
	}
}

func TestEarlyProducerWarningDoesNotAnnotateFailedStop(t *testing.T) {
	e, run, _ := earlyOutputRun(t)
	addEarlySession(run, "python3 producer.py --output result.csv")
	runner := scriptedRunner()
	runner.add("exec_io", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusToolError, Output: "signal failed"})
	e.deps.Tools = runner
	res := e.runOne(context.Background(), run, toolCall("stop", "exec_io", `{"action":"signal","session":"producer-1","signal":"kill"}`))
	if res.Status != tools.StatusToolError || res.Output != "signal failed" {
		t.Fatal(res)
	}
}
