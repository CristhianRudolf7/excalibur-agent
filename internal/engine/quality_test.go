package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestQualityDefaults(t *testing.T) {
	q := DefaultQualityConfig()
	if !q.WorkingMethod || !q.FamilyAdapt || !q.WorkingNotes || !q.LoopNudges || !q.StuckRecovery || !q.RepeatBlock || !q.FinishGuard {
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
	t.Setenv("EXCALIBUR_Q_RECOVERY", "0")
	t.Setenv("EXCALIBUR_Q_REPEATBLOCK", "no")
	t.Setenv("EXCALIBUR_Q_GUARD", "0")
	q := DefaultQualityConfig().WithEnv()
	if q.WorkingMethod || q.FamilyAdapt || q.WorkingNotes || q.LoopNudges || q.StuckRecovery || q.RepeatBlock || q.FinishGuard {
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

// The structured stuck-recovery prompt fires on the near-repeat and
// no-information rules: it keeps the loop: prefix, names the tried
// calls, and forces a summarise / three-strategies / pick-one shape
// instead of another retry. With the switch off it falls back to the
// plain nudge.
func TestRecoveryPrompt(t *testing.T) {
	q := DefaultQualityConfig()
	for _, rule := range []tasks.LoopRule{tasks.RuleNearRepeat, tasks.RuleNoInfo} {
		d := tasks.Detection{Rule: rule, Stage: 1, Action: tasks.ActionNote, Note: "pattern x"}
		got := recoveryPrompt(q, d, []string{"exec ps", "exec ls"})
		if !strings.HasPrefix(got, "loop:") {
			t.Errorf("rule %s: recovery must keep the loop: prefix, got %q", rule, got)
		}
		for _, want := range []string{"Tried", "Three DIFFERENT strategies", "Pick the most promising", "do NOT repeat", "exec ps"} {
			if !strings.Contains(got, want) {
				t.Errorf("rule %s: recovery must contain %q, got %q", rule, want, got)
			}
		}
	}
	off := QualityConfig{}
	d := tasks.Detection{Rule: tasks.RuleNearRepeat, Stage: 1, Action: tasks.ActionNote, Note: "pattern x"}
	if got, want := recoveryPrompt(off, d, nil), "loop: pattern x"; got != want {
		t.Errorf("recovery off falls back to the plain nudge: got %q want %q", got, want)
	}
}

// onDetection routes the new rules to the recovery prompt at stage
// 1 and still escalates at stage 2 through the standard path.
func TestOnDetectionRecoveryPrompt(t *testing.T) {
	ctx := context.Background()
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	recordCall(run, "exec ps -p 1")
	d := tasks.Detection{Rule: tasks.RuleNearRepeat, Stage: 1, Action: tasks.ActionNote, Note: "same ps 3 times"}
	if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
		t.Fatalf("stage 1 must not stop, got stop=%v err=%v", stop, err)
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.HasPrefix(n, "loop:") && strings.Contains(n, "Three DIFFERENT strategies") && strings.Contains(n, "exec ps -p 1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("recovery prompt with tried calls missing: %v", run.VolatileNotes())
	}
	if len(run.notesCopy()) == 0 {
		t.Fatal("recovery detections must be pinned to the working notes")
	}
	// Switch off: the same detection renders the legacy terse note.
	off := QualityConfig{WorkingNotes: true}
	e2 := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: scriptedRunner(), Gate: passingGate(), Quality: &off})
	run2 := testRun(Profile{})
	_, _ = e2.onDetection(ctx, run2, d)
	notes := run2.VolatileNotes()
	if len(notes) != 1 || notes[0] != "loop: same ps 3 times" {
		t.Fatalf("legacy notes = %v", notes)
	}
}

// An exact repeat of the previous single call is rejected once with
// guidance; the identical call after that runs, so a legitimate
// retry is delayed by one step, never denied.
func TestRepeatBlockOnce(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"poll"}`)),
		toolResp(toolCall("c2", "exec", `{"command":"poll"}`)),
		toolResp(toolCall("c3", "exec", `{"command":"poll"}`)),
		finalResp("done"),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "same output"})
	e, js := testEngine(model, runner, nil)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if got := runner.ncalls(); got != 2 {
		t.Fatalf("executions = %d, want 2 (first runs, second blocked, third runs)", got)
	}
	found := false
	for _, ev := range run.History {
		if ev.ToolResult != nil && strings.Contains(ev.Content, "blocked: exact repeat") {
			found = true
		}
	}
	if !found {
		t.Fatal("blocked repeat must be recorded in history with guidance")
	}
	// Switch off: all three identical calls execute.
	model2 := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"poll"}`)),
		toolResp(toolCall("c2", "exec", `{"command":"poll"}`)),
		finalResp("done"),
	}}
	runner2 := scriptedRunner()
	runner2.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "same output"})
	off := QualityConfig{}
	e2 := New(Deps{Models: model2, Context: &fakeBuilder{}, Tools: runner2, Quality: &off})
	run2 := testRun(Profile{})
	js2 := journal.NewMemStore()
	startRun(t, e2, run2, js2)
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	if got := runner2.ncalls(); got != 2 {
		t.Fatalf("blocker off: executions = %d, want 2", got)
	}
}

// Retryable outcomes are never blocked: a timeout followed by the
// identical retry executes immediately.
func TestRepeatBlockSkipsRetryable(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"flaky"}`)),
		toolResp(toolCall("c2", "exec", `{"command":"flaky"}`)),
		finalResp("done"),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusTimeout, Output: "timed out"},
		tools.Result{Status: tools.StatusOK, Output: "recovered"})
	e, js := testEngine(model, runner, nil)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if got := runner.ncalls(); got != 2 {
		t.Fatalf("retryable repeat must execute, executions = %d", got)
	}
	for _, ev := range run.History {
		if ev.ToolResult != nil && strings.Contains(ev.Content, "blocked: exact repeat") {
			t.Fatal("retryable outcomes must never be blocked")
		}
	}
}

// Multi-call batches pass the blocker through: only the unambiguous
// one-call repeat is rejected.
func TestRepeatBlockMultiBatchPassthrough(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(
			toolCall("c1", "exec", `{"command":"a"}`),
			toolCall("c2", "exec", `{"command":"b"}`),
		),
		toolResp(
			toolCall("c3", "exec", `{"command":"a"}`),
			toolCall("c4", "exec", `{"command":"b"}`),
		),
		finalResp("done"),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "ok"})
	e, js := testEngine(model, runner, nil)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if got := runner.ncalls(); got != 4 {
		t.Fatalf("multi-call batches must pass through, executions = %d", got)
	}
}

// Six consecutive dull steps (errors on different commands, so no
// other rule owns them) fire the no-information recovery prompt.
func TestNoInfoRecoveryThroughLoop(t *testing.T) {
	ctx := context.Background()
	var resps []providers.ChatResponse
	for i := 0; i < tasks.LoopNoInfoSteps; i++ {
		cmd := fmt.Sprintf(`{"command":"entirely-unrelated-failing-command-number-%d-with-a-long-tail"}`, i)
		resps = append(resps, toolResp(toolCall("c", "exec", cmd)))
	}
	resps = append(resps, finalResp("done"))
	model := &fakeEndpoint{responses: resps}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusToolError, Output: "boom"})
	e, js := testEngine(model, runner, nil)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "Three DIFFERENT strategies") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no-info recovery prompt missing: %v", run.VolatileNotes())
	}
}

// R4 fires on real progress signals and stays silent on the zero
// state while the stuck-recovery mechanism owns novelty tracking in
// single-run scope; with the switch off the legacy R4 observation
// fires on the zero state as before.
func TestNoProgressZeroStateSkip(t *testing.T) {
	commands := []string{
		"deploy-frontend-service-now", "migrate-database-schema-v2",
		"rebuild-search-index-shard", "rotate-signing-keys-today",
		"compress-archive-artifacts", "verify-checksum-manifest",
		"restart-worker-pool-blue", "snapshot-volume-group-main",
		"prune-stale-container-cache", "audit-access-log-shard",
	}
	outputs := []string{
		"frontend live", "migrations applied", "index rebuilt",
		"keys rotated", "archive stored", "checksums match",
		"workers restarted", "snapshot taken", "cache pruned",
		"audit clean",
	}
	ctx := context.Background()
	build := func() (*fakeEndpoint, *fakeRunner) {
		var resps []providers.ChatResponse
		for _, cmd := range commands {
			resps = append(resps, toolResp(toolCall("c", "exec", `{"command":"`+cmd+`"}`)))
		}
		resps = append(resps, finalResp("done"))
		model := &fakeEndpoint{responses: resps}
		runner := scriptedRunner()
		for _, out := range outputs {
			runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
				tools.Result{Status: tools.StatusOK, Output: out})
		}
		return model, runner
	}
	// Distinct successful outputs keep every step informative and no
	// other rule can own the traffic, so only R4 could fire on the
	// zero progress state. With recovery on it is skipped.
	model, runner := build()
	e, js := testEngine(model, runner, nil)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, n := range run.VolatileNotes() {
		if strings.HasPrefix(n, "loop:") {
			t.Fatalf("healthy varied work must draw no loop note, got: %v", run.VolatileNotes())
		}
	}
	// Switch off: the legacy R4 observation fires on the zero state
	// as before.
	model2, runner2 := build()
	off := QualityConfig{}
	e2 := New(Deps{Models: model2, Context: &fakeBuilder{}, Tools: runner2, Quality: &off})
	run2 := testRun(Profile{})
	js2 := journal.NewMemStore()
	startRun(t, e2, run2, js2)
	if err := e2.Run(ctx, run2); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range run2.VolatileNotes() {
		if strings.Contains(n, "no state change") {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy R4 must fire on the zero state: %v", run2.VolatileNotes())
	}
}
