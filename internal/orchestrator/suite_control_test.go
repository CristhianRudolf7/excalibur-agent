package orchestrator_test

import (
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Suite control of Section 23.5 in embedded mode: the time from a
// control order to quiescence (no step, no model call, no tool effect)
// per cancel class of the in-flight tool, plus the correctness of
// target resolution. Logical time is steps; every case must settle in
// a bounded number of them.

// quiesce drives a runner until it stops consuming or the budget of
// steps runs out, and reports the steps taken.
func quiesce(t *testing.T, o *orchestrator.Orchestrator, r *fake.Runner, budget int) int {
	t.Helper()
	before := r.Snapshot().StepsDone
	for i := 0; i < budget; i++ {
		r.Step(o)
	}
	return r.Snapshot().StepsDone - before
}

func TestSuiteControl(t *testing.T) {
	f := newFixture(t)
	spawn := func(name string, cancel types.CancelMode, keys []string) (orchestrator.RunRecord, *fake.Runner) {
		_, rec := f.spawnOk(f.parent.ID, childCard(name, agents.ProfileImplementer))
		r := f.attach(rec)
		r.AutoAck = false
		r.SetTool("tool-"+name, "args", cancel, keys, 5*time.Minute, f.now)
		return rec, r
	}

	// Immediate tools (reads, searches, HTTP) abort at once: one step
	// to quiescence, tool cleared.
	rec, r := spawn("read", types.CancelImmediate, []string{"docs/a.md"})
	if err := f.o.Pause(rec.Run.ID, types.PauseUser, "suite"); err != nil {
		t.Fatal(err)
	}
	if full, _ := r.Interrupts(); full != 1 {
		t.Fatal("pause must interrupt the in-flight tool")
	}
	taken := quiesce(t, f.o, r, 3)
	if taken != 0 {
		t.Fatalf("immediate tool: run kept consuming (%d steps)", taken)
	}
	if r.Snapshot().HasTool {
		t.Fatal("immediate tool must clear at once")
	}
	if got := runState(t, f.store, rec.Run.ID); got != types.RunPaused {
		t.Fatalf("run must park, got %q", got)
	}
	t.Logf("control immediate:     quiescent in %d steps", taken)

	// Graceful tools (processes) get a signal and end at the next
	// step; the run is already parked and consumes nothing.
	rec, r = spawn("proc", types.CancelGraceful, []string{"session:build"})
	if err := f.o.Pause(rec.Run.ID, types.PauseUser, "suite"); err != nil {
		t.Fatal(err)
	}
	taken = quiesce(t, f.o, r, 3)
	if taken != 0 || r.Snapshot().HasTool {
		t.Fatalf("graceful tool must end without consuming: steps=%d tool=%v", taken, r.Snapshot().HasTool)
	}
	t.Logf("control graceful:      quiescent in %d steps", taken)

	// After-completion tools (atomic writes, git, transactions)
	// finish first with their timeout: the suite bounds the finish,
	// then the run is fully quiescent with no new effects.
	rec, r = spawn("atomic", types.CancelAfterCompletion, []string{"src/a.go"})
	r.FinishAfter = 2
	if err := f.o.Pause(rec.Run.ID, types.PauseUser, "suite"); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, rec.Run.ID); got != types.RunPaused {
		t.Fatalf("run parks while the atomic tool finishes, got %q", got)
	}
	// The paused run consumes no steps even while its tool finishes.
	taken = quiesce(t, f.o, r, 5)
	if taken != 0 {
		t.Fatalf("paused run consumed %d steps during atomic finish", taken)
	}
	if r.Snapshot().HasTool {
		t.Fatal("atomic tool must finish within its bound")
	}
	t.Logf("control after_complete: tool finished, run consumed %d steps", taken)

	// Cooperative tools (external capabilities): the cancel is asked,
	// the harness re-queries the final state, and the run stays
	// parked without consuming.
	rec, r = spawn("browser", types.CancelCooperative, []string{"browser:s"})
	if err := f.o.Interrupt(rec.Run.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, toolOnly := r.Interrupts(); toolOnly != 1 {
		t.Fatal("tool-only interrupt must reach the cooperative tool")
	}
	taken = quiesce(t, f.o, r, 5)
	if taken != 5 {
		t.Fatalf("tool-only interrupt keeps the run working, took %d steps", taken)
	}
	if !r.Snapshot().HasTool {
		t.Fatal("cooperative tool stays pending until the capability answers")
	}
	if got := runState(t, f.store, rec.Run.ID); got != types.RunRunning {
		t.Fatalf("tool-only interrupt keeps the run going, got %q", got)
	}
	t.Logf("control cooperative:   pending, run consumed %d steps", taken)

	// Cancel with a tool in flight: bottom-up, one report per run,
	// definitive afterwards.
	rec, r = spawn("doomed", types.CancelGraceful, []string{"session:d"})
	_ = r
	if err := f.o.Cancel(rec.Run.ID); err != nil {
		t.Fatal(err)
	}
	if got := runState(t, f.store, rec.Run.ID); got != types.RunCanceled {
		t.Fatalf("cancel must terminate, got %q", got)
	}
	if _, ok := f.store.LatestReport(rec.Run.ID); !ok {
		t.Fatal("canceled run must deliver its report")
	}
	taken = quiesce(t, f.o, r, 3)
	if taken != 0 {
		t.Fatalf("canceled run kept consuming (%d steps)", taken)
	}
	t.Logf("control cancel:        terminal in 0 steps, report delivered")

	// Target resolution by name: one Lena resolves, two Lenas ask.
	_, _ = f.spawnOk(f.parent.ID, childCard("lena", agents.ProfileImplementer))
	_, _ = f.spawnOk(f.parent.ID, childCard("lena", agents.ProfileExplorer))
	if got := f.store.FindAgentsByName("lena"); len(got) != 2 {
		t.Fatalf("ambiguous name must return every candidate, got %d", len(got))
	}
	if got := f.store.FindAgentsByName("nobody"); len(got) != 0 {
		t.Fatal("unknown name must resolve to nothing")
	}
}
