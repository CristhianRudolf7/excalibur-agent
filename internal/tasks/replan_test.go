// Replan tests (Section 8.5; verification row 8.11.5): after a
// changed requirement the affected subgraph is correct, finished
// non-invalidated work is frozen, the new plan version lands with
// the previous one queryable, and directives go out.
package tasks

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func seedPlan(t *testing.T, store *MemoryStore, scope types.Scope) string {
	t.Helper()
	p := Plan{ID: "pln_test", Scope: scope, Type: types.PlanMacro, Version: 1, State: PlanActive}
	if err := store.CreatePlan(p); err != nil {
		t.Fatal(err)
	}
	add := func(id, code string, state types.PlanItemState) {
		if err := store.AddPlanItem(PlanItem{ID: id, Code: code, PlanID: p.ID, Title: code, State: state, Version: 1}); err != nil {
			t.Fatal(err)
		}
	}
	add("i1", "S1", types.PlanItemDone)
	add("i2", "S2", types.PlanItemInProgress)
	add("i3", "S3", types.PlanItemPending)
	if err := store.AddDependency(p.ID, Dependency{From: "i2", To: "i3", Type: types.DependencyBlocks}); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestReplanFreezesFinishedWork(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	planID := seedPlan(t, store, scope)

	req := seedRequirement(store, scope, "changed req")
	itemReqs := map[string][]string{"i2": {string(req.ID)}}
	runOfItem := map[string]string{"i2": "run_x", "i3": "run_y"}

	diff := ReplanDiff{
		Add:    []PlanItem{{ID: "i4", Code: "S4", Title: "new step"}},
		Remove: []string{"i3"},
	}
	res, err := ApplyReplan(store, planID,
		ReplanRequest{PlanID: planID, Motive: TriggerRequirementFlip, ChangedReqs: []types.RequirementID{req.ID}},
		diff, itemReqs, runOfItem, true)
	if err != nil {
		t.Fatal(err)
	}
	// Affected: i2 (linked) + i3 (descendant).
	if len(res.Affected) != 2 || res.Affected[0] != "i2" || res.Affected[1] != "i3" {
		t.Fatalf("affected subgraph wrong: %v", res.Affected)
	}
	// Frozen: finished i1, untouched by the change.
	if len(res.Frozen) != 1 || res.Frozen[0] != "i1" {
		t.Fatalf("finished work not frozen: %v", res.Frozen)
	}
	if res.NewVersion != 2 {
		t.Fatalf("expected version 2, got %d", res.NewVersion)
	}
	if len(store.PlanVersions(planID)) != 2 {
		t.Fatal("previous plan version must stay queryable")
	}
	removed, err := store.GetPlanItem("i3")
	if err != nil {
		t.Fatal(err)
	}
	if removed.State != types.PlanItemCanceled {
		t.Fatalf("removed item not canceled: %q", removed.State)
	}
	if len(res.Directives) != 2 {
		t.Fatalf("directives must reach both changed runs: %v", res.Directives)
	}
	if !res.NotifyMaster {
		t.Fatal("scope change must notify the master")
	}
}

func TestReplanRejectsFrozenRemoval(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	planID := seedPlan(t, store, scope)
	_, err := ApplyReplan(store, planID,
		ReplanRequest{PlanID: planID, Motive: TriggerUserAsked, ChangedNodes: []string{"i2"}},
		ReplanDiff{Remove: []string{"i1"}}, nil, nil, false)
	_ = err
	// i1 is finished and outside the affected subgraph: removal
	// must fail. i1 was finished before anything changed.
	if err == nil {
		t.Fatal("replan must not remove frozen finished work")
	}
}

func TestReplanCriteriaInvariant(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	planID := seedPlan(t, store, scope)
	req := seedRequirement(store, scope, "req")
	_, err := ApplyReplan(store, planID,
		ReplanRequest{PlanID: planID, Motive: TriggerCheckFailed3x},
		ReplanDiff{CriteriaChanged: map[types.RequirementID][]types.Criterion{req.ID: {}}}, nil, nil, false)
	if err == nil {
		t.Fatal("empty criteria set must violate the plan invariant")
	}
}
