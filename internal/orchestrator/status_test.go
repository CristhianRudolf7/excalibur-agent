package orchestrator_test

import (
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 15.10 row 5: the snapshot shows the tool in flight with its
// live tail, the computed last progress, and the queue position; the
// report reminder fires after 15 steps or 10 minutes of silence.

// Full tree with a tool in flight, live output, plan progress and
// budget: every row of the Section 15.6 table.
func TestStatusSnapshot(t *testing.T) {
	f := newFixture(t)
	child, crec := f.spawnOk(f.parent.ID, childCard("lena", agents.ProfileImplementer))
	_ = child
	cr := f.attach(crec)
	cr.SetWaitingModel(true)
	cr.SetTool("exec", "cargo test --all", types.CancelGraceful, []string{"session:t1"}, 0, f.now.Add(-3*time.Minute))
	cr.SetLiveTail("test export_pdf ... ok\ntest import_csv ... ok")
	for i := 0; i < 10; i++ {
		cr.Step(f.o)
	}
	cr.SetProgress("7 of 9 plan steps", 7, 9, "export")

	st, err := f.o.Status(crec.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Tool != "exec" {
		t.Fatalf("tool in flight missing: %+v", st)
	}
	if !strings.Contains(st.LiveTail, "import_csv") {
		t.Fatalf("live tail missing: %q", st.LiveTail)
	}
	if st.ToolElapsed < 3*time.Minute {
		t.Fatalf("tool elapsed not measured: %v", st.ToolElapsed)
	}
	if !st.WaitingModel {
		t.Fatal("model wait not shown")
	}
	if st.LastProgress != "7 of 9 plan steps" {
		t.Fatalf("last progress not computed: %q", st.LastProgress)
	}
	if st.PlanDone != 7 || st.PlanTotal != 9 {
		t.Fatalf("plan progress missing: %d/%d", st.PlanDone, st.PlanTotal)
	}
	if st.BudgetUsedPct <= 0 {
		t.Fatal("budget must show consumption")
	}
	plain := st.Plain()
	for _, want := range []string{"lena", "exec", "import_csv", "7 of 9"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain template misses %q: %q", want, plain)
		}
	}
	contract := st.ToAPI()
	if contract.RunID != string(crec.Run.ID) || contract.State != types.RunRunning {
		t.Fatalf("bad api mapping: %+v", contract)
	}
	if contract.ToolInFlight != "exec" || contract.PlanDone != 7 || contract.PlanTotal != 9 {
		t.Fatalf("api snapshot incomplete: %+v", contract)
	}
	var _ api.RunStatusResult = contract
}

// The parent snapshot nests the subtree; queued runs show position.
func TestStatusTreeAndQueue(t *testing.T) {
	cfg := orchestrator.DefaultConfig()
	cfg.MaxRunningPerUser = 1
	f := newFixtureWith(t, cfg)
	if got := runState(t, f.store, f.prun.Run.ID); got != types.RunRunning {
		t.Fatalf("parent must hold the single slot, got %q", got)
	}
	_, queued := f.spawnOk(f.parent.ID, childCard("q", agents.ProfileImplementer))
	st, err := f.o.StatusAgent(f.parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Children) != 1 {
		t.Fatalf("tree must nest the child, got %d", len(st.Children))
	}
	kid := st.Children[0]
	if kid.State != types.RunQueued || kid.QueuePosition != 0 {
		t.Fatalf("queued child must show position: %+v", kid)
	}
	if !strings.Contains(kid.Plain(), "queue position 0") {
		t.Fatalf("plain must show the queue position: %q", kid.Plain())
	}
	_ = queued
}

// Fifteen steps (or ten minutes) without a report or plan change
// raise the compiler reminder flag.
func TestStatusReportReminder(t *testing.T) {
	f := newFixture(t)
	_, crec := f.spawnOk(f.parent.ID, childCard("silent", agents.ProfileImplementer))
	cr := f.attach(crec)
	cr.HoldNarrative()
	for i := 0; i < 15; i++ {
		cr.Step(f.o)
	}
	st, err := f.o.Status(crec.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Reminder {
		t.Fatal("reminder must fire after 15 silent steps")
	}
}
