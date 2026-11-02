// Explicit finish protocol tests (agent-quality, Section 7.3 closing).
//
// Non-interactive runs end ONLY through `finish` with summary and
// evidence: text-only turns never complete, `finish` is refused while
// the last command errored or verification fails, the first accepted
// `finish` triggers a confirmation round, and `blocked` needs a
// concrete reason checked against the filesystem. OFF restores the
// old text-finish behaviour.
package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func finishArgs(summary, evidence string) string {
	raw, _ := json.Marshal(map[string]string{"summary": summary, "evidence": evidence})
	return string(raw)
}

func finishBlockedArgs(summary, evidence, reason string) string {
	raw, _ := json.Marshal(map[string]string{
		"summary": summary, "evidence": evidence, "status": "blocked", "reason": reason,
	})
	return string(raw)
}

func explicitRun() *Run {
	return testRun(Profile{ExplicitFinish: true})
}

func artifactOffCfg() *tasks.ArtifactConfig {
	cfg := tasks.DefaultArtifactConfig()
	cfg.Enable = false
	return &cfg
}

func selfVerifyOffCfg() *tasks.SelfVerifyConfig {
	cfg := tasks.DefaultSelfVerifyConfig()
	cfg.Enable = false
	return &cfg
}

// Text-only turns never end the run: the engine replies with a
// continuation note, counts the streak, and enters closing mode after
// 3 in a row.
func TestExplicitFinishTextOnlyNeverCompletes(t *testing.T) {
	ctx := finishTestContext(t)
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		finalResp("working on it"),
		finalResp("still working"),
		finalResp("almost done"),
		toolResp(toolCall("f1", "finish", finishArgs("did it", "exec true ok"))),
		toolResp(toolCall("f2", "finish", finishArgs("did it", "exec true ok"))),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls() != 5 {
		t.Fatalf("model calls = %d, want 5 (three text-only continuations plus the two-turn finish)", model.calls())
	}
	if run.FinishCalls != 2 {
		t.Fatalf("finish calls = %d, want 2", run.FinishCalls)
	}
	foundCont, foundClosing := false, false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "No tool call.") {
			foundCont = true
		}
		if strings.Contains(n, "closing mode") {
			foundClosing = true
		}
	}
	if !foundCont {
		t.Fatalf("continuation note missing: %v", run.VolatileNotes())
	}
	if !foundClosing {
		t.Fatalf("closing mode note missing after 3 text-only turns: %v", run.VolatileNotes())
	}
	if run.Summary != "did it" {
		t.Fatalf("summary = %q, want finish.summary (never the instruction)", run.Summary)
	}
	if run.FinalText == "" {
		t.Fatal("raw final text must be stored for the records")
	}
}

// The failed-check-then-completed pattern: the agent runs its own
// check, gets exit 1, and the very next turn must NOT end the run as
// completed. `finish` is refused while the last command errored.
func TestExplicitFinishRefusedAfterFailedCheck(t *testing.T) {
	ctx := finishTestContext(t)
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"python3 -c 'import foo'"}`)),
		finalResp("looks broken but I will declare done"),
		toolResp(toolCall("f1", "finish", finishArgs("fixed", "ran the check"))),
		toolResp(toolCall("c2", "exec", `{"command":"pip install foo"}`)),
		toolResp(toolCall("c3", "exec", `{"command":"python3 -c 'import foo'"}`)),
		toolResp(toolCall("f2", "finish", finishArgs("fixed foo", "pip install then import ok"))),
		toolResp(toolCall("f3", "finish", finishArgs("fixed foo", "pip install then import ok"))),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusToolError, Output: "exit: 1\nImportError: No module named 'foo'"},
		tools.Result{Status: tools.StatusOK, Output: "exit: 0\ninstalled foo"},
		tools.Result{Status: tools.StatusOK, Output: "exit: 0\nimport ok"},
	)
	e, js := testEngine(model, runner, passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if run.FinishRefusals != 1 {
		t.Fatalf("refusals = %d, want 1 (finish while the last check failed)", run.FinishRefusals)
	}
	if run.FinishCalls != 3 {
		t.Fatalf("finish calls = %d, want 3 (one refused, two confirmation)", run.FinishCalls)
	}
	found := false
	for _, n := range historyContents(run) {
		if strings.Contains(n, "finish refused") && strings.Contains(n, "last command errored") {
			found = true
		}
	}
	if !found {
		t.Fatalf("refusal must name the failed last command: %v", historyContents(run))
	}
	if run.Summary != "fixed foo" {
		t.Fatalf("summary = %q, want the accepted finish.summary", run.Summary)
	}
}

// Confirmation round: the first accepted `finish` arms the confirm,
// the second ends the run.
func TestExplicitFinishConfirmationRound(t *testing.T) {
	ctx := finishTestContext(t)
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", finishArgs("s1", "e1"))),
		toolResp(toolCall("f2", "finish", finishArgs("s2", "e2"))),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls() != 2 {
		t.Fatalf("model calls = %d, want 2 (confirm round)", model.calls())
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.HasPrefix(n, "finish checks passed.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("confirmation note missing: %v", run.VolatileNotes())
	}
	if run.Summary != "s2" {
		t.Fatalf("summary = %q, want the second (final) finish.summary", run.Summary)
	}
}

// `finish` must be called alone: mixed with other tools it is refused
// without executing anything.
func TestExplicitFinishMustBeAlone(t *testing.T) {
	ctx := finishTestContext(t)
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(
			toolCall("c1", "exec", `{"command":"true"}`),
			toolCall("f1", "finish", finishArgs("s", "e")),
		),
		toolResp(toolCall("f2", "finish", finishArgs("s", "exec true ok"))),
		toolResp(toolCall("f3", "finish", finishArgs("s", "exec true ok"))),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "exit: 0\nok"})
	e, js := testEngine(model, runner, passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	for _, c := range runner.calls {
		if c.Name == "exec" {
			t.Fatalf("mixed finish turn must not execute other tools, got exec call: %+v", c)
		}
	}
	found := false
	for _, n := range historyContents(run) {
		if strings.Contains(n, "must be called alone") {
			found = true
		}
	}
	if !found {
		t.Fatalf("alone-note missing: %v", historyContents(run))
	}
}

// Blocked via `finish` with status blocked and a concrete reason ends
// the run as blocked.
func TestExplicitFinishBlocked(t *testing.T) {
	ctx := finishTestContext(t)
	// D7: a blocked claim needs an attempt first; the install attempt that
	// failed (exit 1 in its header) is the evidence the reason quotes.
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"pip install secretpkg"}`)),
		toolResp(toolCall("f1", "finish", finishBlockedArgs("stuck", "tried pip install", "need the secretpkg package from the operator: pip reports No matching distribution found"))),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "exit: 1  duration: 0.4s  session: sess-1  alive: false\nERROR: No matching distribution found for secretpkg"})
	e, js := testEngine(model, runner, passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunBlocked)
	if _, _, block, _ := run.Reasons(); block != types.BlockMissingData {
		t.Fatalf("block reason = %q, want missing_data", block)
	}
}

// A blocked claim naming a path that exists is refused.
func TestExplicitFinishBlockedPathExistsRefused(t *testing.T) {
	ctx := finishTestContext(t)
	dir := t.TempDir()
	existing := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := finishBlockedArgs("stuck", "tried", "need "+existing+" which is missing")
	okArgs := finishArgs("done", "verified")
	// A finish after a refusal needs tool work in between (D2: the
	// no-change rule answers an unchanged finish from the same reasons).
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", blocked)),
		toolResp(toolCall("c1", "exec", `{"command":"ls `+dir+`"}`)),
		toolResp(toolCall("f2", "finish", okArgs)),
		toolResp(toolCall("f3", "finish", okArgs)),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "exit: 0\nok"})
	e, js := testEngine(model, runner, passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if run.FinishRefusals != 1 {
		t.Fatalf("refusals = %d, want 1 (existing path)", run.FinishRefusals)
	}
	found := false
	for _, n := range historyContents(run) {
		if strings.Contains(n, "finish refused") && strings.Contains(n, "exists") {
			found = true
		}
	}
	if !found {
		t.Fatalf("existing-path refusal missing: %v", historyContents(run))
	}
}

// Missing required artifacts refuse `finish`: no finish without the
// file the instruction demands.
func TestExplicitFinishRefusedOnMissingArtifact(t *testing.T) {
	ctx := finishTestContext(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "output.txt")
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", finishArgs("wrote output", "did the work"))),
		toolResp(toolCall("c1", "exec", `{"command":"echo hi"}`)),
		toolResp(toolCall("f2", "finish", finishArgs("wrote output", "echo hi then verified "+missing))),
		toolResp(toolCall("f3", "finish", finishArgs("wrote output", "echo hi then verified "+missing))),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "exit: 0\nhi"})
	e, js := testEngine(model, runner, passingGate())
	// Self-verify ON (no refine, no strong verifier) with the
	// artifact gate ON and the default OS stat: the missing file
	// blocks `finish`.
	cfg := tasks.DefaultSelfVerifyConfig()
	cfg.EnableRefine = false
	cfg.EnableStrong = false
	e.deps.SelfVerify = &cfg
	run := testRun(Profile{ExplicitFinish: true, Limit: tasks.Limit{MaxSteps: 10}})
	run.Request = "Write the result to " + missing + "."
	run.Workdir = dir
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	// The first finish is refused (missing artifact); the script never
	// creates the file, so the run keeps refusing until the step
	// budget pauses it. Refusals are unbounded by GateAttempts.
	mustState(t, run, types.RunPaused)
	if run.FinishRefusals < 1 {
		t.Fatalf("refusals = %d, want >=1 (missing artifact)", run.FinishRefusals)
	}
	if len(run.RefusalRecords()) == 0 {
		t.Fatal("artifact refusal missing from durable audit")
	}
}

// Switch OFF restores the old behaviour: a text-only turn runs the
// gate and completes.
func TestExplicitFinishSwitchOff(t *testing.T) {
	ctx := finishTestContext(t)
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	off := DefaultExplicitFinishConfig()
	off.Enabled = false
	e.deps.ExplicitFinish = &off
	run := testRun(Profile{ExplicitFinish: true})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls() != 1 {
		t.Fatalf("switch off must finish on the first final, got %d calls", model.calls())
	}
	if run.FinishCalls != 0 {
		t.Fatalf("switch off must never count finish calls, got %d", run.FinishCalls)
	}
}

// Invalid `finish` arguments are refused with the reason.
func TestExplicitFinishInvalidArgs(t *testing.T) {
	ctx := finishTestContext(t)
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", `{"summary":"","evidence":""}`)),
		toolResp(toolCall("f2", "finish", finishArgs("s", "e"))),
		toolResp(toolCall("f3", "finish", finishArgs("s", "e"))),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if run.FinishRefusals != 1 {
		t.Fatalf("refusals = %d, want 1 (empty summary)", run.FinishRefusals)
	}
}

// A provider that repeats refused finishes cannot grow history forever.
func TestFinishOnlyProviderSafetyStop(t *testing.T) {
	responses := make([]providers.ChatResponse, 30)
	for i := range responses {
		responses[i] = toolResp(toolCall("f", "finish", `{}`))
	}
	model := &fakeEndpoint{responses: responses}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(finishTestContext(t), run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if model.calls() != 25 {
		t.Fatalf("calls=%d, want 25", model.calls())
	}
}

func TestFinishSafetyUnbudgetedTurns(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Tracker.Limit = tasks.Limit{}
	run.Steps = 201
	if reason := e.finishSafetyReason(run); !strings.Contains(reason, "200 turns") {
		t.Fatal(reason)
	}
	run.Tracker.Limit.MaxSteps = 300
	if reason := e.finishSafetyReason(run); reason != "" {
		t.Fatal(reason)
	}
}
