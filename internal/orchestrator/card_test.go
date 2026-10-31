package orchestrator

import (
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testCard() DelegationCard {
	return DelegationCard{
		Name: "Lena", Description: "implements module X",
		Profile: agents.ProfileImplementer,
		Goal:    "implement X", Deliverable: "module X with tests",
		Scope: types.Scope{Kind: types.ScopeProject, ID: "p1"},
		Paths: []string{"src/x"}, Done: []string{"tests pass"},
		Report: "summary, files, evidence",
	}
}

func TestCardValidate(t *testing.T) {
	now := time.Now().UTC()
	if err := testCard().Validate(now, 2); err != nil {
		t.Fatalf("valid card rejected: %v", err)
	}
	bad := testCard()
	bad.Goal = ""
	if err := bad.Validate(now, 2); err == nil {
		t.Fatal("card without goal accepted")
	}
	bad = testCard()
	bad.Tools = []string{"exec", "nuke_the_site"}
	if err := bad.Validate(now, 2); err == nil {
		t.Fatal("card with out-of-profile tool accepted")
	}
	bad = testCard()
	bad.Deadline = now.Add(-time.Hour)
	if err := bad.Validate(now, 2); err == nil {
		t.Fatal("card with past deadline accepted")
	}
	bad = testCard()
	bad.Profile = "nope"
	if err := bad.Validate(now, 2); err == nil {
		t.Fatal("card with unknown profile accepted")
	}
}

func TestAdviseFanOut(t *testing.T) {
	a := AdviseFanOut([]FanOutReason{FanOutParallel}, [][]string{{"a"}, {"b"}})
	if !a.Spawn || len(a.Warnings) != 0 {
		t.Fatalf("parallel disjoint must spawn cleanly: %+v", a)
	}
	a = AdviseFanOut([]FanOutReason{FanOutCoupled}, nil)
	if a.Spawn {
		t.Fatal("coupled sequential work must not fan out")
	}
	if len(a.Warnings) == 0 {
		t.Fatal("coupled sequential work must warn")
	}
	a = AdviseFanOut([]FanOutReason{FanOutVerify, FanOutIsolation}, [][]string{{"src/a"}, {"src/a/b"}})
	if !a.Spawn {
		t.Fatal("verification and isolation must spawn")
	}
	if len(a.Warnings) == 0 {
		t.Fatal("overlapping file sets must warn")
	}
	a = AdviseFanOut([]FanOutReason{FanOutParentWork}, nil)
	if a.Spawn {
		t.Fatal("parent-starts-alone alone must not spawn")
	}
}

func TestRemainingBudget(t *testing.T) {
	parent := types.Budget{MaxSteps: 150, MaxTokens: 1000, MaxCostUSD: 5, MaxSeconds: 600, MaxProcs: 4}
	used := Usage{Steps: 50, InputTokens: 300, OutputTokens: 100, CostUSD: 1, Elapsed: 60 * time.Second}
	rest := RemainingBudget(parent, used)
	if rest.MaxSteps != 100 || rest.MaxTokens != 600 {
		t.Fatalf("bad remainder: %+v", rest)
	}
	if rest.MaxCostUSD != 4 || rest.MaxSeconds != 540 {
		t.Fatalf("bad remainder: %+v", rest)
	}
	if rest.MaxProcs != 4 {
		t.Fatal("procs cap is shared, not consumed")
	}
	// A child never gets more than the parent has left.
	child := ClampChildBudget(parent, used, types.Budget{MaxSteps: 500}, agents.ChildDefaultSteps)
	if child.MaxSteps != 100 {
		t.Fatalf("child budget not clamped: %+v", child)
	}
	child = ClampChildBudget(parent, used, types.Budget{}, agents.ChildDefaultSteps)
	if child.MaxSteps != agents.ChildDefaultSteps {
		t.Fatalf("child default steps not applied: %+v", child)
	}
	shares := SplitBudget(parent, used, []float64{1, 1})
	if shares[0].MaxSteps+shares[1].MaxSteps > 100 {
		t.Fatalf("shares exceed remainder: %+v", shares)
	}
	if shares[0].MaxSteps != 50 || shares[1].MaxSteps != 50 {
		t.Fatalf("shares not proportional: %+v", shares)
	}
}

func TestPlanUndo(t *testing.T) {
	steps := PlanUndo([]EffectRecord{
		{Kind: EffectEdit, Tool: "edit_file", Detail: "src/a.go"},
		{Kind: EffectCommand, Tool: "exec", Detail: "go test ./..."},
		{Kind: EffectExternal, Tool: "calendar", Detail: "event created", HasInverse: true},
		{Kind: EffectExternal, Tool: "email", Detail: "mail sent"},
	})
	if len(steps) != 4 {
		t.Fatalf("want 4 steps, got %d", len(steps))
	}
	// Reverse (saga) order: mail first.
	if steps[0].Reversible {
		t.Fatal("sent mail without inverse must be not reversible")
	}
	if !steps[1].Reversible || steps[1].Inverse != "calendar" {
		t.Fatalf("calendar compensation missing: %+v", steps[1])
	}
	if !steps[2].Reversible || !steps[3].Reversible {
		t.Fatal("command and edit must be reversible through the harness")
	}
}
