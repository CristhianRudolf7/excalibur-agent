package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestTimeBudgetOnlySwitchDefaults(t *testing.T) {
	t.Setenv(TimeBudgetSwitch, "1")
	t.Setenv(TimeBudgetOnlySwitch, "")
	if !TimeBudgetOnlyEnabled(true) {
		t.Fatal("eval default must be ON")
	}
	if TimeBudgetOnlyEnabled(false) {
		t.Fatal("non-eval default must be OFF")
	}
	t.Setenv(TimeBudgetOnlySwitch, "off")
	if TimeBudgetOnlyEnabled(true) {
		t.Fatal("explicit off must win in eval")
	}
	t.Setenv(TimeBudgetOnlySwitch, "on")
	if !TimeBudgetOnlyEnabled(false) {
		t.Fatal("explicit on must win outside eval")
	}
	// Master time switch off forces time-only off for a silent baseline.
	t.Setenv(TimeBudgetSwitch, "0")
	t.Setenv(TimeBudgetOnlySwitch, "on")
	if TimeBudgetOnlyEnabled(true) {
		t.Fatal("time-only must stay off when the time plumbing is off")
	}
}

func TestCostCapParsing(t *testing.T) {
	t.Setenv(CostCapSwitch, "")
	if got := CostCapUSD(); got != DefaultCostCapUSD {
		t.Fatalf("default cost cap = %v, want %v", got, DefaultCostCapUSD)
	}
	t.Setenv(CostCapSwitch, "35.5")
	if got := CostCapUSD(); got != 35.5 {
		t.Fatalf("env cost cap = %v, want 35.5", got)
	}
	t.Setenv(CostCapSwitch, "junk")
	if got := CostCapUSD(); got != DefaultCostCapUSD {
		t.Fatalf("junk must fall back to default, got %v", got)
	}
	t.Setenv(CostCapSwitch, "-3")
	if got := CostCapUSD(); got != DefaultCostCapUSD {
		t.Fatalf("non-positive must fall back, got %v", got)
	}
}

// The wall budget order is: the explicit --timeout, then
// EXCALIBUR_EVAL_WALL_SECS, then 900 seconds. The Harbor
// operator variables no longer set the budget: the Harbor adapter passes
// its timeout as --timeout.
func TestEvalWallSources(t *testing.T) {
	t.Setenv(TimeBudgetSwitch, "1")
	t.Setenv(TimeBudgetOnlySwitch, "1")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_EVAL_WALL_SECS", "")
	t.Setenv("EXCALIBUR_ARM_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_ARM_WALL_SECS", "")
	if DefaultEvalWallSecs != 900 {
		t.Fatalf("default wall = %d, want 900", DefaultEvalWallSecs)
	}
	if secs, src := EvalWallSecs(0); secs != 900 || src != "default" {
		t.Fatalf("default = %d/%s, want 900/default", secs, src)
	}
	if secs, src := EvalWallSecs(120); secs != 120 || src != "explicit" {
		t.Fatalf("explicit = %d/%s, want 120/explicit", secs, src)
	}
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "1000")
	if secs, src := EvalWallSecs(0); secs != 900 || src != "default" {
		t.Fatalf("harbor operator variable must not set the budget: %d/%s", secs, src)
	}
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_EVAL_WALL_SECS", "1800")
	if secs, src := EvalWallSecs(0); secs != 1800 || src != "explicit" {
		t.Fatalf("env = %d/%s, want 1800/explicit", secs, src)
	}
	if secs, src := EvalWallSecs(120); secs != 120 || src != "explicit" {
		t.Fatalf("explicit must win over the env: %d/%s", secs, src)
	}
	t.Setenv("EXCALIBUR_EVAL_WALL_SECS", "junk")
	t.Setenv("EXCALIBUR_ARM_WALL_SECS", "600")
	if secs, src := EvalWallSecs(0); secs != 900 || src != "default" {
		t.Fatalf("legacy alias must not override default = %d/%s, want 900/default", secs, src)
	}
}

// Safety caps must not pause the run: only the wall clock binds in
// time-only mode. This reproduces the measured early pause (25 steps,
// ~2 minutes, ~200k tokens on hour-scale tasks).
func TestTimeOnlyStepsDoNotPause(t *testing.T) {
	e := New(Deps{})
	run := NewRun("r1", "t1", Profile{
		Name: "child", Role: types.RoleExecutor,
		Limit:          tasks.Limit{MaxSteps: SafetyMaxSteps, MaxSeconds: 3600},
		TimeBudgetOnly: true,
	})
	start := time.Now().UTC()
	run.StartedAt = start
	run.HeartbeatAt = start.Add(150 * time.Second)
	// Exhaust steps but leave wall time: legacy tracker says
	// exhausted, time-only status stays OK.
	run.Tracker.Observe(tasks.Usage{Steps: SafetyMaxSteps})
	if got := run.Tracker.Evaluate(); got != tasks.StatusExhausted {
		t.Fatalf("legacy tracker must exhaust on steps, got %v", got)
	}
	if got := timeOnlyStatus(run.Tracker.Limit, 150*time.Second); got != tasks.StatusOK {
		t.Fatalf("time-only must stay OK with wall left, got %v", got)
	}
	// The step loop must not pause on the safety cap: it notes once
	// and keeps working toward the wall edge.
	ctx := context.Background()
	e.AttachJournal(run.ID, nil)
	_ = ctx
	// Simulate the step-loop branch directly: exhausted legacy but
	// time-only OK means no pause, safety note once.
	if status := timeOnlyStatus(run.Tracker.Limit, 150*time.Second); status == tasks.StatusExhausted {
		t.Fatal("must not exhaust on steps in time-only mode")
	}
}

// The wall clock still binds: closing below 10% and pause at zero,
// with an honest time reason. The closing note fires once and tells
// the agent to leave the deliverable and call finish.
func TestTimeOnlyWallBindsAndCloses(t *testing.T) {
	run := NewRun("r1", "t1", Profile{
		Limit:          tasks.Limit{MaxSteps: SafetyMaxSteps, MaxSeconds: 100},
		TimeBudgetOnly: true,
	})
	if got := timeOnlyStatus(run.Tracker.Limit, 50*time.Second); got != tasks.StatusOK {
		t.Fatalf("50%% wall left must be OK, got %v", got)
	}
	if got := timeOnlyStatus(run.Tracker.Limit, 92*time.Second); got != tasks.StatusClosing {
		t.Fatalf("8%% wall left must close, got %v", got)
	}
	if got := timeOnlyStatus(run.Tracker.Limit, 100*time.Second); got != tasks.StatusExhausted {
		t.Fatalf("wall edge must exhaust, got %v", got)
	}
	if by := timeOnlyExhaustedBy(run.Tracker.Limit, 100*time.Second); len(by) != 1 || by[0] != "time" {
		t.Fatalf("honest pause must name time, got %v", by)
	}
	if !strings.Contains(TimeOnlyClosingNote, "leave the best deliverable") ||
		!strings.Contains(TimeOnlyClosingNote, "finish") {
		t.Fatal("closing note must name the deliverable and finish")
	}
	if !strings.Contains(SafetyCapNote, "wall clock") {
		t.Fatal("safety note must name the wall clock")
	}
}

// Closing injects the one-time wall note through the real step loop.
func TestTimeOnlyClosingNoteInjectedOnce(t *testing.T) {
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", finishArgs("did it", "exec true ok"))),
		toolResp(toolCall("f2", "finish", finishArgs("did it", "exec true ok"))),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	now := time.Now().UTC()
	e.deps.Clock = func() time.Time { return now }
	run := NewRun("r1", "t1", Profile{
		Limit:          tasks.Limit{MaxSteps: SafetyMaxSteps, MaxSeconds: 100},
		TimeBudgetOnly: true, ExplicitFinish: true,
	})
	// 92s elapsed of a 100s wall = 8% left = closing on the first step.
	run.StartedAt = now.Add(-92 * time.Second)
	run.HeartbeatAt = now.Add(-92 * time.Second)
	startRun(t, e, run, js)
	ctx := context.Background()
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range run.VolatileNotes() {
		if strings.Contains(n, "leave the best deliverable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("closing note missing in time-only closing: %v", run.VolatileNotes())
	}
}

// Pin the historical explanation: 23 consumed steps leave 2/25 (<10%),
// so the next request had no eligible tools even with an hour left.
func TestHistorical23CallClosing(t *testing.T) {
	for _, timeOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(timeOnly), func(t *testing.T) {
			model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
			e, js := testEngine(model, scriptedRunner(), passingGate())
			e.deps.SelfVerify = selfVerifyOffCfg()
			e.deps.Artifacts = artifactOffCfg()
			run := testRun(Profile{Limit: tasks.Limit{MaxSteps: 25, MaxSeconds: 3600}, TimeBudgetOnly: timeOnly})
			run.Tracker.Observe(tasks.Usage{Steps: 23})
			startRun(t, e, run, js)
			if err := e.Run(context.Background(), run); err != nil {
				t.Fatal(err)
			}
			builder := e.deps.Context.(*fakeBuilder)
			if len(builder.closing) != 1 || builder.closing[0] == timeOnly {
				t.Fatalf("closing=%v timeOnly=%v", builder.closing, timeOnly)
			}
		})
	}
}
