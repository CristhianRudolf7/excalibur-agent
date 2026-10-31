// State machine, wake events, completion gate, closing mode and
// loop escalation: the Section 7.7 rows owned by the engine.
package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestTransitionsFromEveryNonTerminalState(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		setup func(e *Engine, run *Run)
		// legal transitions must succeed, illegal must fail.
		legal   []func(e *Engine, run *Run) error
		illegal []func(e *Engine, run *Run) error
	}{
		{"created",
			func(e *Engine, run *Run) {},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Admit(ctx, run) },
				func(e *Engine, run *Run) error { return e.Cancel(ctx, run) },
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Start(ctx, run) },
				func(e *Engine, run *Run) error { return e.RequestPause(ctx, run, types.PauseUser, "u") },
				func(e *Engine, run *Run) error { return e.Wait(ctx, run, types.WaitTool) },
			}},
		{"queued",
			func(e *Engine, run *Run) { _ = e.Admit(ctx, run) },
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Start(ctx, run) },
				func(e *Engine, run *Run) error { return e.RequestPause(ctx, run, types.PauseUser, "u") },
				func(e *Engine, run *Run) error { return e.Cancel(ctx, run) },
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Wake(ctx, run, WakeEvent{Kind: WakeTool}) },
				func(e *Engine, run *Run) error { return e.Resume(ctx, run) },
			}},
		{"running",
			func(e *Engine, run *Run) { _ = e.Admit(ctx, run); _ = e.Start(ctx, run) },
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Wait(ctx, run, types.WaitTool) },
				func(e *Engine, run *Run) error { return e.RequestPause(ctx, run, types.PauseUser, "u") },
				func(e *Engine, run *Run) error { return e.Cancel(ctx, run) },
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Resume(ctx, run) },
				func(e *Engine, run *Run) error { return e.Wake(ctx, run, WakeEvent{Kind: WakeTool}) },
			}},
		{"waiting",
			func(e *Engine, run *Run) {
				_ = e.Admit(ctx, run)
				_ = e.Start(ctx, run)
				_ = e.Wait(ctx, run, types.WaitUser)
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Wake(ctx, run, WakeEvent{Kind: WakeUser}) },
				func(e *Engine, run *Run) error { return e.RequestPause(ctx, run, types.PauseUser, "u") },
				func(e *Engine, run *Run) error { return e.Cancel(ctx, run) },
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Resume(ctx, run) },
				func(e *Engine, run *Run) error { return e.Admit(ctx, run) },
			}},
		{"paused",
			func(e *Engine, run *Run) {
				_ = e.Admit(ctx, run)
				_ = e.Start(ctx, run)
				_ = e.RequestPause(ctx, run, types.PauseUser, "u")
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Resume(ctx, run) },
				func(e *Engine, run *Run) error { return e.Cancel(ctx, run) },
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Wake(ctx, run, WakeEvent{Kind: WakeUser}) },
				func(e *Engine, run *Run) error { return e.Wait(ctx, run, types.WaitTool) },
			}},
		{"blocked",
			func(e *Engine, run *Run) {
				run.mu.Lock()
				run.state = types.RunBlocked
				run.mu.Unlock()
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Resume(ctx, run) },
				func(e *Engine, run *Run) error { return e.Cancel(ctx, run) },
			},
			[]func(e *Engine, run *Run) error{
				func(e *Engine, run *Run) error { return e.Wait(ctx, run, types.WaitTool) },
				func(e *Engine, run *Run) error { return e.RequestPause(ctx, run, types.PauseUser, "u") },
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for i, op := range c.illegal {
				e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
				run := testRun(Profile{})
				e.AttachJournal(run.ID, js)
				c.setup(e, run)
				if err := op(e, run); err == nil {
					t.Fatalf("illegal op %d from %s succeeded", i, c.name)
				}
			}
			for i, op := range c.legal {
				e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
				run := testRun(Profile{})
				e.AttachJournal(run.ID, js)
				c.setup(e, run)
				if err := op(e, run); err != nil {
					t.Fatalf("legal op %d from %s failed: %v", i, c.name, err)
				}
			}
		})
	}
}

func TestStateChangesAreJournaled(t *testing.T) {
	ctx := context.Background()
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	e.AttachJournal(run.ID, js)
	_ = e.Admit(ctx, run)
	_ = e.Start(ctx, run)
	_ = e.Wait(ctx, run, types.WaitAgenda)
	_ = e.Wake(ctx, run, WakeEvent{Kind: WakeAgenda})
	entries, _ := js.List(ctx, run.ID)
	var changes int
	for _, en := range entries {
		if en.Type == journal.TypeStateChange {
			changes++
		}
	}
	if changes != 4 {
		t.Fatalf("state changes = %d, want 4", changes)
	}
}

func TestWaitingConsumesNoModelAndEveryWakeResumes(t *testing.T) {
	wakes := []WakeEvent{
		{Kind: WakeTool}, {Kind: WakeUser}, {Kind: WakeSubagents},
		{Kind: WakeAgenda}, {Kind: WakeCapability}, {Kind: WakeLease},
		{Kind: WakeDirective},
	}
	for _, w := range wakes {
		t.Run(w.Kind, func(t *testing.T) {
			ctx := context.Background()
			model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
			e, js := testEngine(model, scriptedRunner(), passingGate())
			run := testRun(Profile{})
			startRun(t, e, run, js)
			if err := e.Wait(ctx, run, types.WaitReason(w.Kind)); err != nil {
				t.Fatal(err)
			}
			if err := e.Run(ctx, run); err != nil {
				t.Fatal(err)
			}
			if model.calls() != 0 {
				t.Fatalf("waiting run called the model %d times", model.calls())
			}
			if err := e.Wake(ctx, run, w); err != nil {
				t.Fatal(err)
			}
			if err := e.Run(ctx, run); err != nil {
				t.Fatal(err)
			}
			mustState(t, run, types.RunCompleted)
			if model.calls() != 1 {
				t.Fatalf("woken run should call the model once, got %d", model.calls())
			}
		})
	}
}

func TestDirectiveWakesWaitingRun(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Wait(ctx, run, types.WaitSubagents); err != nil {
		t.Fatal(err)
	}
	if err := e.DeliverDirective(ctx, run, Directive{ID: "d1", Text: "replan R4", Priority: "interrupt"}); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunRunning)
	if len(run.PendingDirectives()) != 1 {
		t.Fatal("directive should queue for the next step")
	}
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if len(run.PendingDirectives()) != 0 {
		t.Fatal("process_signals should consume directives")
	}
}

func TestPauseAndCancelNeedNoWake(t *testing.T) {
	ctx := context.Background()
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Wait(ctx, run, types.WaitCapability); err != nil {
		t.Fatal(err)
	}
	if err := e.RequestPause(ctx, run, types.PauseUser, "u"); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunPaused)
	if err := e.Resume(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Wait(ctx, run, types.WaitCapability); err != nil {
		t.Fatal(err)
	}
	if err := e.Cancel(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCanceled)
}

// gateCase builds a run whose gate fails for one 7.5 reason.
func gateCase(code, method string, state types.CriterionState, kind tasks.EvidenceKind, stale bool) tasks.GateInput {
	return tasks.GateInput{
		Level: types.TaskN1,
		Criteria: []tasks.GateCriterion{{
			Criterion: types.Criterion{Code: mustCriterionRef(code), Method: types.CriterionMethod(method), Statement: code},
			State:     state, Stale: stale, Kind: kind,
		}},
	}
}

func mustCriterionRef(s string) types.CriterionRef {
	r, err := types.ParseCriterionRef(s)
	if err != nil {
		panic(err)
	}
	return r
}

func TestGateDefectsNeverComplete(t *testing.T) {
	cases := []struct {
		name  string
		input tasks.GateInput
		want  types.RunState
	}{
		{"criterion without evidence",
			gateCase("C1.1", "test", types.CriterionUnverified, tasks.EvidenceExecutable, false),
			types.RunCompletedPending},
		{"stale evidence",
			gateCase("C1.1", "test", types.CriterionUnverified, tasks.EvidenceExecutable, true),
			types.RunCompletedPending},
		{"failed project tests",
			tasks.GateInput{ChecklistRequired: true, ChecklistComplete: false, ChecklistDetail: "test_login failed"},
			types.RunCompletedPending},
		{"paused child blocks",
			tasks.GateInput{Children: []tasks.GateChild{{ID: "c", State: types.RunPaused}}},
			types.RunCompletedPending},
		{"instruction revision newer",
			tasks.GateInput{InstructionsRevSeen: 1, InstructionsCurrent: 2},
			types.RunCompletedPending},
		{"inspection judgment without verifier on N2",
			tasks.GateInput{Level: types.TaskN2, Criteria: []tasks.GateCriterion{{
				Criterion: types.Criterion{Code: mustCriterionRef("C1.1"), Method: types.CriterionInspection, Statement: "looks right"},
				State:     types.CriterionSatisfied, Kind: tasks.EvidenceJudgment,
			}}},
			types.RunCompletedPending},
		{"inspection judgment with verifier agrees passes",
			tasks.GateInput{Level: types.TaskN2, Criteria: []tasks.GateCriterion{{
				Criterion: types.Criterion{Code: mustCriterionRef("C1.1"), Method: types.CriterionInspection, Statement: "looks right"},
				State:     types.CriterionSatisfied, Kind: tasks.EvidenceJudgment, VerifierAgreed: true,
			}}},
			types.RunCompleted},
		{"code criterion never counts judgment",
			gateCase("C1.1", "command", types.CriterionSatisfied, tasks.EvidenceJudgment, false),
			types.RunCompletedPending},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			model := &fakeEndpoint{responses: []providers.ChatResponse{
				finalResp("v1"), finalResp("v2"), finalResp("v3"),
			}}
			gate := GateFunc(func(*Run) tasks.GateReport { return tasks.Evaluate(c.input) })
			e, js := testEngine(model, scriptedRunner(), gate)
			run := testRun(Profile{})
			startRun(t, e, run, js)
			if err := e.Run(ctx, run); err != nil {
				t.Fatal(err)
			}
			mustState(t, run, c.want)
			if c.want != types.RunCompleted && model.calls() != 2 {
				t.Fatalf("two failed attempts before the honest state, got %d model calls", model.calls())
			}
			if c.want == types.RunCompleted && model.calls() != 1 {
				t.Fatalf("passing gate should finish on the first final, got %d", model.calls())
			}
			if rep := e.Report(run); c.want != types.RunCompleted && len(rep.Pendings) == 0 {
				t.Fatal("honest state needs pendings in the report")
			}
		})
	}
}

func TestGateHonestBlockedOnLoop(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("v1"), finalResp("v2")}}
	gate := GateFunc(func(*Run) tasks.GateReport {
		return tasks.GateReport{OK: false, Pendings: []string{"loop: same error 3 times"}}
	})
	e, js := testEngine(model, scriptedRunner(), gate)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunBlocked)
}

func TestUserConfirmationEndsPendingAndClosesLater(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("v1"), finalResp("v2")}}
	input := tasks.GateInput{Criteria: []tasks.GateCriterion{{
		Criterion: types.Criterion{Code: mustCriterionRef("C1.1"), Method: types.CriterionUserConfirmation, Statement: "taste"},
		State:     types.CriterionUnverified, Kind: tasks.EvidenceConfirmation,
	}}}
	gate := GateFunc(func(*Run) tasks.GateReport { return tasks.Evaluate(input) })
	e, js := testEngine(model, scriptedRunner(), gate)
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	rep := e.Report(run)
	if !strings.Contains(strings.Join(rep.Pendings, ";"), "pending your confirmation") {
		t.Fatalf("pendings: %v", rep.Pendings)
	}
}

func TestClosingModeKeepsPrefixAndStopsTools(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	run := testRun(Profile{Limit: tasks.Limit{MaxSteps: 10}})
	e.AttachJournal(run.ID, js)
	// Drive the builder directly: closing flips tool_choice only.
	b := &fakeBuilder{tools: []providers.ToolDefinition{{Name: "read_file"}}}
	open, openC, err := b.BuildRequest(ctx, run, false)
	if err != nil {
		t.Fatal(err)
	}
	closing, closedC, err := b.BuildRequest(ctx, run, true)
	if err != nil {
		t.Fatal(err)
	}
	if closing.ToolChoice != "none" {
		t.Fatalf("closing tool choice = %q", closing.ToolChoice)
	}
	if len(closing.Tools) != len(open.Tools) {
		t.Fatal("closing must keep the definitions for the cache prefix")
	}
	if prefixHash(openC) != prefixHash(closedC) {
		t.Fatal("closing must not move the prefix hash")
	}
	// Exhausted budgets pause with the budget motive.
	run.Tracker.Used.Steps = 10
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	e.deps.Context = &fakeBuilder{}
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunPaused)
	if _, _, _, _ = run.Reasons(); true {
		rep := e.Report(run)
		if rep.PlainState != "incomplete" {
			t.Fatalf("plain = %q", rep.PlainState)
		}
	}
}

func TestLoopEscalationNoteExpertBlocked(t *testing.T) {
	ctx := context.Background()
	same := toolResp(toolCall("c", "lookup", `{"q":"x"}`))
	model := &fakeEndpoint{responses: []providers.ChatResponse{same, same, same, same, same, same}}
	runner := scriptedRunner()
	runner.add("lookup", types.EffectReadOnly, types.IdemPure,
		tools.Result{Status: tools.StatusOK, Output: "same"})
	var expertCalls int
	e, js := testEngine(model, runner, passingGate())
	e.deps.Expert = func(context.Context, *Run, tasks.Detection) (string, error) {
		expertCalls++
		return "try a different query", nil
	}
	run := testRun(Profile{})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunBlocked)
	if expertCalls != 1 {
		t.Fatalf("expert consulted %d times, want 1 (stage 2)", expertCalls)
	}
	foundNote, foundExpert := false, false
	for _, n := range run.VolatileNotes() {
		if strings.HasPrefix(n, "loop:") {
			foundNote = true
		}
		if strings.HasPrefix(n, "expert:") {
			foundExpert = true
		}
	}
	if !foundNote || !foundExpert {
		t.Fatalf("notes: %v", run.VolatileNotes())
	}
}

func TestPauseAndBlockEmitFullReports(t *testing.T) {
	ctx := context.Background()
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := testRun(Profile{})
	startRun(t, e, run, js)
	run.Summary = "half done"
	run.Files = []string{"a.go"}
	run.InjectNote("x")
	if err := e.RequestPause(ctx, run, types.PauseUser, "u"); err != nil {
		t.Fatal(err)
	}
	rep := e.Report(run)
	if rep.State != types.RunPaused || rep.PlainState != "incomplete" {
		t.Fatalf("paused report: %+v", rep)
	}
	if rep.Summary != "half done" || len(rep.Files) != 1 {
		t.Fatalf("report fields: %+v", rep)
	}
	if rep.Metrics.Elapsed < 0 {
		t.Fatal("metrics must be present")
	}
	run2 := testRun(Profile{})
	e.AttachJournal(run2.ID, js)
	startRun(t, e, run2, js)
	run2.mu.Lock()
	run2.state = types.RunBlocked
	run2.blockReason = types.BlockLoop
	run2.LastGate = tasks.GateReport{Pendings: []string{"loop: x"}}
	run2.mu.Unlock()
	rep2 := e.Report(run2)
	if rep2.State != types.RunBlocked || rep2.PlainState != "blocked" {
		t.Fatalf("blocked report: %+v", rep2)
	}
	if len(rep2.Pendings) != 1 {
		t.Fatalf("pendings: %+v", rep2)
	}
}
