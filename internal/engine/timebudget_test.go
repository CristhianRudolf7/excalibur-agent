package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// The Harbor override resolves to the 90% closing budget only when no
// explicit flag is set (Sections 23.2, docs/decisions/0037).
func TestResolveMaxSeconds(t *testing.T) {
	t.Setenv(TimeBudgetSwitch, "1")
	if got := ResolveMaxSeconds(120); got != 120 {
		t.Fatalf("explicit must win, got %d", got)
	}
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "1000")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	if got := ResolveMaxSeconds(0); got != 900 {
		t.Fatalf("90%% of 1000 = 900, got %d", got)
	}
	// First parseable env wins; garbage is skipped.
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "junk")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "500")
	if got := ResolveMaxSeconds(0); got != 450 {
		t.Fatalf("fallback env 90%% of 500 = 450, got %d", got)
	}
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "-5")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "0")
	if got := ResolveMaxSeconds(0); got != 0 {
		t.Fatalf("non-positive override means no budget, got %d", got)
	}
	// The switch disables the override path but not the explicit flag.
	t.Setenv(TimeBudgetSwitch, "0")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "1000")
	if got := ResolveMaxSeconds(0); got != 0 {
		t.Fatalf("disabled switch must ignore env, got %d", got)
	}
	if got := ResolveMaxSeconds(60); got != 0 {
		t.Fatalf("disabled switch must fall back to the context deadline, got %d", got)
	}
}

func TestFormatRemaining(t *testing.T) {
	cases := map[time.Duration]string{
		3*time.Hour + 12*time.Minute:   "3h12m left",
		42*time.Minute + 5*time.Second: "42m left",
		95 * time.Second:               "95s left",
		0:                              "0s left",
		-5 * time.Second:               "overdue 5s",
		-90 * time.Minute:              "overdue 1h30m",
	}
	for in, want := range cases {
		if got := FormatRemaining(in); got != want {
			t.Fatalf("FormatRemaining(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestBudgetLineSegments(t *testing.T) {
	run := NewRun("r1", "t1", Profile{Limit: tasks.Limit{
		MaxSteps: 25, MaxToolCalls: 50, MaxTokens: 10000,
		MaxCostUSD: 0.5, MaxSeconds: 3600,
	}})
	run.Tracker.Observe(tasks.Usage{Steps: 12, ToolCalls: 8, InputTokens: 2000, OutputTokens: 1210, CostUSD: 0.012})
	now := run.StartedAt.Add(18 * time.Minute)
	line := run.BudgetLine(now)
	want := "steps 12/25, tools 8/50, tokens 3210/10000, cost $0.012/$0.5, time 42m left of 1h0m"
	if line != want {
		t.Fatalf("BudgetLine = %q, want %q", line, want)
	}
	// Uncapped runs add no volatile bytes.
	bare := NewRun("r2", "t1", Profile{})
	if line := bare.BudgetLine(time.Now()); line != "" {
		t.Fatalf("uncapped run must render empty, got %q", line)
	}
	// Overdue time reads honest.
	late := NewRun("r3", "t1", Profile{Limit: tasks.Limit{MaxSeconds: 60}})
	if line := late.BudgetLine(late.StartedAt.Add(65 * time.Second)); line != "time overdue 5s of 60s" {
		t.Fatalf("overdue line = %q", line)
	}
}

func TestBuilderFillsBudgetState(t *testing.T) {
	run := NewRun("r1", "t1", Profile{Limit: tasks.Limit{MaxSteps: 10}})
	b := &DefaultBuilder{
		Config:  ctxcompiler.DefaultConfig(),
		Counter: ctxcompiler.NewEstimator(0),
		Snapshot: func(ctx context.Context, r *Run) (ctxcompiler.Snapshot, error) {
			return ctxcompiler.Snapshot{}, nil
		},
		Budget: func(r *Run) string { return "steps 0/10" },
	}
	_, compiled, err := b.BuildRequest(context.Background(), run, false)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Layers[ctxcompiler.LayerVolatile] == "" {
		t.Fatal("volatile layer must render")
	}
	if got := compiled.LayerText(ctxcompiler.LayerVolatile); !strings.Contains(got, "steps 0/10") {
		t.Fatalf("budget hook must fill BudgetState, got:\n%s", got)
	}
	// A snapshot-carried value always wins over the hook.
	b2 := &DefaultBuilder{
		Config:  ctxcompiler.DefaultConfig(),
		Counter: ctxcompiler.NewEstimator(0),
		Snapshot: func(ctx context.Context, r *Run) (ctxcompiler.Snapshot, error) {
			return ctxcompiler.Snapshot{Volatile: ctxcompiler.Volatile{BudgetState: "pinned"}}, nil
		},
		Budget: func(r *Run) string { return "hook" },
	}
	_, compiled2, err := b2.BuildRequest(context.Background(), run, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled2.LayerText(ctxcompiler.LayerVolatile); !strings.Contains(got, "pinned") || strings.Contains(got, "hook") {
		t.Fatalf("snapshot value must win, got:\n%s", got)
	}
}

// A wall-clock budget trips closing mode below 10% and pauses with
// the budget motive at zero (Sections 7.3, 18.4).
func TestTimeBudgetClosingThenPaused(t *testing.T) {
	e := New(Deps{})
	run := NewRun("r1", "t1", Profile{Limit: tasks.Limit{MaxSteps: 1000, MaxSeconds: 100}})
	start := time.Now().UTC()
	run.StartedAt = start
	run.HeartbeatAt = start.Add(92 * time.Second)
	if got := e.checkBudgets(run, 92*time.Second); got != tasks.StatusClosing {
		t.Fatalf("8%% left must be closing, got %v", got)
	}
	run.HeartbeatAt = start.Add(100 * time.Second)
	if got := e.checkBudgets(run, 100*time.Second); got != tasks.StatusExhausted {
		t.Fatalf("zero left must exhaust, got %v", got)
	}
}

func TestExpiredDeadlinePausesRunningEngine(t *testing.T) {
	e, js := testEngine(&fakeEndpoint{}, &fakeRunner{}, passingGate())
	run := testRun(Profile{Limit: tasks.Limit{MaxSeconds: 1}})
	startRun(t, e, run, js)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunPaused)
}
