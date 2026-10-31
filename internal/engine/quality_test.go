package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestQualityDefaults(t *testing.T) {
	q := DefaultQualityConfig()
	if !q.WorkingMethod || !q.FamilyAdapt || !q.WorkingNotes || !q.LoopNudges || !q.FinishGuard {
		t.Fatalf("all quality mechanisms must default ON: %+v", q)
	}
	if q.MaxGuardTrips <= 0 || q.MaxNotes <= 0 {
		t.Fatalf("caps must be positive: %+v", q)
	}
}

func TestQualityEnvOverlay(t *testing.T) {
	t.Setenv("EXCALIBUR_Q_METHOD", "0")
	t.Setenv("EXCALIBUR_Q_FAMILY", "no")
	t.Setenv("EXCALIBUR_Q_NOTES", "off")
	t.Setenv("EXCALIBUR_Q_NUDGES", "false")
	t.Setenv("EXCALIBUR_Q_GUARD", "0")
	q := DefaultQualityConfig().WithEnv()
	if q.WorkingMethod || q.FamilyAdapt || q.WorkingNotes || q.LoopNudges || q.FinishGuard {
		t.Fatalf("env switches must force mechanisms off: %+v", q)
	}
	// Unset variables keep the value; invalid values are ignored.
	t.Setenv("EXCALIBUR_Q_GUARD", "1")
	t.Setenv("EXCALIBUR_Q_METHOD", "maybe")
	q = DefaultQualityConfig().WithEnv()
	if !q.FinishGuard {
		t.Fatal("EXCALIBUR_Q_GUARD=1 must force the guard on")
	}
	if !q.WorkingMethod {
		t.Fatal("invalid EXCALIBUR_Q_METHOD must be ignored")
	}
}

func TestNudgeTexts(t *testing.T) {
	q := DefaultQualityConfig()
	rules := []tasks.LoopRule{
		tasks.RuleIdenticalRepeat, tasks.RuleSameError, tasks.RuleCycle,
		tasks.RuleNoProgress, tasks.RuleRepeatedText, tasks.RuleEditMismatch,
	}
	for _, rule := range rules {
		d := tasks.Detection{Rule: rule, Stage: 1, Action: tasks.ActionNote, Note: "pattern x"}
		note := nudgeFor(q, d, "")
		if !strings.HasPrefix(note, "loop:") {
			t.Errorf("rule %s: nudge must keep the loop: prefix, got %q", rule, note)
		}
		if !strings.Contains(note, "Recovery:") {
			t.Errorf("rule %s: nudge must name a recovery strategy, got %q", rule, note)
		}
	}
	esc := tasks.Detection{Rule: tasks.RuleSameError, Stage: 2, Action: tasks.ActionEscalate, Note: "pattern x"}
	if got := escalateNote(q, esc, ""); !strings.HasPrefix(got, "loop escalated:") || !strings.Contains(got, "Strategy switch:") {
		t.Errorf("escalation without expert must switch strategy, got %q", got)
	}
	if got := escalateNote(q, esc, "try Y"); !strings.HasPrefix(got, "expert:") || !strings.Contains(got, "try Y") {
		t.Errorf("expert diagnosis must be preserved, got %q", got)
	}
	// Ablation off restores the legacy terse notes.
	off := QualityConfig{}
	d := tasks.Detection{Rule: tasks.RuleSameError, Stage: 1, Action: tasks.ActionNote, Note: "pattern x"}
	if got := nudgeFor(off, d, ""); got != "loop: pattern x" {
		t.Errorf("legacy note = %q, want %q", got, "loop: pattern x")
	}
	if got := escalateNote(off, esc, "try Y"); got != "expert: try Y" {
		t.Errorf("legacy expert note = %q", got)
	}
}

func TestOnDetectionNudgeAndPin(t *testing.T) {
	ctx := context.Background()
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	d := tasks.Detection{Rule: tasks.RuleSameError, Stage: 1, Action: tasks.ActionNote, Note: "ran exec 3 times with error boom"}
	if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
		t.Fatalf("stage 1 must not stop, got stop=%v err=%v", stop, err)
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.HasPrefix(n, "loop:") && strings.Contains(n, "Recovery:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("volatile notes lack an actionable nudge: %v", run.VolatileNotes())
	}
	if len(run.notesCopy()) == 0 {
		t.Fatal("detections must be pinned to the working notes")
	}
	// Ablation off: legacy note, still pinned when notes are on.
	off := QualityConfig{WorkingNotes: true}
	e2 := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate(), Quality: &off})
	run2 := testRun(Profile{})
	_, _ = e2.onDetection(ctx, run2, d)
	notes := run2.VolatileNotes()
	if len(notes) != 1 || notes[0] != "loop: ran exec 3 times with error boom" {
		t.Fatalf("legacy notes = %v", notes)
	}
}

// The thin-work guard only fires when no gate is wired and tools
// were attempted without a single success: a run that struggled and
// then tries to finish is sent back to work twice, then the
// (unwired) gate decides. A run that never attempted a tool passes
// straight through.
func TestFinishGuardThinWork(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"make"}`)),
		finalResp("v1"), finalResp("v2"), finalResp("v3"),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusToolError, Output: "boom"})
	e, js := testEngine(model, runner, nil)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls() != 4 {
		t.Fatalf("model calls = %d, want 4 (one failed tool, two guard trips, then the gate)", model.calls())
	}
	if run.GuardTrips != 2 {
		t.Fatalf("guard trips = %d, want 2", run.GuardTrips)
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "no verified work yet") {
			found = true
		}
	}
	if !found {
		t.Fatalf("guard note missing: %v", run.VolatileNotes())
	}
	// No tool attempts at all: a trivial answer finishes at once.
	clean := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("hi")}}
	e2, js2 := testEngine(clean, scriptedRunner(), nil)
	run2 := testRun(Profile{})
	startRun(t, e2, run2, js2)
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	mustState(t, run2, types.RunCompleted)
	if clean.calls() != 1 {
		t.Fatalf("trivial answers must finish at once, got %d calls", clean.calls())
	}
}

func TestFinishGuardStandsDown(t *testing.T) {
	ctx := context.Background()
	// A wired gate always wins, even with no tool work.
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("v1")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls() != 1 {
		t.Fatalf("wired gate must finish on the first final, got %d calls", model.calls())
	}
	// One successful tool call stands the unwired guard down too.
	model2 := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "write_file", `{"path":"a.txt","content":"hi"}`)),
		finalResp("done"),
	}}
	runner := scriptedRunner()
	runner.add("write_file", types.EffectWriteLocal, types.IdemKeyed,
		tools.Result{Status: tools.StatusOK, Output: "wrote a.txt"})
	e2, js2 := testEngine(model2, runner, nil)
	run2 := testRun(Profile{})
	startRun(t, e2, run2, js2)
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	mustState(t, run2, types.RunCompleted)
	if model2.calls() != 2 {
		t.Fatalf("one verified tool call must stand the guard down, got %d calls", model2.calls())
	}
}

func TestFinishGuardHook(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("v1"), finalResp("v2")}}
	trips := 0
	e, js := testEngine(model, scriptedRunner(), nil)
	e.deps.FinishGuard = func(*Run) (bool, string) {
		trips++
		if trips == 1 {
			return true, "custom: verify X first"
		}
		return false, ""
	}
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls() != 2 {
		t.Fatalf("hook must decide the trips, got %d calls", model.calls())
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "custom: verify X first") {
			found = true
		}
	}
	if !found {
		t.Fatalf("hook note missing: %v", run.VolatileNotes())
	}
}

// Failures and touched paths are pinned to the working notes, and
// the error-hint hook injects one recovery note per step.
func TestWorkingNotesExtraction(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"make","workdir":"/app"}`)),
		toolResp(toolCall("c2", "write_file", `{"path":"/app/out.txt","content":"x"}`)),
		finalResp("done"),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusToolError, Output: "make: *** No targets\n"})
	runner.add("write_file", types.EffectWriteLocal, types.IdemKeyed,
		tools.Result{Status: tools.StatusOK, Output: "wrote /app/out.txt"})
	e, js := testEngine(model, runner, nil)
	e.deps.ErrorHint = func(tool, args, errText string) string {
		if tool == "exec" {
			return "read the make output and fix the target"
		}
		return ""
	}
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	pinned := strings.Join(run.notesCopy(), "\n")
	if !strings.Contains(pinned, "FAIL exec") || !strings.Contains(pinned, "No targets") {
		t.Errorf("failures must be pinned, got: %s", pinned)
	}
	if !strings.Contains(pinned, "did write_file /app/out.txt") {
		t.Errorf("touched paths must be pinned, got: %s", pinned)
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "recovery hint for exec") {
			found = true
		}
	}
	if !found {
		t.Errorf("error-hint hook must inject a note, got: %v", run.VolatileNotes())
	}
}

func TestFactHelpers(t *testing.T) {
	if got := factForToolCall("exec", `{"command":"make"}`, tools.Result{Status: tools.StatusOK, Output: "ok"}); got != "" {
		t.Errorf("pathless success pins nothing, got %q", got)
	}
	if got := factForToolCall("read_file", `{"path":"a.go"}`, tools.Result{Status: tools.StatusOK, Output: "pkg"}); got != "did read_file a.go" {
		t.Errorf("path success = %q", got)
	}
	if got := factForToolCall("exec", `{"command":"make"}`, tools.Result{Status: tools.StatusToolError, Output: "\nboom\n"}); !strings.HasPrefix(got, "FAIL exec") || !strings.Contains(got, "boom") {
		t.Errorf("failure fact = %q", got)
	}
	if argsPath(`{"path":123}`) != "" || argsPath(`not json`) != "" {
		t.Fatal("argsPath must only accept string paths")
	}
	run := testRun(Profile{})
	run.addFact("a", 2)
	run.addFact("a", 2)
	run.addFact("b", 2)
	run.addFact("c", 2)
	if got := run.notesCopy(); len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("notes dedupe and cap = %v", got)
	}
}
