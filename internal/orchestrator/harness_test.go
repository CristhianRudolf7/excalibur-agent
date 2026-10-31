package orchestrator_test

import (
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// fixture is the embedded-mode harness: one orchestrator over a
// memory store with a controllable clock, a master, a running parent
// and (on demand) children with attached fake runners. No daemon, no
// model, no SQL.
type fixture struct {
	t     *testing.T
	now   time.Time
	o     *orchestrator.Orchestrator
	store *orchestrator.MemoryStore
	jrnl  *orchestrator.RecordingJournal

	master types.Agent
	parent types.Agent
	prun   orchestrator.RunRecord
	pr     *fake.Runner
}

func newFixture(t *testing.T) *fixture {
	return newFixtureWith(t, orchestrator.DefaultConfig())
}

func newFixtureWith(t *testing.T, cfg orchestrator.Config) *fixture {
	t.Helper()
	f := &fixture{t: t, now: time.Now().UTC()}
	cfg.Clock = func() time.Time { return f.now }
	f.store = orchestrator.NewMemoryStore()
	f.jrnl = &orchestrator.RecordingJournal{}
	f.o = orchestrator.New(cfg, f.store, f.jrnl)
	master, err := f.o.RegisterMaster("Master", types.GeneralScope())
	if err != nil {
		t.Fatal(err)
	}
	f.master = master
	scope := types.Scope{Kind: types.ScopeProject, ID: "p1"}
	child, rec := f.spawnOk(master.ID, orchestrator.DelegationCard{
		Name: "Pia", Description: "coordinates task", Profile: agents.ProfileCoordinator,
		Goal: "do T", Deliverable: "done T", Scope: scope,
		Done: []string{"gate passes"}, Report: "summary",
		TaskID: types.TaskID("tsk_000000000000000000000001"),
	})
	f.parent = child
	f.prun = rec
	f.pr = f.attach(rec)
	return f
}

func (f *fixture) advance(d time.Duration) { f.now = f.now.Add(d) }

func (f *fixture) spawnOk(parentID types.AgentID, card orchestrator.DelegationCard) (types.Agent, orchestrator.RunRecord) {
	f.t.Helper()
	child, rec, err := f.o.Spawn(parentID, card)
	if err != nil {
		f.t.Fatalf("spawn: %v", err)
	}
	return child, rec
}

func (f *fixture) attach(rec orchestrator.RunRecord) *fake.Runner {
	f.t.Helper()
	agent, err := f.store.GetAgent(rec.Run.AgentID)
	if err != nil {
		f.t.Fatal(err)
	}
	r := fake.NewRunner(rec.Run.ID, agent.ID)
	r.AutoAck = true
	f.o.AttachRunner(r)
	return r
}

func childCard(name string, profile agents.ProfileName) orchestrator.DelegationCard {
	return orchestrator.DelegationCard{
		Name: name, Description: "worker " + name, Profile: profile,
		Goal: "goal " + name, Deliverable: "deliverable " + name,
		Scope: types.Scope{Kind: types.ScopeProject, ID: "p1"},
		Paths: []string{"src/" + name}, Done: []string{"done " + name},
		Report: "summary",
	}
}

func runState(t *testing.T, store *orchestrator.MemoryStore, id types.RunID) types.RunState {
	t.Helper()
	rec, err := store.GetRun(id)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Run.State
}
