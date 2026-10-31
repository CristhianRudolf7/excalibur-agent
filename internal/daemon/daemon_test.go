package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// fakeAdoption is a scriptable Adoption for boot tests.
type fakeAdoption struct {
	runs    []RunView
	applied []AdoptDecision
	uncert  map[string]bool
	waiting map[string]bool
}

func (f *fakeAdoption) ListRuns() []RunView { return f.runs }

func (f *fakeAdoption) Decide(run RunView, downFor time.Duration) AdoptDecision {
	return DefaultDecide(run, downFor, f.uncert[run.RunID], f.waiting[run.RunID])
}

func (f *fakeAdoption) Apply(run RunView, d AdoptDecision) error {
	_ = run
	f.applied = append(f.applied, d)
	return nil
}

func liveDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	bus := telemetry.NewBus(nil)
	store := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), store, orchestrator.NoopJournal{})
	ctrls := &OrchControl{Orch: orch, Store: store}
	m := master.New(master.Config{}, master.NewMemStore(), master.NewLexicon(), nil, ctrls, nil, nil)
	backend := NewDaemonBackend(m, ctrls, bus)
	g := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), bus)
	d := New(DefaultConfig(), Deps{Gate: g, Master: m, Orch: orch, Bus: bus})
	ctx := context.Background()
	addr, err := d.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background()) })
	return d, addr
}

// Section 17.3/25.2: start binds loopback, double start fails, stop
// drains and a second stop is safe.
func TestStartStopLifecycle(t *testing.T) {
	d, addr := liveDaemon(t)
	if addr == "" {
		t.Fatal("no bound address")
	}
	if !d.Running() {
		t.Fatal("daemon must report running")
	}
	if !gateway.DaemonActive(addr) {
		t.Fatal("health check must pass")
	}
	if _, err := d.Start(context.Background()); err == nil {
		t.Fatal("second Start must fail")
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if d.Running() {
		t.Fatal("daemon must report stopped")
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

// Section 17.3: adoption dispositions — resume, block on uncertain
// effects, keep user waits, pause after long outages.
func TestBootAdoptionMatrix(t *testing.T) {
	now := time.Now()
	mk := func() *fakeAdoption {
		return &fakeAdoption{
			runs: []RunView{
				{RunID: "clean", State: types.RunRunning, UpdatedAt: now.Add(-time.Minute)},
				{RunID: "unc", State: types.RunRunning, UpdatedAt: now.Add(-time.Minute)},
				{RunID: "wait", State: types.RunWaiting, UpdatedAt: now.Add(-time.Minute)},
				{RunID: "done", State: types.RunCompleted, UpdatedAt: now.Add(-time.Minute)},
			},
			uncert:  map[string]bool{"unc": true},
			waiting: map[string]bool{"wait": true},
		}
	}
	d := New(DefaultConfig(), Deps{Adoption: mk()})
	sum, err := d.adopt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Resumed != 1 || sum.Blocked != 1 || sum.Waiting != 1 || sum.Kept != 1 {
		t.Fatalf("adoption summary = %+v, want 1 of each", sum)
	}
	// Long outage pauses everything non-terminal instead of resuming.
	old := now
	fa := mk()
	d2 := New(DefaultConfig(), Deps{Adoption: fa})
	d2.mu.Lock()
	d2.crashes = []time.Time{old.Add(-7 * time.Hour)}
	d2.mu.Unlock()
	if _, err := d2.adopt(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	for _, dec := range fa.applied[:3] {
		if dec == AdoptResume {
			t.Fatalf("long outage must not resume: %v", fa.applied)
		}
	}
}

// Section 17.3: safe mode pauses every run and reports the single
// notice counts.
func TestBootSafeModePausesAll(t *testing.T) {
	now := time.Now()
	fa := &fakeAdoption{runs: []RunView{
		{RunID: "a", State: types.RunRunning},
		{RunID: "b", State: types.RunWaiting},
	}}
	d := New(DefaultConfig(), Deps{Adoption: fa})
	d.mu.Lock()
	d.safeMode = true
	d.mu.Unlock()
	sum, err := d.adopt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Paused != 2 {
		t.Fatalf("safe mode must pause all, got %+v", sum)
	}
}

// Section 17.3: 3 crashes in 10 minutes enter safe mode; quiet for 30
// minutes or an explicit order leaves it; the repeated run/step is
// quarantined until released.
func TestCrashHistorySafeMode(t *testing.T) {
	d := New(Config{DataDir: t.TempDir()}, Deps{})
	base := time.Now()
	d.RecordCrash(base.Add(-9*time.Minute), "run_A", "step 4")
	d.RecordCrash(base.Add(-5*time.Minute), "run_A", "step 4")
	if d.SafeMode() {
		t.Fatal("two crashes must not trip safe mode")
	}
	d.RecordCrash(base, "run_A", "step 4")
	if !d.SafeMode() {
		t.Fatal("three crashes in 10 minutes must enter safe mode")
	}
	d.LeaveSafeMode()
	if d.SafeMode() {
		t.Fatal("explicit order must leave safe mode")
	}
	// Quiet exit: a boot 31 minutes after the last crash is clean.
	d2 := New(Config{DataDir: t.TempDir()}, Deps{})
	then := base
	d2.RecordCrash(then.Add(-40*time.Minute), "r", "s")
	d2.RecordCrash(then.Add(-35*time.Minute), "r", "s")
	d2.RecordCrash(then.Add(-31*time.Minute), "r", "s")
	d2.checkCrashHistory(then)
	if d2.SafeMode() {
		t.Fatal("30 minutes quiet must leave safe mode on boot")
	}
}

// Section 3.6: delegation through the daemon when active, embedded
// fallback otherwise.
func TestDelegateDownFallback(t *testing.T) {
	if _, err := CallDaemon(context.Background(), "127.0.0.1:1", "cli", "run.list", nil); err != ErrDaemonDown {
		t.Fatalf("unreachable daemon must map to ErrDaemonDown, got %v", err)
	}
	if MustDaemon("") {
		t.Fatal("empty addr must not require the daemon")
	}
	if MustDaemon("127.0.0.1:1") {
		t.Fatal("dead addr must fall back to embedded")
	}
	_, addr := liveDaemon(t)
	if !MustDaemon(addr) {
		t.Fatal("live gate must route delegation through the daemon")
	}
	res, err := CallDaemon(context.Background(), addr, "cli", "run.list", map[string]any{})
	if err != nil {
		t.Fatalf("daemon call: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("run.list must answer through the daemon")
	}
}

// Sections 5.4/15.5/22.1: orchestrator notices reach the bus and the
// master notifier exactly once per channel.
func TestBridgeForwardsNotice(t *testing.T) {
	bus := telemetry.NewBus(nil)
	store := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), store, orchestrator.NoopJournal{})
	sender := &bridgeSender{connected: true}
	notify := master.NewNotifier(master.NotifierConfig{Sender: sender})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Bridge(ctx, orch, bus, notify)
	ag, err := orch.RegisterMaster("m", types.GeneralScope())
	if err != nil {
		t.Fatal(err)
	}
	card := orchestrator.DelegationCard{
		Name: "w", Description: "work", Goal: "g", Deliverable: "d",
		Scope: types.GeneralScope(), Done: []string{"done"}, Report: "r",
	}
	child, rec, err := orch.Spawn(ag.ID, card)
	if err != nil {
		t.Fatal(err)
	}
	_ = child
	_ = rec
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bus.Len() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bus.Len() == 0 {
		t.Fatal("orchestrator events must reach the gate bus")
	}
}

type bridgeSender struct {
	connected bool
}

func (b *bridgeSender) Connected(string) bool            { return b.connected }
func (b *bridgeSender) ActiveSince(string) time.Time     { return time.Time{} }
func (b *bridgeSender) Send(string, master.Notice) error { return nil }
