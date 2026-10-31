package orchestrator_test

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 15.10 rows 1-3: hierarchy depth, tool clamping, delegation
// context rules, in embedded mode.

// A level-2 child must not create subagents: no tool in its profile
// and an orchestrator rejection on attempt.
func TestLevel2CannotSpawn(t *testing.T) {
	f := newFixture(t)
	child, rec := f.spawnOk(f.parent.ID, childCard("lena", agents.ProfileImplementer))
	_ = f.attach(rec)
	profile, _ := agents.Lookup(agents.ProfileImplementer)
	if agents.MaySpawn(profile, 2) {
		t.Fatal("level-2 implementer must not spawn")
	}
	_, _, err := f.o.Spawn(child.ID, childCard("nobody", agents.ProfileExplorer))
	if err == nil {
		t.Fatal("orchestrator accepted a spawn from a level-2 child")
	}
	found := false
	for _, typ := range f.jrnl.Types() {
		if typ == "spawn_rejected" {
			found = true
		}
	}
	if !found {
		t.Fatal("rejected spawn left no journal trace")
	}
}

// The card never widens the profile; the master with no work tools
// still creates a parent that has them (Sections 15.4, 15.9).
func TestCardClampAndMasterDelegation(t *testing.T) {
	f := newFixture(t)
	if runState(t, f.store, f.prun.Run.ID) != types.RunRunning {
		t.Fatal("fresh parent must be admitted to running")
	}
	// Explorer card asking for exec: rejected.
	_, _, err := f.o.Spawn(f.parent.ID, orchestrator.DelegationCard{
		Name: "X", Description: "explores", Profile: agents.ProfileExplorer,
		Goal: "g", Deliverable: "d", Scope: types.Scope{Kind: types.ScopeProject, ID: "p1"},
		Tools: []string{"exec", "read_file"}, Done: []string{"d"}, Report: "r",
	})
	if err == nil {
		t.Fatal("card widening the explorer profile accepted")
	}
	// A reduced toolset is accepted.
	child, _, err := f.o.Spawn(f.parent.ID, orchestrator.DelegationCard{
		Name: "Y", Description: "reads only", Profile: agents.ProfileExplorer,
		Goal: "g", Deliverable: "d", Scope: types.Scope{Kind: types.ScopeProject, ID: "p1"},
		Tools: []string{"read_file", "grep"}, Done: []string{"d"}, Report: "r",
	})
	if err != nil {
		t.Fatalf("reduced toolset rejected: %v", err)
	}
	profile, _ := agents.Lookup(agents.ProfileName(child.Profile))
	_ = profile
	if got := f.o.Tree(f.master.ID); len(got) != 3 {
		t.Fatalf("want master+parent+child in tree, got %d", len(got))
	}
}

// Child budget comes out of the parent remainder (Section 15.9).
func TestChildBudgetFromParentRemainder(t *testing.T) {
	f := newFixture(t)
	f.store.RecordUsage(f.prun.Run.ID, orchestrator.Usage{Steps: 100})
	_, rec, err := f.o.Spawn(f.parent.ID, childCard("impl", agents.ProfileImplementer))
	if err != nil {
		t.Fatal(err)
	}
	// Parent default 150 steps, 100 used: child capped at 50.
	if rec.Run.Budget.MaxSteps != 50 {
		t.Fatalf("want child capped at 50 steps, got %+v", rec.Run.Budget)
	}
	if _, _, err := f.o.Spawn(f.parent.ID, childCard("impl2", agents.ProfileImplementer)); err != nil {
		t.Fatalf("second child within remainder must spawn: %v", err)
	}
}

// Fan-out caps: 4 active children per parent, 8 running per user;
// waiting, paused and blocked runs do not consume the running cap
// (Section 15.3).
func TestFanOutCaps(t *testing.T) {
	f := newFixture(t)
	var first orchestrator.RunRecord
	for i := 0; i < 4; i++ {
		_, rec := f.spawnOk(f.parent.ID, childCard(string(rune('a'+i)), agents.ProfileImplementer))
		if i == 0 {
			first = rec
		}
		f.attach(rec)
	}
	if got := f.o.ActiveChildren(f.parent.ID); got != 4 {
		t.Fatalf("want 4 active children, got %d", got)
	}
	// The fifth child queues: active cap reached, running cap not yet.
	_, rec := f.spawnOk(f.parent.ID, childCard("fifth", agents.ProfileImplementer))
	if runState(t, f.store, rec.Run.ID) != types.RunQueued {
		t.Fatalf("fifth child must queue, got %q", runState(t, f.store, rec.Run.ID))
	}
	if rec.Run.QueuePosition < 0 {
		t.Fatal("queued run must carry a queue position")
	}
	// Pausing one child frees an active slot: the queued child is
	// admitted.
	if err := f.o.Pause(first.Run.ID, types.PauseUser, ""); err != nil {
		t.Fatal(err)
	}
	if runState(t, f.store, rec.Run.ID) != types.RunRunning {
		t.Fatalf("queued child must be admitted after pause, got %q", runState(t, f.store, rec.Run.ID))
	}
}

// Eight running executions block the ninth; a waiting parent does not
// hold a slot against its own children.
func TestRunningCapAndWaitingParent(t *testing.T) {
	f := newFixture(t)
	kid := func(p types.AgentID, name string) orchestrator.RunRecord {
		_, rec := f.spawnOk(p, childCard(name, agents.ProfileImplementer))
		f.attach(rec)
		return rec
	}
	// Parent P1 + 4 children = 5; second parent P2 + 2 children = 8.
	for _, n := range []string{"a", "b", "c", "d"} {
		kid(f.parent.ID, n)
	}
	p2, p2rec := f.spawnOk(f.master.ID, childCard("p2", agents.ProfileCoordinator))
	_ = p2rec
	p2r := f.attach(p2rec)
	_ = p2r
	kid(p2.ID, "e")
	kid(p2.ID, "f")
	if got := f.o.RunningCount(); got != 8 {
		t.Fatalf("want 8 running, got %d", got)
	}
	_, rec := f.spawnOk(p2.ID, childCard("ninth", agents.ProfileImplementer))
	if runState(t, f.store, rec.Run.ID) != types.RunQueued {
		t.Fatal("ninth execution must queue")
	}
	// P1 waiting on its children frees one slot: the ninth is
	// admitted without deadlock.
	if err := f.o.SetWaiting(f.prun.Run.ID, types.WaitSubagents); err != nil {
		t.Fatal(err)
	}
	if runState(t, f.store, rec.Run.ID) != types.RunRunning {
		t.Fatalf("no mutual block: queued child admitted, got %q", runState(t, f.store, rec.Run.ID))
	}
}
