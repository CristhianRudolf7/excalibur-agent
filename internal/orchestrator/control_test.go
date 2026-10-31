package orchestrator_test

import (
	"sync"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 15.8: pause, resume, interrupt, cancel and redirect over
// every reachable state; cascades; definitive cancel; arrival-order
// serialization of concurrent orders.

// Pausing a parent cascades to the subtree with reason parent; resume
// only wakes what that same order paused.
func TestPauseCascadeAndResumeByOrder(t *testing.T) {
	f := newFixture(t)
	c1, r1 := f.spawnOk(f.parent.ID, childCard("a", agents.ProfileImplementer))
	cr1 := f.attach(r1)
	_, r2 := f.spawnOk(f.parent.ID, childCard("b", agents.ProfileImplementer))
	cr2 := f.attach(r2)
	_ = cr1
	_ = cr2
	leases := 0
	f.o.SetHooks(orchestrator.Hooks{ReleaseLeases: func(types.RunID) { leases++ }})
	if err := f.o.Pause(f.prun.Run.ID, types.PauseUser, "order-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []types.RunID{f.prun.Run.ID, r1.Run.ID, r2.Run.ID} {
		if got := runState(t, f.store, id); got != types.RunPaused {
			t.Fatalf("run %q must be paused, got %q", id, got)
		}
	}
	if leases != 3 {
		t.Fatalf("leases must release on pause, got %d", leases)
	}
	rec, _ := f.store.GetRun(r1.Run.ID)
	if rec.Run.PausedBy != "order-1" || rec.Run.PauseReason == nil || *rec.Run.PauseReason != types.PauseParent {
		t.Fatalf("cascade must mark parent reason and order: %+v", rec.Run)
	}
	if full, _ := cr1.Interrupts(); full != 1 {
		t.Fatal("pause must interrupt the in-flight run")
	}
	// A sibling order pauses one child again under a new order.
	if err := f.o.Pause(r1.Run.ID, types.PauseUser, "order-2"); err != nil {
		t.Fatal(err)
	}
	// Resume of order-1 wakes the parent and b, but not a.
	if err := f.o.Resume(f.prun.Run.ID, "order-1"); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, r2.Run.ID); got != types.RunRunning {
		t.Fatalf("child b must resume, got %q", got)
	}
	if got := runState(t, f.store, r1.Run.ID); got != types.RunPaused {
		t.Fatalf("child a stays paused under order-2, got %q", got)
	}
	_ = c1
	// Resuming with the wrong order is rejected.
	if err := f.o.Resume(r1.Run.ID, "order-1"); err == nil {
		t.Fatal("resume with another order must fail")
	}
	if err := f.o.Resume(r1.Run.ID, "order-2"); err != nil {
		t.Fatal(err)
	}
}

// Interrupt stops the run but not its descendants; tool-only
// interrupt only stops the tool.
func TestInterruptVariants(t *testing.T) {
	f := newFixture(t)
	_, r1 := f.spawnOk(f.parent.ID, childCard("a", agents.ProfileImplementer))
	cr1 := f.attach(r1)
	cr1.SetTool("exec", "make test", types.CancelGraceful, nil, 0, f.now)
	if err := f.o.Interrupt(r1.Run.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, toolOnly := cr1.Interrupts(); toolOnly != 1 {
		t.Fatal("tool-only interrupt must only stop the tool")
	}
	if got := runState(t, f.store, r1.Run.ID); got != types.RunRunning {
		t.Fatalf("tool-only interrupt keeps the run going, got %q", got)
	}
	if err := f.o.Interrupt(r1.Run.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, r1.Run.ID); got != types.RunPaused {
		t.Fatalf("interrupt parks the run, got %q", got)
	}
	// Descendants of nobody: the parent keeps working.
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunRunning {
		t.Fatalf("interrupt touches no descendants, parent got %q", got)
	}
}

// Cancel walks bottom-up with a report per run and is definitive:
// later orders are rejected.
func TestCancelCascadeDefinitive(t *testing.T) {
	f := newFixture(t)
	_, r1 := f.spawnOk(f.parent.ID, childCard("a", agents.ProfileImplementer))
	_ = f.attach(r1)
	if err := f.o.Cancel(f.prun.Run.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []types.RunID{f.prun.Run.ID, r1.Run.ID} {
		if got := runState(t, f.store, id); got != types.RunCanceled {
			t.Fatalf("run %q must be canceled, got %q", id, got)
		}
		if _, ok := f.store.LatestReport(id); !ok {
			t.Fatalf("canceled run %q delivered no report", id)
		}
	}
	if err := f.o.Cancel(f.prun.Run.ID); err == nil {
		t.Fatal("cancel after cancel must be rejected")
	}
	if err := f.o.Pause(f.prun.Run.ID, types.PauseUser, ""); err == nil {
		t.Fatal("orders on a canceled run must be rejected")
	}
}

// Redirect on a paused run resumes it with the directive; a normal
// directive to a paused run queues until resume.
func TestRedirectResumesPaused(t *testing.T) {
	f := newFixture(t)
	_, r1 := f.spawnOk(f.parent.ID, childCard("a", agents.ProfileImplementer))
	cr1 := f.attach(r1)
	cr1.AutoAck = false
	if err := f.o.Pause(r1.Run.ID, types.PauseUser, "o1"); err != nil {
		t.Fatal(err)
	}
	if err := f.o.Redirect(r1.Run.ID, "use CSV", orchestrator.ActionReplan, true); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, r1.Run.ID); got != types.RunRunning {
		t.Fatalf("redirect must resume a paused run, got %q", got)
	}
	if got := f.o.PendingDirectives(r1.Run.AgentID); len(got) != 1 {
		t.Fatalf("interrupt directive must be pending, got %d", len(got))
	}
}

// Only the Section 7.2 edges exist; everything else is rejected.
func TestIllegalTransitions(t *testing.T) {
	f := newFixture(t)
	if err := f.o.Transition(f.prun.Run.ID, types.RunCompleted); err == nil {
		t.Fatal("running -> completed must be rejected (gate first)")
	}
	if err := f.o.Finish(f.prun.Run.ID, types.RunCompleted, "gate passed"); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunCompleted {
		t.Fatalf("finish must walk running -> verifying -> completed, got %q", got)
	}
	typesFound := map[string]bool{}
	for _, e := range f.jrnl.Entries() {
		if e.Type == orchestrator.JournalStateChange {
			typesFound[e.Payload] = true
		}
	}
	if !typesFound["running->verifying"] || !typesFound["verifying->completed"] {
		t.Fatalf("gate path missing from journal: %v", typesFound)
	}
}

// Block, unblock and recovering walk their diagram edges.
func TestBlockAndRecovering(t *testing.T) {
	f := newFixture(t)
	if err := f.o.Block(f.prun.Run.ID, types.BlockLoop, "signature R1"); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunBlocked {
		t.Fatalf("want blocked, got %q", got)
	}
	if _, ok := f.store.LatestReport(f.prun.Run.ID); !ok {
		t.Fatal("blocked run must deliver a report")
	}
	if err := f.o.Unblock(f.prun.Run.ID); err != nil {
		t.Fatal(err)
	}
	// Unblock returns to the queue; admission re-runs it.
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunRunning {
		t.Fatalf("unblocked run must be readmitted, got %q", got)
	}
	if err := f.o.SetRecovering(f.prun.Run.ID); err != nil {
		t.Fatalf("running runs enter recovering: %v", err)
	}
	if err := f.o.Transition(f.prun.Run.ID, types.RunPaused); err != nil {
		t.Fatal(err)
	}
	if err := f.o.SetRecovering(f.prun.Run.ID); err == nil {
		t.Fatal("paused runs must not enter recovering")
	}
	// The waiting edge on a fresh child.
	_, rx := f.spawnOk(f.parent.ID, childCard("w", agents.ProfileImplementer))
	_ = f.attach(rx)
	if err := f.o.SetWaiting(rx.Run.ID, types.WaitTool); err != nil {
		t.Fatal(err)
	}
	if err := f.o.SetRecovering(rx.Run.ID); err != nil {
		t.Fatalf("waiting runs enter recovering: %v", err)
	}
}

// Concurrent orders on one run serialize in arrival order; cancel
// wins definitively once it lands.
func TestConcurrentOrdersSerialize(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errs[i] = f.o.Pause(f.prun.Run.ID, types.PauseUser, "")
			} else {
				errs[i] = f.o.Cancel(f.prun.Run.ID)
			}
		}(i)
	}
	wg.Wait()
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunCanceled {
		t.Fatalf("cancel must win once it lands, got %q", got)
	}
	cancels := 0
	for _, err := range errs {
		if err == nil {
			cancels++
		}
	}
	if cancels == 0 {
		t.Fatal("at least one order must succeed")
	}
}
