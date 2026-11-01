package daemon

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

// countingSampler counts RSS samples taken by the RAM watchdog.
type countingSampler struct{ n atomic.Int64 }

func (c *countingSampler) Sample(int) (platform.ResourceSample, error) {
	c.n.Add(1)
	return platform.ResourceSample{RSSBytes: 1}, nil
}

// eventually polls cond for up to two seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestServicesStartAndStopCleanly arms every daemon service (agenda,
// trigger pump, daily review, retention, RAM watchdog), checks that
// each one runs, then checks that Stop waits for all of them.
func TestServicesStartAndStopCleanly(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "svc.db"), store.DefaultOptions(), migrations.Main())
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

	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	notifier := master.NewNotifier(master.NotifierConfig{})
	launcher, err := NewOrchestratorLauncher(orch, notifier)
	if err != nil {
		t.Fatal(err)
	}
	ag := agenda.NewAgenda(agenda.NewSQLAgendaStore(db), agenda.NewSQLReminderStore(db), launcher)
	trig, err := NewDaemonTriggerLauncher(orch, orchStore, notifier)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan agenda.Event, 4)
	memSvc := memory.NewService(db)
	sampler := &countingSampler{}
	var compactions atomic.Int64
	// A fixed clock inside the maintenance hour makes the retention
	// pass due on its first poll.
	window := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

	svc, err := Assemble(AssembleOptions{Store: orchStore, Orch: orch, Bus: telemetry.NewBus(nil)})
	if err != nil {
		t.Fatal(err)
	}
	svc.Agenda = ag
	svc.Triggers = agenda.NewTriggerPump(agenda.NewTriggerEngine(), trig)
	svc.Events = events
	svc.Memory = memSvc
	svc.Review = &MemoryReviewLoop{Service: memSvc, Interval: 5 * time.Millisecond}
	svc.ReviewScopes = []string{"general"}
	svc.Dog = &RAMWatchdog{Sampler: sampler, Orch: orch, Threshold: 1 << 30, Interval: 5 * time.Millisecond}
	svc.Retention = &RetentionLoop{
		Scheduler: NewRetentionScheduler(3), Interval: 5 * time.Millisecond,
		Now: func() time.Time { return window },
		Phases: RetentionPhases{CompactJournals: func(context.Context) error {
			compactions.Add(1)
			return nil
		}},
	}

	d := New(DefaultConfig(), Deps{Orch: orch, Services: svc})
	if _, err := d.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !ag.Running() {
		t.Fatal("agenda timer must run after daemon start")
	}
	d.mu.Lock()
	loops := append([]<-chan struct{}(nil), d.svcDone...)
	d.mu.Unlock()
	if len(loops) != 4 {
		t.Fatalf("want 4 service loops (pump, review, retention, watchdog), have %d", len(loops))
	}

	events <- agenda.Event{ID: "evt-1", Type: "test.event"}
	eventually(t, "watchdog sampling", func() bool { return sampler.n.Load() > 0 })
	eventually(t, "retention pass", func() bool { return compactions.Load() > 0 })

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if ag.Running() {
		t.Fatal("agenda timer must stop with the daemon")
	}
	for i, done := range loops {
		select {
		case <-done:
		default:
			t.Fatalf("service loop %d still running after Stop", i)
		}
	}
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}
