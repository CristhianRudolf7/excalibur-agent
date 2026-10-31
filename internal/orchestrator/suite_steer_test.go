package orchestrator_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Suite steer of Section 23.5 in embedded mode: corrections injected
// at seeded pseudo-random points of a live tree; the suite checks
// adherence (the priority each case must take) and latency (steps
// from injection to application). Deterministic seed: reproducible
// without a model.

// steerCase is one injected correction with its expected priority.
type steerCase struct {
	name     string
	child    string
	keys     []string
	cancel   types.CancelMode
	want     orchestrator.DirectivePriority
	finishes bool // tool completes despite the order
}

func TestSuiteSteer(t *testing.T) {
	f := newFixture(t)
	rng := rand.New(rand.NewSource(42))
	area := []string{"src/export"}
	cases := []steerCase{
		{name: "explicit stop", child: "stop", keys: []string{"session:t"}, cancel: types.CancelGraceful, want: orchestrator.PriorityInterrupt},
		{name: "impact touch", child: "impl", keys: []string{"src/export/pdf.rs"}, cancel: types.CancelImmediate, want: orchestrator.PriorityInterrupt},
		{name: "graceful suite", child: "suite", keys: []string{"session:test"}, cancel: types.CancelGraceful, want: orchestrator.PriorityNormal, finishes: true},
		{name: "cooperative browser", child: "browser", keys: []string{"browser:sess"}, cancel: types.CancelCooperative, want: orchestrator.PriorityNormal, finishes: true},
		{name: "atomic write", child: "writer", keys: []string{"tmp/scratch"}, cancel: types.CancelAfterCompletion, want: orchestrator.PriorityNormal, finishes: true},
	}
	runners := map[string]*fake.Runner{}
	agentOf := map[string]types.AgentID{}
	taskOf := map[string]types.TaskID{}
	for _, c := range cases {
		_, rec := f.spawnOk(f.parent.ID, childCard(c.child, agents.ProfileImplementer))
		// One task per child: corrections fan out to that branch
		// only, so seeded cases stay independent.
		taskID := types.TaskID("tsk_steer_" + c.child)
		rec.Run.TaskID = taskID
		if err := f.store.UpdateRun(rec); err != nil {
			t.Fatal(err)
		}
		taskOf[c.child] = taskID
		r := f.attach(rec)
		r.AutoAck = false
		// Seeded pseudo-random injection point: 0-3 steps in.
		for i := 0; i < rng.Intn(4); i++ {
			r.Step(f.o)
		}
		r.SetTool("tool-"+c.child, "args", c.cancel, c.keys, 5*time.Minute, f.now)
		runners[c.child] = r
		agentOf[c.child] = rec.Run.AgentID
	}
	// Correction one per child, all sharing the impact area.
	type outcome struct {
		want, got orchestrator.DirectivePriority
		applied   string
	}
	results := map[string]outcome{}
	for _, c := range cases {
		explicit := c.name == "explicit stop"
		corr := orchestrator.Correction{
			TaskIDs:      []types.TaskID{taskOf[c.child]},
			Text:         "steer: " + c.name,
			Summary:      c.name,
			Action:       orchestrator.ActionReplan,
			ExplicitStop: explicit,
			ImpactArea:   area,
		}
		taskScope := types.Scope{Kind: types.ScopeTask, ID: string(taskOf[c.child])}
		got, err := f.o.PropagateCorrection(corr, taskScope, "steer")
		if err != nil {
			t.Fatal(err)
		}
		var pri orchestrator.DirectivePriority
		for _, r := range got {
			if r.Run == runners[c.child].RunID() {
				pri = r.Priority
			}
		}
		if pri == "" {
			t.Fatalf("steer %q: no directive reached the child", c.name)
		}
		r := runners[c.child]
		applied := ""
		switch {
		case c.want == orchestrator.PriorityInterrupt:
			// Immediate application: the run is interrupted and the
			// directive redelivered at once (latency 0 steps).
			if full, _ := r.Interrupts(); full != 1 {
				t.Errorf("steer %q: interrupt not requested", c.name)
			}
			if len(r.Delivered()) == 0 {
				t.Errorf("steer %q: directive not redelivered in context", c.name)
			}
			applied = "immediate: 0 steps"
		case c.cancel == types.CancelCooperative || c.cancel == types.CancelAfterCompletion:
			// The tool is not interruptible: the directive stays
			// pending and the master learns the maximum wait.
			r.Step(f.o)
			_, pend := f.o.CheckDirectiveAcks(f.now)
			mine := false
			for _, p := range pend {
				if p.Run == r.RunID() {
					mine = true
					if p.MaxWait <= 0 {
						t.Errorf("steer %q: pending without a maximum wait", c.name)
					}
				}
			}
			if !mine {
				t.Errorf("steer %q: %v tool must stay pending with its maximum wait", c.name, c.cancel)
			}
			applied = "pending with maximum wait"
		default:
			// Interruptible tool, normal priority: applied at the
			// next safe point (1 step), else escalated at 2.
			lat := -1
			for step := 0; step < 4; step++ {
				r.Step(f.o)
				if len(f.o.PendingDirectives(agentOf[c.child])) == 0 {
					lat = step + 1
					break
				}
				f.o.CheckDirectiveAcks(f.now)
			}
			// AutoAck is off in this suite: the deadline path
			// escalates after exactly 2 steps.
			esc, _ := f.o.CheckDirectiveAcks(f.now)
			_ = esc
			if lat > 2 {
				t.Errorf("steer %q: applied after %d steps, want <= 2", c.name, lat)
			}
			applied = "safe point or 2-step escalation"
		}
		results[c.name] = outcome{want: c.want, got: pri, applied: applied}
	}
	failed := false
	for _, c := range cases {
		o := results[c.name]
		if o.got != o.want {
			t.Errorf("steer %q: want priority %q, got %q", c.name, o.want, o.got)
			failed = true
		}
		t.Logf("steer %-18q priority=%-8q %s", c.name, o.got, o.applied)
	}
	if failed {
		t.Fatal("steer suite failed")
	}
	// No-ack path: a silent addressee escalates after exactly 2 steps.
	// (Under its own parent: the suite parents already hold the
	// fan-out cap.)
	p2, _ := f.spawnOk(f.master.ID, childCard("silent-parent", agents.ProfileCoordinator))
	silent, rec := f.spawnOk(p2.ID, childCard("silent", agents.ProfileImplementer))
	_ = silent
	sr := f.attach(rec)
	sr.AutoAck = false
	sr.SetTool("exec", "sleep 60", types.CancelGraceful, []string{"session:s"}, 0, f.now)
	if _, err := f.o.SendDirective(f.parent.ID, rec.Run.AgentID, orchestrator.ActionAdjust, "adjust", "", orchestrator.PriorityNormal); err != nil {
		t.Fatal(err)
	}
	sr.Step(f.o)
	if esc, _ := f.o.CheckDirectiveAcks(f.now); len(esc) != 0 {
		t.Fatal("no escalation before the 2-step deadline")
	}
	sr.Step(f.o)
	esc, _ := f.o.CheckDirectiveAcks(f.now)
	if len(esc) != 1 {
		t.Fatalf("silent addressee must escalate after 2 steps, got %v", esc)
	}
}
