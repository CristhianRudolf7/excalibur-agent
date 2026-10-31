// Graph tests (Sections 8.2, 8.5): cycle detection on insert,
// descendant computation and the deterministic affected subgraph.
package tasks

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestWouldCycleRejectsLoop(t *testing.T) {
	deps := []Dependency{
		{From: "a", To: "b", Type: types.DependencyBlocks},
		{From: "b", To: "c", Type: types.DependencyBlocks},
	}
	if !WouldCycle([]string{"a", "b", "c"}, deps, "c", "a") {
		t.Fatal("c -> a closes a cycle and must be rejected")
	}
	if WouldCycle([]string{"a", "b", "c"}, deps, "a", "c") {
		t.Fatal("a -> c is acyclic and must be accepted")
	}
	if !WouldCycle([]string{"a"}, nil, "a", "a") {
		t.Fatal("self loop must be rejected")
	}
}

func TestFindCycleReportsStableCycle(t *testing.T) {
	deps := []Dependency{
		{From: "a", To: "b", Type: types.DependencyBlocks},
		{From: "b", To: "a", Type: types.DependencyBlocks},
	}
	cycle := FindCycle([]string{"a", "b"}, deps)
	if cycle == nil {
		t.Fatal("expected a cycle")
	}
	if cycle[0] != cycle[len(cycle)-1] {
		t.Fatalf("cycle must close on itself: %v", cycle)
	}
	if FindCycle([]string{"a", "b"}, deps[:1]) != nil {
		t.Fatal("single edge is acyclic")
	}
}

func TestDescendantsSorted(t *testing.T) {
	deps := []Dependency{
		{From: "a", To: "c", Type: types.DependencyBlocks},
		{From: "a", To: "b", Type: types.DependencyBlocks},
		{From: "b", To: "d", Type: types.DependencyInforms},
	}
	got := Descendants(deps, []string{"a"})
	want := []string{"b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestAffectedSubgraphIncludesChangedRequirements(t *testing.T) {
	deps := []Dependency{{From: "i1", To: "i2", Type: types.DependencyBlocks}}
	taskReqs := map[string][]string{"i1": {"R1"}, "i9": {"R2"}}
	got := AffectedSubgraph(deps, []string{"i1"}, taskReqs, []string{"R2"})
	if len(got) != 3 || got[0] != "i1" || got[1] != "i2" || got[2] != "i9" {
		t.Fatalf("affected subgraph wrong: %v", got)
	}
}

func TestValidatePlan(t *testing.T) {
	req := types.RequirementID("req_01JTESTTESTTESTTESTTES")
	crit := types.CriterionID("crt_01JTESTTESTTESTTESTTES")
	snap := PlanSnapshot{
		Items:        map[string]PlanItem{"a": {}, "b": {}},
		Dependencies: []Dependency{{From: "a", To: "ghost", Type: types.DependencyBlocks}},
		ReqCriteria:  map[types.RequirementID][]types.CriterionID{req: {}},
	}
	errs := ValidatePlan(snap)
	if len(errs) != 2 {
		t.Fatalf("expected missing-endpoint + empty-criteria errors, got %v", errs)
	}
	snap.Dependencies = []Dependency{
		{From: "a", To: "b", Type: types.DependencyBlocks},
		{From: "b", To: "a", Type: types.DependencyBlocks},
	}
	snap.ReqCriteria[req] = []types.CriterionID{crit}
	snap.CriterionMethods = map[types.CriterionID]types.CriterionMethod{}
	errs = ValidatePlan(snap)
	if len(errs) != 2 {
		t.Fatalf("expected cycle + missing-method errors, got %v", errs)
	}
}
