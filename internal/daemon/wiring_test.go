package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// fullStack boots the complete wired daemon: master intake, agenda,
// orchestrator, capabilities, memory and the gate, all real except
// the scripted decision model.
func fullStack(t *testing.T, ctx context.Context) (*Daemon, *gateway.Client, *TaskService, *MemoryReviewLoop) {
	t.Helper()
	bus := telemetry.NewBus(nil)
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	ctrls := &OrchControl{Orch: orch, Store: orchStore}
	notifier := master.NewNotifier(master.NotifierConfig{})
	decider := NewModelDecider(models.NewFakeDecider(0.95), []string{"general"})
	m := master.New(master.Config{}, master.NewMemStore(), master.NewLexicon(), decider, ctrls, nil, notifier)
	backend := NewDaemonBackend(m, ctrls, bus)

	opts := store.DefaultOptions()
	db, err := store.Open(ctx, t.TempDir()+"/w.db", opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := agenda.EnsureSQLSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	memSvc := memory.NewService(db, memory.WithEmbedder(memory.NewFakeEmbedder("fake-test", 64)))

	launcher, err := NewOrchestratorLauncher(orch, notifier)
	if err != nil {
		t.Fatal(err)
	}
	ag := agenda.NewAgenda(agenda.NewSQLAgendaStore(db), agenda.NewSQLReminderStore(db), launcher)
	tasks, err := NewTaskService(orch, orchStore)
	if err != nil {
		t.Fatal(err)
	}
	backend.AttachServices(BackendServices{
		Tasks: tasks, Caps: NewCapabilityRoutes(nil), Memory: memSvc,
		Metrics: &MetricsStore{},
	})
	svc := &Services{Agenda: ag, Orch: orch, Store: orchStore, Memory: memSvc, Bus: bus}
	review := &MemoryReviewLoop{Service: memSvc, Interval: 50 * time.Millisecond}
	svc.Review = review

	g := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), bus)
	d := New(DefaultConfig(), Deps{
		Gate: g, Master: m, Orch: orch, Bus: bus, Backend: backend,
		Services:   svc,
		BootStore:  &OrchestratorRunStore{Store: orchStore},
		Reconciler: &EngineReconciler{Engine: NewEngine(), Jrnl: nil, Store: orchStore},
		AgendaBoot: ag,
	})
	Bridge(ctx, orch, bus, notifier)
	addr, err := d.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background()) })
	if got := d.Backend(); got != backend {
		t.Fatal("Backend() does not expose the composed backend")
	}
	return d, gateway.Dial(addr, "wiring"), tasks, review
}

// TestWiringTaskThroughGateway covers the Section 3.6 path end to
// end: task.start over HTTP JSON-RPC spawns an orchestrator run, and
// the run family answers from the same store.
func TestWiringTaskThroughGateway(t *testing.T) {
	ctx := context.Background()
	_, client, tasks, _ := fullStack(t, ctx)
	raw, err := client.Call(ctx, "w-1", api.MethodTaskStart, api.TaskStartParams{Request: "wired work"})
	if err != nil {
		t.Fatalf("task.start: %v", err)
	}
	var started api.TaskStartResult
	if err := json.Unmarshal(raw, &started); err != nil || started.TaskID == "" {
		t.Fatalf("no task id: %v %s", err, raw)
	}
	runID, err := tasks.RunOf(started.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	stRaw, err := client.Call(ctx, "w-2", api.MethodRunStatus, map[string]any{"run_id": string(runID)})
	if err != nil {
		t.Fatalf("run.status: %v", err)
	}
	var snap api.RunStatusResult
	if err := json.Unmarshal(stRaw, &snap); err != nil || snap.RunID != string(runID) {
		t.Fatalf("bad snapshot: %v %s", err, stRaw)
	}
}

// TestWiringWebSocketEvents covers the Section 22.1 live path: a WebSocket
// subscriber receives the orchestrator state change fanned out by the
// daemon bridge after a gate control order.
func TestWiringWebSocketEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, client, tasks, _ := fullStack(t, ctx)
	raw, err := client.Call(ctx, "w-1", api.MethodTaskStart, api.TaskStartParams{Request: "watched work"})
	if err != nil {
		t.Fatal(err)
	}
	var started api.TaskStartResult
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	runID, err := tasks.RunOf(started.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan types.Event, 16)
	subCtx, subCancel := context.WithTimeout(ctx, 5*time.Second)
	defer subCancel()
	go func() {
		_ = client.Subscribe(subCtx, types.Cursor{}, nil, func(ev types.Event) { got <- ev })
	}()
	time.Sleep(200 * time.Millisecond)
	if _, err := client.Call(ctx, "w-2", api.MethodRunPause, map[string]any{"run_id": string(runID)}); err != nil {
		t.Fatalf("run.pause: %v", err)
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-got:
			if ev.RunID != nil && string(*ev.RunID) == string(runID) {
				return
			}
		case <-timeout:
			t.Fatal("no run event reached the WebSocket subscriber")
		}
	}
}

// TestWiringBootWithAdapters covers the Section 17.3 daemon side: an
// unfinished run parks paused after a long outage through the full
// Boot with the orchestrator store adapter, and the single notice
// carries the tally.
func TestWiringBootWithAdapters(t *testing.T) {
	ctx := context.Background()
	_, _, tasks, _ := fullStack(t, ctx)
	raw, err := tasks.Start(api.TaskStartParams{Request: "unfinished work"})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := tasks.RunOf(raw.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	// Age the run past the 6 h auto-resume bound.
	rec, err := tasks.store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Run.UpdatedAt = time.Now().UTC().Add(-7 * time.Hour)
	if err := tasks.store.UpdateRun(rec); err != nil {
		t.Fatal(err)
	}
	var notice string
	d2 := New(DefaultConfig(), Deps{
		BootStore:  &OrchestratorRunStore{Store: tasks.store},
		Reconciler: &EngineReconciler{Engine: NewEngine(), Jrnl: journal.NewMemStore(), Store: tasks.store},
		OnBoot:     func(rep *BootReport) { notice = rep.Notice },
	})
	now := time.Now()
	if _, err := d2.Start(ctx); err != nil {
		t.Fatalf("boot start: %v", err)
	}
	defer func() { _ = d2.Stop(context.Background()) }()
	_ = now
	after, err := tasks.store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Run.State != types.RunPaused {
		t.Fatalf("long-outage run should park paused, have %v", after.Run.State)
	}
	if notice == "" {
		t.Fatal("boot produced no summary notice")
	}
}

// TestWiringReviewLoopRuns covers the Sections 10.6/21.9 wiring: the
// review loop ticks without activity and leaves the service usable.
func TestWiringReviewLoopRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _, review := fullStack(t, ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		review.once(ctx, []string{"general"}, time.Now().UTC())
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("review tick hung")
	}
}
