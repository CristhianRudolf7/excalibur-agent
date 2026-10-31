package orchestrator_test

import (
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 15.7 point 4: one priority per case. Mirrors the 26.11
// walkthrough: the implementer inside the impact area is interrupted,
// the test runner under a graceful process goes normal.

// Explicit stop orders always interrupt.
func TestDecidePriorityExplicitStop(t *testing.T) {
	got := orchestrator.DecidePriority(orchestrator.PriorityInfo{ExplicitStop: true}, nil)
	if got != orchestrator.PriorityInterrupt {
		t.Fatalf("explicit stop must interrupt, got %q", got)
	}
}

// The in-flight tool touching the impact area interrupts; anything
// else stays normal.
func TestDecidePriorityImpactArea(t *testing.T) {
	area := []string{"src/export"}
	touching := orchestrator.PriorityInfo{ResourceKeys: []string{"src/export/pdf.rs"}, ImpactArea: area}
	if got := orchestrator.DecidePriority(touching, nil); got != orchestrator.PriorityInterrupt {
		t.Fatalf("tool inside the impact area must interrupt, got %q", got)
	}
	away := orchestrator.PriorityInfo{ResourceKeys: []string{"src/mobile/ui.kt"}, ImpactArea: area}
	if got := orchestrator.DecidePriority(away, nil); got != orchestrator.PriorityNormal {
		t.Fatalf("tool outside the impact area stays normal, got %q", got)
	}
}

// A forbidden current action (decider, high confidence) interrupts.
func TestDecidePriorityProhibition(t *testing.T) {
	judge := func(action, _ string) (bool, float64) { return action == "replan", 0.9 }
	info := orchestrator.PriorityInfo{Action: "replan", Instruction: "freeze the API"}
	if got := orchestrator.DecidePriority(info, judge); got != orchestrator.PriorityInterrupt {
		t.Fatalf("forbidden action must interrupt, got %q", got)
	}
	info.Action = "adjust"
	if got := orchestrator.DecidePriority(info, judge); got != orchestrator.PriorityNormal {
		t.Fatalf("allowed action stays normal, got %q", got)
	}
	weak := func(action, _ string) (bool, float64) { return true, 0.4 }
	if got := orchestrator.DecidePriority(info, weak); got != orchestrator.PriorityNormal {
		t.Fatal("low-confidence prohibition stays normal")
	}
}

// A pending affected external action interrupts.
func TestDecidePriorityPendingExternal(t *testing.T) {
	info := orchestrator.PriorityInfo{HasPendingExternal: true}
	if got := orchestrator.DecidePriority(info, nil); got != orchestrator.PriorityInterrupt {
		t.Fatalf("pending external action must interrupt, got %q", got)
	}
}

// MatchResourceKeys covers exact and prefix overlap both ways.
func TestMatchResourceKeys(t *testing.T) {
	keys := []string{"src/export/pdf.rs", "tmp/x"}
	area := []string{"src/export", "other"}
	hit := orchestrator.MatchResourceKeys(keys, area)
	if len(hit) != 1 || hit[0] != "src/export/pdf.rs" {
		t.Fatalf("bad intersection: %v", hit)
	}
	if len(orchestrator.MatchResourceKeys(keys, []string{"elsewhere"})) != 0 {
		t.Fatal("disjoint sets must not match")
	}
}

// End to end: the implementer editing inside the impact area gets an
// interrupt directive; the suite runner under a graceful process gets
// a normal one; finished runs are reported, not disturbed; the master
// confirmation summarizes all three.
func TestPropagateCorrection(t *testing.T) {
	f := newFixture(t)
	_, impl := f.spawnOk(f.parent.ID, childCard("impl", agents.ProfileImplementer))
	ir := f.attach(impl)
	ir.AutoAck = false
	ir.SetTool("edit_file", "src/export/pdf.rs", types.CancelImmediate,
		[]string{"src/export/pdf.rs"}, 0, f.now)
	_, lena := f.spawnOk(f.parent.ID, childCard("lena", agents.ProfileVerifier))
	lr := f.attach(lena)
	lr.AutoAck = false
	// The suite runner touches a terminal session, not the source
	// tree: no intersection with the impact area, so normal — the
	// suite finishes and its evidence goes stale (26.11).
	lr.SetTool("exec", "cargo test", types.CancelGraceful, []string{"session:test-1"}, 5*time.Minute, f.now)
	// A finished sibling: reported, not disturbed.
	_, done := f.spawnOk(f.parent.ID, childCard("done", agents.ProfileExplorer))
	dr := f.attach(done)
	_ = dr
	if err := f.o.Finish(done.Run.ID, types.RunCompleted, "verified"); err != nil {
		t.Fatal(err)
	}

	invalidated := 0
	f.o.SetHooks(orchestrator.Hooks{
		InvalidateEvidence: func(tasks []types.TaskID, rev int) { invalidated = rev },
	})
	taskID := types.TaskID("tsk_000000000000000000000002")
	implRec, _ := f.store.GetRun(impl.Run.ID)
	implRec.Run.TaskID = taskID
	_ = f.store.UpdateRun(implRec)
	lenaRec, _ := f.store.GetRun(lena.Run.ID)
	lenaRec.Run.TaskID = taskID
	_ = f.store.UpdateRun(lenaRec)
	doneRec, _ := f.store.GetRun(done.Run.ID)
	doneRec.Run.TaskID = taskID
	_ = f.store.UpdateRun(doneRec)

	// Scope-wide instruction: every run in scope p1 is affected.
	scope := types.Scope{Kind: types.ScopeProject, ID: "p1"}
	results, err := f.o.PropagateCorrection(orchestrator.Correction{
		TaskIDs: []types.TaskID{taskID},
		Text:    "export CSV, not PDF", Summary: "R4 now CSV",
		Action: orchestrator.ActionReplan, ImpactArea: []string{"src/export"},
	}, scope, "test")
	if err != nil {
		t.Fatal(err)
	}
	byRun := map[types.RunID]orchestrator.CorrectionResult{}
	for _, r := range results {
		byRun[r.Run] = r
	}
	if byRun[impl.Run.ID].Priority != orchestrator.PriorityInterrupt {
		t.Fatalf("implementer inside the impact area must interrupt: %+v", byRun[impl.Run.ID])
	}
	if byRun[lena.Run.ID].Priority != orchestrator.PriorityNormal {
		t.Fatalf("suite runner goes normal: %+v", byRun[lena.Run.ID])
	}
	if byRun[done.Run.ID].Note != "already finished" {
		t.Fatalf("finished run must be reported, not disturbed: %+v", byRun[done.Run.ID])
	}
	if invalidated != 1 {
		t.Fatalf("evidence must be invalidated at revision 1, got %d", invalidated)
	}
	latest, ok := f.store.LatestInstruction(scope)
	if !ok || latest.Revision != 1 || latest.Text != "export CSV, not PDF" {
		t.Fatalf("instruction revision not persisted first: %+v", latest)
	}
	// The interrupt directive reached the implementer and woke nothing
	// it should not: the suite runner stays running.
	if full, _ := ir.Interrupts(); full != 1 {
		t.Fatal("implementer must be interrupted")
	}
	if got := runState(t, f.store, lena.Run.ID); got != types.RunRunning {
		t.Fatalf("normal directive must not stop the suite, got %q", got)
	}
	if got := f.o.PendingDirectives(lena.Run.AgentID); len(got) != 1 {
		t.Fatalf("normal directive must be pending for the suite runner, got %d", len(got))
	}
}
