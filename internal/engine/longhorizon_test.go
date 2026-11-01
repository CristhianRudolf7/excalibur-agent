package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func longHorizonOff() *tasks.LongHorizonConfig {
	cfg := tasks.DefaultLongHorizonConfig()
	cfg.Enable = false
	return &cfg
}

func execHistoryAlive() []ctxcompiler.ActivityEvent {
	return []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "exec", `{"command":"make -j","background":false}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "session: sess-1  alive: true\nbuilding..."}},
	}
}

// Time- and evidence-aware continuation blocks a shallow finish while
// significant budget remains and executable evidence is thin.
func TestLongHorizonContinuationBlocksShallowFinish(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		finalResp("done"), finalResp("done"), finalResp("done"),
		finalResp("done"), finalResp("done"), finalResp("done"),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	e.deps.Artifacts = artifactOff()
	lh := tasks.DefaultLongHorizonConfig()
	e.deps.LongHorizon = &lh
	run := testRun(Profile{Limit: tasks.Limit{MaxSteps: 25}})
	run.Request = "Create the file /app/filter.py that strips scripts and verify it by running the checks."
	run.History = writeHistory("/app/filter.py")
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	// GateAttempts (2) plus long-horizon continuations (3): the shallow
	// write with no exercising check must keep returning to work.
	if model.calls() < 4 {
		t.Fatalf("shallow finish with time left must continue, got %d model calls", model.calls())
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "long-horizon continuation") {
			found = true
		}
	}
	if !found {
		t.Fatalf("continuation note missing: %v", run.VolatileNotes())
	}
	run.mu.Lock()
	trips := run.lhTrips
	run.mu.Unlock()
	if trips == 0 {
		t.Fatal("continuation trips must be recorded")
	}
}

// Little time left stands the continuation down: the normal gate
// decides, so tight budgets finish fast.
func TestLongHorizonContinuationStandsDownWhenLate(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	e.deps.Artifacts = artifactOff()
	lh := tasks.DefaultLongHorizonConfig()
	e.deps.LongHorizon = &lh
	run := testRun(Profile{Limit: tasks.Limit{MaxSteps: 25, MaxSeconds: 100}})
	run.StartedAt = time.Now().UTC().Add(-99 * time.Second)
	run.HeartbeatAt = time.Now().UTC()
	run.Request = "Create the file /app/filter.py that strips scripts."
	run.History = writeHistory("/app/filter.py")
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	// One final plus at most the gate retry: no long-horizon trips when
	// only ~1% of the wall budget remains.
	run.mu.Lock()
	trips := run.lhTrips
	run.mu.Unlock()
	if trips != 0 {
		t.Fatalf("late finish must not continue, trips = %d", trips)
	}
}

// The master switch restores the pre-long-horizon finish path.
func TestLongHorizonSwitchOff(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	e.deps.Artifacts = artifactOff()
	e.deps.LongHorizon = longHorizonOff()
	run := testRun(Profile{Limit: tasks.Limit{MaxSteps: 25}})
	run.Request = "Create the file /app/filter.py that strips scripts and verify it by running the checks."
	run.History = writeHistory("/app/filter.py")
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	if model.calls() != 2 {
		t.Fatalf("switch off must keep the legacy 2-call bound, got %d", model.calls())
	}
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "long-horizon continuation") {
			t.Fatalf("switch off must stay silent, got %q", n)
		}
	}
}

// Large tasks get an explicit milestone plan pinned to the working
// notes with a next-milestone nudge; small tasks stay untouched.
func TestLongHorizonMilestones(t *testing.T) {
	e := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: scriptedRunner()})
	lh := tasks.DefaultLongHorizonConfig()
	e.deps.LongHorizon = &lh
	big := testRun(Profile{})
	big.Request = "Migrate the service to the new API: " + strings.Repeat("rewrite the handlers, update the tests, verify end to end. ", 20)
	e.ensureLHMilestones(big)
	big.mu.Lock()
	init, large, n := big.lhInit, big.lhLarge, len(big.lhMilestones)
	big.mu.Unlock()
	if !init || !large || n < 4 {
		t.Fatalf("large task must build a plan, init=%v large=%v n=%d", init, large, n)
	}
	if notes := big.notesCopy(); len(notes) == 0 {
		t.Fatal("plan must pin to working notes")
	}
	small := testRun(Profile{})
	small.Request = "hi"
	e.ensureLHMilestones(small)
	small.mu.Lock()
	defer small.mu.Unlock()
	if !small.lhInit || small.lhLarge || len(small.lhMilestones) != 0 {
		t.Fatalf("small task must stay plan-free: %+v", small.lhMilestones)
	}
	// Progress checks fire every N steps with the next milestone.
	big2 := testRun(Profile{})
	big2.Request = "Migrate the service: " + strings.Repeat("do the work and verify. ", 30)
	e.ensureLHMilestones(big2)
	big2.mu.Lock()
	big2.Steps = lh.MilestoneEvery
	big2.mu.Unlock()
	e.lhProgressCheck(big2)
	found := false
	for _, n := range big2.VolatileNotes() {
		if strings.Contains(n, "milestone plan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("progress check must nudge the next milestone: %v", big2.VolatileNotes())
	}
}

// The pacing line renders elapsed/remaining budget plus milestone
// progress, and stays silent with the switch off.
func TestLongHorizonPacing(t *testing.T) {
	run := NewRun("r1", "t1", Profile{Limit: tasks.Limit{MaxSteps: 25, MaxSeconds: 3600}})
	run.Tracker.Observe(tasks.Usage{Steps: 8})
	line := run.LongHorizonPacing(run.StartedAt.Add(150 * time.Second))
	for _, want := range []string{"pacing:", "elapsed", "steps 8/25", "time"} {
		if !strings.Contains(line, want) {
			t.Fatalf("pacing must contain %q, got %q", want, line)
		}
	}
	t.Setenv("EXCALIBUR_LONG_HORIZON", "off")
	if got := run.LongHorizonPacing(run.StartedAt.Add(150 * time.Second)); got != "" {
		t.Fatalf("switch off must stay silent, got %q", got)
	}
}

// Long commands earn the background+poll nudge; backgrounded or short
// commands stay silent.
func TestLongHorizonBackgroundNudge(t *testing.T) {
	e := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: scriptedRunner()})
	lh := tasks.DefaultLongHorizonConfig()
	e.deps.LongHorizon = &lh
	run := testRun(Profile{})
	batch := []providers.ToolCall{toolCall("c1", "exec", `{"command":"make -j4","background":false}`)}
	e.lhBackgroundNudge(run, batch, nil)
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "background:true") {
			found = true
		}
	}
	if !found {
		t.Fatalf("long command must nudge background+poll: %v", run.VolatileNotes())
	}
	quiet := testRun(Profile{})
	e.lhBackgroundNudge(quiet, []providers.ToolCall{toolCall("c1", "exec", `{"command":"echo hi","background":false}`)}, nil)
	for _, n := range quiet.VolatileNotes() {
		if strings.Contains(n, "background:true") {
			t.Fatalf("short command must stay silent, got %q", n)
		}
	}
}

// After the continuation bound the run enters closing mode: the
// builder renders tool_choice none from then on.
func TestLongHorizonBoundThenClosing(t *testing.T) {
	e := New(Deps{Models: &fakeEndpoint{}, Context: &fakeBuilder{}, Tools: scriptedRunner()})
	lh := tasks.DefaultLongHorizonConfig()
	lh.MaxContinuations = 1
	e.deps.LongHorizon = &lh
	run := testRun(Profile{Limit: tasks.Limit{MaxSteps: 25}})
	run.Request = "Create the file /app/filter.py that strips scripts and verify it by running the checks."
	run.History = writeHistory("/app/filter.py")
	rep := tasks.GateReport{OK: true}
	if cont, _ := e.lhShouldContinue(run, rep); !cont {
		t.Fatal("first thin finish with time left must continue")
	}
	if cont, _ := e.lhShouldContinue(run, rep); cont {
		t.Fatal("second attempt must stand down at the bound")
	}
	if !e.lhClosingNow(run) {
		t.Fatal("bound must force closing mode")
	}
	_ = ctxcompiler.Compiled{}
	_ = types.RunCompleted
	_ = context.Background
	_ = execHistoryAlive
}
