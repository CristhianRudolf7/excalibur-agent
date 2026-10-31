// Plan tests (Section 8.4): macro creation without duplicates,
// approval flow, micro plan lifecycle, and the compact injection.
package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestEnsureMacroPlanOnce(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	first, err := EnsureMacroPlan(store, scope)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureMacroPlan(store, scope)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatal("macro plan must not duplicate")
	}
}

func TestApprovalFlow(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	if NeedsApproval(types.TaskN1, false) {
		t.Fatal("N1 without a request needs no approval")
	}
	if !NeedsApproval(types.TaskN3, false) || !NeedsApproval(types.TaskN1, true) {
		t.Fatal("N3 or an explicit request needs approval")
	}
	p, err := EnsureMacroPlan(store, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := SubmitForApproval(store, p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetPlan(p.ID)
	if got.State != PlanPending {
		t.Fatalf("plan must wait for approval: %q", got.State)
	}
	comments, err := ApprovePlan(store, p.ID, []string{"split step 2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments become directives: %v", comments)
	}
	if _, err := ApprovePlan(store, p.ID, nil); err == nil {
		t.Fatal("double approval must fail")
	}
}

func TestMicroPlanLifecycle(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	task := types.NewTaskID()
	micro, err := EnsureMicroPlan(store, task, scope)
	if err != nil {
		t.Fatal(err)
	}
	if micro.Type != types.PlanMicro || micro.TaskID != task {
		t.Fatalf("micro plan must belong to its task: %+v", micro)
	}
	again, err := EnsureMicroPlan(store, task, scope)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != micro.ID {
		t.Fatal("active micro plan must be reused")
	}
	if err := ArchiveMicroPlan(store, task, scope); err != nil {
		t.Fatal(err)
	}
	fresh, err := EnsureMicroPlan(store, task, scope)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == micro.ID {
		t.Fatal("archived micro plan must not be reused")
	}
}

func TestCompactMicro(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	task := types.NewTaskID()
	micro, _ := EnsureMicroPlan(store, task, scope)
	for _, it := range []PlanItem{
		{ID: "m1", PlanID: micro.ID, Code: "S1", Title: "draft", State: types.PlanItemDone, Version: 1},
		{ID: "m2", PlanID: micro.ID, Code: "S2", Title: "verify", State: types.PlanItemInProgress, Agent: "child", Version: 1},
	} {
		if err := store.AddPlanItem(it); err != nil {
			t.Fatal(err)
		}
	}
	got := CompactMicro(store, micro.ID)
	if !strings.Contains(got, "[x] S1 draft") || !strings.Contains(got, "[>] S2 verify @child") {
		t.Fatalf("compact render wrong:\n%s", got)
	}
	if !strings.Contains(got, "1/2 done") {
		t.Fatalf("progress missing:\n%s", got)
	}
}
