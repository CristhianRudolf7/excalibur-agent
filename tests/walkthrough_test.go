// Walkthroughs 26.1-26.11 as backend tests through the real gateway
// (Section 26.13): every scenario runs against the wired daemon stack
// over HTTP JSON-RPC with the real packages (master intake, agenda,
// orchestrator, capabilities, memory, vault, workspace). Model turns
// use the scripted interlocutor; live-model verification stays behind
// the live tag per repository rule.
package wiring_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// stack is one wired daemon with its gate client.
type stack struct {
	ctx      context.Context
	cancel   context.CancelFunc
	d        *daemon.Daemon
	client   *gateway.Client
	backend  *daemon.DaemonBackend
	orch     *orchestrator.Orchestrator
	store    orchestrator.Store
	ag       *agenda.Agenda
	notifier *master.Notifier
	mem      *memory.Service
	projects *workspace.Registry
	tasks    *daemon.TaskService
	db       *store.DB
	vlt      *vault.Vault
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bus := telemetry.NewBus(nil)
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	ctrls := &daemon.OrchControl{Orch: orch, Store: orchStore}
	notifier := master.NewNotifier(master.NotifierConfig{})
	decider := daemon.NewModelDecider(models.NewFakeDecider(0.95, 0.9, 0.1, 0.1), []string{"general"})
	m := master.New(master.Config{}, master.NewMemStore(), master.NewLexicon(), decider, ctrls, nil, notifier)
	backend := daemon.NewDaemonBackend(m, ctrls, bus)

	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "w.db"), opts, migrations.Main())
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
	memSvc := memory.NewService(db,
		memory.WithEmbedder(memory.NewFakeEmbedder("fake-test", 64)))
	v, err := vault.Open(ctx, filepath.Join(t.TempDir(), "vault.db"), vault.Options{Keyring: platformfake.NewKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	if err := v.Unlock(ctx, nil); err != nil {
		t.Fatal(err)
	}
	agStore := agenda.NewSQLAgendaStore(db)
	remStore := agenda.NewSQLReminderStore(db)
	launcher, err := daemon.NewOrchestratorLauncher(orch, notifier)
	if err != nil {
		t.Fatal(err)
	}
	ag := agenda.NewAgenda(agStore, remStore, launcher)
	reg := workspace.NewRegistry()
	ix := skills.NewIndex(skills.Roots{}, skills.BuiltinFS())
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	tasks, err := daemon.NewTaskService(orch, orchStore)
	if err != nil {
		t.Fatal(err)
	}
	regs := daemon.NewCapabilityRoutes(mustDefaultRegistry(t))
	backend.AttachServices(daemon.BackendServices{
		Tasks:    tasks,
		Caps:     regs,
		Memory:   memSvc,
		Skills:   ix,
		Vault:    v,
		Projects: reg,
		History:  workspace.NewHistory(),
		Config:   &daemon.FileConfig{UserPath: filepath.Join(t.TempDir(), "config.toml")},
		Metrics:  &daemon.MetricsStore{},
	})
	g := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), bus)
	d := daemon.New(daemon.DefaultConfig(), daemon.Deps{Gate: g, Master: m, Orch: orch, Bus: bus, Backend: backend})
	daemon.Bridge(ctx, orch, bus, notifier)
	addr, err := d.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background()) })
	return &stack{
		ctx: ctx, cancel: cancel, d: d,
		client:  gateway.Dial(addr, "walkthrough"),
		backend: backend, orch: orch, store: orchStore,
		ag: ag, notifier: notifier, mem: memSvc,
		projects: reg, tasks: tasks, db: db, vlt: v,
	}
}

func mustDefaultRegistry(t *testing.T) *capabilities.Registry {
	t.Helper()
	return capabilities.DefaultRegistry()
}

// call performs one gate call and fails the test on error.
func (s *stack) call(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	raw, err := s.client.Call(s.ctx, "wt-1", method, params)
	if err != nil {
		t.Fatalf("gate %s: %v", method, err)
	}
	return raw
}
