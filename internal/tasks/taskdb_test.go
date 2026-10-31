// task_db tests (Section 8.10; verification rows 8.11.1 and
// 8.11.9): a batch creating a dependency cycle or naming something
// missing is rejected whole with the concrete reason and the
// database unchanged; concurrent updates of one row with one
// version apply once and reject with the current state; queries
// respect 20 rows and ~2000 tokens with a has-more indicator.
package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func seedPlanItems(t *testing.T, store *MemoryStore, planID string) (string, string) {
	t.Helper()
	for _, it := range []PlanItem{
		{ID: "taskdb_a", PlanID: planID, Code: "S1", Title: "a", State: types.PlanItemPending, Version: 1},
		{ID: "taskdb_b", PlanID: planID, Code: "S2", Title: "b", State: types.PlanItemPending, Version: 1},
	} {
		if err := store.AddPlanItem(it); err != nil {
			t.Fatal(err)
		}
	}
	return "taskdb_a", "taskdb_b"
}

func TestBatchRejectsCycleWhole(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	planID := "pln_taskdb"
	if err := store.CreatePlan(Plan{ID: planID, Scope: scope, Type: types.PlanMacro, Version: 1, State: PlanActive}); err != nil {
		t.Fatal(err)
	}
	a, b := seedPlanItems(t, store, planID)
	ops := []OpCall{
		{Op: "add_dependency", Params: map[string]any{"plan": planID, "from": a, "to": b}},
		{Op: "add_dependency", Params: map[string]any{"plan": planID, "from": b, "to": a}},
	}
	_, err := ApplyBatch(store, scope, ops)
	if err == nil {
		t.Fatal("cyclic batch must be rejected")
	}
	if be, ok := err.(*BatchError); !ok || be.Index != 1 || !strings.Contains(be.Error(), "cycle") {
		t.Fatalf("concrete cycle reason expected, got %v", err)
	}
	if len(store.ListDependencies(planID)) != 0 {
		t.Fatal("rejected batch must leave the database unchanged")
	}
}

func TestBatchRejectsMissingReference(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	ops := []OpCall{
		{Op: "add_criterion", Params: map[string]any{"requirement": "R99", "statement": "x", "method": "test"}},
		{Op: "add_requirement", Params: map[string]any{"text": "must not land"}},
	}
	_, err := ApplyBatch(store, scope, ops)
	if err == nil {
		t.Fatal("batch with a missing reference must be rejected")
	}
	if !strings.Contains(err.Error(), "R99") {
		t.Fatalf("error must name the missing reference: %v", err)
	}
	if len(store.ListRequirements(scope)) != 0 {
		t.Fatal("rejected batch must apply nothing")
	}
}

func TestBatchGroupsOpsInOneTransaction(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	res, err := ApplyBatch(store, scope, []OpCall{
		{Op: "add_requirement", Params: map[string]any{"text": "keep headers", "priority": "must"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	code := res[0].Code // e.g. "R1"
	res, err = ApplyBatch(store, scope, []OpCall{
		{Op: "add_criterion", Params: map[string]any{"requirement": code, "statement": "PDF keeps headers", "method": "test"}},
		{Op: "query", Params: map[string]any{
			"entity": "criteria",
			"where":  map[string]any{"status": []any{"unverified"}},
			"fields": []any{"code", "statement", "status"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[1].Rows == nil {
		t.Fatalf("query must run in the same batch: %+v", res)
	}
	if len(res[1].Rows) != 1 || res[1].Rows[0]["code"] != res[0].Code {
		t.Fatalf("query rows wrong: %+v vs %q", res[1].Rows, res[0].Code)
	}
	if _, ok := res[1].Rows[0]["statement"]; !ok {
		t.Fatalf("requested fields missing: %+v", res[1].Rows)
	}
}

func TestOptimisticConcurrency(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	res, err := ApplyBatch(store, scope, []OpCall{
		{Op: "add_requirement", Params: map[string]any{"text": "keep headers"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res[0].ID
	r, err := store.GetRequirement(types.RequirementID(id))
	if err != nil {
		t.Fatal(err)
	}
	// First writer wins with the read version.
	if err := store.UpdateRequirement(r, r.Version); err != nil {
		t.Fatal(err)
	}
	// Second writer with the same version is rejected with the
	// current row.
	err = store.UpdateRequirement(r, r.Version)
	vc, ok := err.(*VersionConflict)
	if !ok {
		t.Fatalf("stale write must raise VersionConflict, got %v", err)
	}
	if vc.Have != r.Version+1 || vc.Want != r.Version {
		t.Fatalf("conflict must carry current state: %+v", vc)
	}
	if vc.Current == nil {
		t.Fatal("conflict must carry the current row for fresh decisions")
	}
}

func TestQueryCapsRowsAndTokens(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	for i := 0; i < 30; i++ {
		req := seedRequirement(store, scope, "req")
		_, _ = seedCriterion(store, req, req.Code.Number(), types.CriterionTest, "f.go")
	}
	res, err := ApplyBatch(store, scope, []OpCall{
		{Op: "query", Params: map[string]any{"entity": "criteria", "fields": []any{"code", "status"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := res[0]
	if len(q.Rows) != DefaultQueryRows {
		t.Fatalf("default cap is 20 rows, got %d", len(q.Rows))
	}
	if !q.HasMore {
		t.Fatal("has-more indicator required past the cap")
	}
	if q.Tokens > DefaultQueryTokens*2 {
		t.Fatalf("token estimate out of range: %d", q.Tokens)
	}
}
