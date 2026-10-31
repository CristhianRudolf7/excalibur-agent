package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestExitCodeMapping(t *testing.T) {
	cases := map[types.RunState]int{
		types.RunCompleted:        ExitOK,
		types.RunCompletedPending: ExitPending,
		types.RunBlocked:          ExitBlocked,
		types.RunPaused:           ExitPaused,
		types.RunFailed:           ExitFailed,
		types.RunCanceled:         ExitCanceled,
		types.RunRunning:          ExitError,
	}
	for state, want := range cases {
		if got := CodeForRunState(state); got != want {
			t.Fatalf("CodeForRunState(%q) = %d, want %d", state, got, want)
		}
	}
	taskCases := map[types.TaskState]int{
		types.TaskCompleted:        ExitOK,
		types.TaskCompletedPending: ExitPending,
		types.TaskBlocked:          ExitBlocked,
		types.TaskPaused:           ExitPaused,
		types.TaskFailed:           ExitFailed,
		types.TaskCanceled:         ExitCanceled,
		types.TaskInProgress:       ExitError,
	}
	for state, want := range taskCases {
		if got := CodeForTaskState(state); got != want {
			t.Fatalf("CodeForTaskState(%q) = %d, want %d", state, got, want)
		}
	}
	if ExitCode(nil) != ExitOK {
		t.Fatal("nil must map to 0")
	}
	if ExitCode(&CodedError{Code: ExitFailed, Err: context.DeadlineExceeded}) != ExitFailed {
		t.Fatal("CodedError must win")
	}
	if ExitCode(context.DeadlineExceeded) != ExitError {
		t.Fatal("plain errors are harness errors")
	}
}

// TestExecExitTable drives every Section 6.5 final state through the
// same entry point the CLI uses and checks the exit code.
func TestExecExitTable(t *testing.T) {
	states := map[types.RunState]int{
		types.RunCompleted:        0,
		types.RunCompletedPending: 2,
		types.RunBlocked:          3,
		types.RunPaused:           4,
		types.RunFailed:           5,
		types.RunCanceled:         130,
	}
	for state, want := range states {
		out, err := RunExec(context.Background(), ExecOptions{
			Request:      "do the thing",
			LogsDir:      t.TempDir(),
			Profile:      ProfileEval,
			ForceOutcome: state,
			ForceReason:  "test",
		})
		if err != nil {
			t.Fatalf("state %q: %v", state, err)
		}
		if out.ExitCode != want {
			t.Fatalf("state %q exit = %d, want %d", state, out.ExitCode, want)
		}
		if out.State != state {
			t.Fatalf("state = %q, want %q", out.State, state)
		}
	}
}

// TestExecEmptyRequestBlocked covers the Section 6.7 row: no
// mandatory datum means blocked with code 3, never a question.
func TestExecEmptyRequestBlocked(t *testing.T) {
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "   ", LogsDir: t.TempDir(), Profile: ProfileDelegate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunBlocked || out.ExitCode != ExitBlocked {
		t.Fatalf("empty request = %q/%d, want blocked/3", out.State, out.ExitCode)
	}
	if out.Question != "" {
		t.Fatal("exec must never ask (Section 6.5)")
	}
}

// TestExecOutputSchema covers the Section 6.7 row: the final result
// validates against --output-schema on success, and a mismatch is a
// harness error.
func TestExecOutputSchema(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	schemaDoc := `{"type":"object","required":["state","summary"],
		"properties":{"state":{"type":"string"},"summary":{"type":"string"},
		"reason":{"type":"string"},"assumptions":{"type":"array"}}}`
	if err := os.WriteFile(schema, []byte(schemaDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write it", LogsDir: t.TempDir(), OutputSchema: schema,
	})
	if err != nil {
		t.Fatalf("valid schema must pass: %v", err)
	}
	if out.ExitCode != ExitOK {
		t.Fatalf("exit = %d", out.ExitCode)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"type":"object","required":["nope"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunExec(context.Background(), ExecOptions{
		Request: "write it", LogsDir: t.TempDir(), OutputSchema: bad,
	}); err == nil {
		t.Fatal("schema mismatch must fail as a harness error")
	} else if ExitCode(err) != ExitError {
		t.Fatalf("schema mismatch exit = %d, want 1", ExitCode(err))
	}
}

// TestExecArtifacts checks the Appendix F minimum records: profile
// with hashes, valid ATIF, metrics.json and events, all incremental.
func TestExecArtifacts(t *testing.T) {
	logs := t.TempDir()
	var events strings.Builder
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "do the thing", LogsDir: logs, Profile: ProfileEval,
		EmitJSON: true, Emit: &events,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ATIFPath != filepath.Join(logs, "trajectory.json") {
		t.Fatalf("eval ATIF must sit in /logs/agent, got %q", out.ATIFPath)
	}
	if err := telemetry.ValidateFile(out.ATIFPath); err != nil {
		t.Fatalf("ATIF invalid: %v", err)
	}
	if out.MetricsPath != eval.MetricsPath(logs) {
		t.Fatalf("metrics path = %q", out.MetricsPath)
	}
	if _, err := eval.FromMetricsFile(out.MetricsPath); err != nil {
		t.Fatalf("metrics must feed AgentContext: %v", err)
	}
	for _, name := range []string{"profile.json", "profile.sha256", "result.json", "events.jsonl", "metrics.json"} {
		if _, err := os.Stat(filepath.Join(out.Dir, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}
	if out.ProfileHash == "" || out.ProfileHash != eval.DefaultEvalProfile().Hash() {
		t.Fatalf("eval profile hash = %q", out.ProfileHash)
	}
	if events.Len() == 0 {
		t.Fatal("EmitJSON must stream the event flow")
	}
	var first map[string]any
	for _, line := range strings.Split(strings.TrimSpace(events.String()), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("event line is not JSON: %v", err)
		}
		if first == nil {
			first = ev
		}
	}
	if first["event"] != "run_started" {
		t.Fatalf("first event = %v", first["event"])
	}
}

// TestExecEvalGuards covers the Section 23.8 profile rows: a
// non-empty data dir aborts without override, an override is
// recorded, and two trials never share state.
func TestExecEvalGuards(t *testing.T) {
	logs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(logs, "excalibur"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "excalibur", "dirty"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: logs, Profile: ProfileEval,
	}); err == nil {
		t.Fatal("dirty eval data dir must abort")
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: logs, Profile: ProfileEval, ProfileOverride: "declared-deviation",
	})
	if err != nil {
		t.Fatalf("override must allow: %v", err)
	}
	if !out.Overridden {
		t.Fatal("override must be recorded")
	}
	second, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: t.TempDir(), Profile: ProfileEval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID == second.TaskID || out.ATIFPath == second.ATIFPath {
		t.Fatal("trials must be isolated (Section 23.8)")
	}
}

// TestExecCancelAndTimeout covers interruption (130) and budget/time
// pauses (4) of the Section 6.5 table.
func TestExecCancelAndTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := RunExec(ctx, ExecOptions{Request: "x", LogsDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCanceled || out.ExitCode != ExitCanceled {
		t.Fatalf("canceled ctx = %q/%d", out.State, out.ExitCode)
	}
	exhausted := 0.0
	out, err = RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: t.TempDir(), BudgetUSD: &exhausted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunPaused || out.ExitCode != ExitPaused {
		t.Fatalf("exhausted budget = %q/%d, want paused/4", out.State, out.ExitCode)
	}
	dctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	_ = dctx
}

// TestExecAssumeBranch covers Section 6.4 in exec: the ask marker is
// assumed, recorded, and the run completes.
func TestExecAssumeBranch(t *testing.T) {
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "deploy it {{ask: which host?}}", LogsDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if len(out.Assumptions) != 1 || !strings.Contains(out.Assumptions[0], "which host?") {
		t.Fatalf("assumptions = %v", out.Assumptions)
	}
}

// TestExecPartialArtifactsOnFailure covers the Section 23.8 row for
// cut trials: a run that fails mid-work still leaves a valid partial
// ATIF trajectory, events and metrics.
func TestExecPartialArtifactsOnFailure(t *testing.T) {
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "fragile work", LogsDir: logs, Profile: ProfileEval,
		Work: func(ctx context.Context, run *ExecRun) error {
			if err := run.EmitStep(telemetry.ATIFStep{
				Role: "assistant", Content: "starting",
				ToolCalls: []telemetry.ATIFToolCall{{ID: "call_1", Name: "exec", Arguments: "{}"}},
			}, "tool_call"); err != nil {
				return err
			}
			return fmt.Errorf("tool exploded")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunFailed || out.ExitCode != ExitFailed {
		t.Fatalf("state = %q/%d", out.State, out.ExitCode)
	}
	if err := telemetry.ValidateFile(out.ATIFPath); err != nil {
		t.Fatalf("partial ATIF must stay valid: %v", err)
	}
	if _, err := eval.FromMetricsFile(out.MetricsPath); err != nil {
		t.Fatalf("partial metrics must feed AgentContext: %v", err)
	}
	for _, name := range []string{"events.jsonl", "result.json"} {
		if _, err := os.Stat(filepath.Join(out.Dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

// TestExecTimeoutPauses covers the Section 6.5 time bound: an expired
// deadline pauses the run (code 4), it never fails it.
func TestExecTimeoutPauses(t *testing.T) {
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "slow work", LogsDir: t.TempDir(), TimeoutSecs: 1,
		Work: func(ctx context.Context, run *ExecRun) error {
			<-ctx.Done()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunPaused || out.ExitCode != ExitPaused {
		t.Fatalf("timeout = %q/%d, want paused/4", out.State, out.ExitCode)
	}
}

// TestExecEvalSingleModel covers the Section 23.8 single-model row:
// the eval run records exactly the evaluated model. The stub stands
// in explicitly (EXCALIBUR_PROVIDER=stub); unknown kinds fail at
// startup instead of running silently.
func TestExecEvalSingleModel(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER", "stub")
	t.Setenv("EXCALIBUR_MODEL", "gizmo-1")
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: logs, Profile: ProfileEval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eval.CheckSingleModel([]string{"gizmo-1"}, "gizmo-1"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out.MetricsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap["model"] != "gizmo-1" || snap["provider"] != "stub" {
		t.Fatalf("metrics identity = %v", snap)
	}
}

// TestExecUnknownProviderFails covers the production startup rule:
// a configured-but-unknown provider kind fails fast with a harness
// error (exit 1), it never runs a silent stub.
func TestExecUnknownProviderFails(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER", "acme")
	t.Setenv("EXCALIBUR_MODEL", "gizmo-1")
	t.Setenv("EXCALIBUR_CREDENTIALS_FILE", "/nonexistent-operator-credentials.json")
	if _, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: t.TempDir(), Profile: ProfileEval,
	}); err == nil {
		t.Fatal("unknown provider kind must fail")
	} else if ExitCode(err) != ExitError {
		t.Fatalf("unknown provider exit = %d, want %d", ExitCode(err), ExitError)
	} else if !strings.Contains(err.Error(), `unknown provider kind "acme"`) {
		t.Fatalf("error must name the kind: %v", err)
	}
}

// TestExecUnconfiguredFails covers the production startup rule:
// with no provider configured, exec fails with a clear error and a
// stable exit code instead of silently running a stub.
func TestExecUnconfiguredFails(t *testing.T) {
	old := resolveSettings
	resolveSettings = func() (models.ModelSettings, config.Config, error) {
		return models.ModelSettings{}, config.Config{}, nil
	}
	defer func() { resolveSettings = old }()
	if _, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: t.TempDir(), Profile: ProfileDefault,
	}); err == nil {
		t.Fatal("unconfigured provider must fail")
	} else if ExitCode(err) != ExitError {
		t.Fatalf("unconfigured exit = %d, want %d", ExitCode(err), ExitError)
	} else if !strings.Contains(err.Error(), "no model provider configured") {
		t.Fatalf("error must say how to configure: %v", err)
	}
}
