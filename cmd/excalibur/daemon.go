package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// daemonPidFile is the process rendezvous beside daemon.addr.
const daemonPidFile = "daemon.pid"

// runDaemonStart boots the full daemon in the foreground (Sections
// 3.2, 17.3): store, orchestrator, agenda, memory, capabilities,
// gate and the boot reconciler. It blocks until SIGINT/SIGTERM, when
// it stops cleanly and drops the rendezvous files.
func runDaemonStart(args []string) error {
	fs := flag.NewFlagSet("daemon start", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "state directory (default: platform data dir)")
	listen := fs.String("listen", "127.0.0.1:0", "loopback bind address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dirs := installDirs()
	data := *dataDir
	if data == "" {
		d, err := dirs.DataDir()
		if err != nil {
			return fmt.Errorf("daemon start: %w", err)
		}
		data = d
	}
	var runtime string
	if rt, ok := dirs.(interface{ RuntimeDir() (string, error) }); ok {
		if r, err := rt.RuntimeDir(); err == nil {
			runtime = daemon.RuntimeAddrDir(r)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bus := telemetry.NewBus(nil)
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	ctrls := &daemon.OrchControl{Orch: orch, Store: orchStore}
	notifier := master.NewNotifier(master.NotifierConfig{})
	decider := daemon.NewModelDecider(nil, []string{"general"})
	m := master.New(master.Config{Turns: master.DefaultTurnConfig()}, master.NewMemStore(), master.NewLexicon(), decider, ctrls, nil, notifier)
	backend := daemon.NewDaemonBackend(m, ctrls, bus)

	// Durable handles: main database with the memory and agenda
	// tables, journal over the porter.
	var db *store.DB
	if data != "" {
		opts := store.DefaultOptions()
		opts.BlobDir = filepath.Join(data, "blobs")
		var err error
		db, err = store.Open(ctx, filepath.Join(data, "excalibur.db"), opts, migrations.Main())
		if err != nil {
			return fmt.Errorf("daemon start: open store: %w", err)
		}
		defer func() { _ = db.Close() }()
		if err := memory.EnsureSchema(ctx, db); err != nil {
			return fmt.Errorf("daemon start: memory schema: %w", err)
		}
		if err := agenda.EnsureSQLSchema(ctx, db); err != nil {
			return fmt.Errorf("daemon start: agenda schema: %w", err)
		}
	}

	var jrnl journal.Store = journal.NewMemStore()
	if db != nil {
		jrnl = &journal.SQLStore{W: db, R: db}
	}
	svc, err := daemon.Assemble(daemon.AssembleOptions{Store: orchStore, Jrnl: jrnl, Bus: bus})
	if err != nil {
		return fmt.Errorf("daemon start: %w", err)
	}

	// Agenda over SQLite when available, memory stores otherwise.
	var agendaStore agenda.AgendaStore = agenda.NewMemoryAgendaStore()
	var reminderStore agenda.ReminderStore = agenda.NewMemoryReminderStore()
	var trigStore *agenda.SQLTriggerStore
	if db != nil {
		agendaStore = agenda.NewSQLAgendaStore(db)
		reminderStore = agenda.NewSQLReminderStore(db)
		trigStore = agenda.NewSQLTriggerStore(db)
	}
	launcher, err := daemon.NewOrchestratorLauncher(orch, notifier)
	if err != nil {
		return err
	}
	ag := agenda.NewAgenda(agendaStore, reminderStore, launcher)

	trigLauncher, err := daemon.NewDaemonTriggerLauncher(orch, orchStore, notifier)
	if err != nil {
		return err
	}
	trigEng := agenda.NewTriggerEngine()
	if trigStore != nil {
		for _, r := range trigStore.Load() {
			_ = trigEng.AddRule(r)
		}
	}
	pump := agenda.NewTriggerPump(trigEng, trigLauncher)

	// Memory with the provider embedder only when configured; without
	// keys it degrades to lexical/temporal/relational retrieval
	// (Section 10.8) instead of failing to start.
	var memSvc *memory.Service
	if db != nil {
		memSvc = memory.NewService(db)
	}

	ix := skills.NewIndex(skills.Roots{}, skills.BuiltinFS())
	_ = ix.Scan()

	tasks, err := daemon.NewTaskService(orch, orchStore)
	if err != nil {
		return err
	}
	backend.AttachServices(daemon.BackendServices{
		Tasks:    tasks,
		Caps:     daemon.NewCapabilityRoutes(nil),
		Memory:   memSvc,
		Skills:   ix,
		Projects: workspace.NewRegistry(),
		History:  workspace.NewHistory(),
		Config:   &daemon.FileConfig{},
		Metrics:  &daemon.MetricsStore{},
	})

	gcfg := gateway.DefaultConfig()
	gcfg.Listen = *listen
	g := gateway.New(gcfg, backend, gateway.NewMemoryBlobs(), bus)
	d := daemon.New(daemon.Config{Listen: *listen, DataDir: data, RuntimeDir: runtime},
		daemon.Deps{
			Gate: g, Master: m, Orch: orch, Bus: bus, Backend: backend,
			BootStore:  &daemon.OrchestratorRunStore{Store: orchStore},
			Reconciler: &daemon.EngineReconciler{Engine: svc.Engine, Jrnl: svc.Jrnl, Store: orchStore},
			AgendaBoot: ag,
			OnBoot:     func(rep *daemon.BootReport) { fmt.Fprintln(os.Stderr, "boot:", rep.Notice) },
		})
	_ = pump
	addr, err := d.Start(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("daemon listening on %s\n", addr)
	if runtime != "" {
		_ = os.WriteFile(filepath.Join(runtime, daemonPidFile), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
	}
	<-ctx.Done()
	if runtime != "" {
		_ = os.Remove(filepath.Join(runtime, daemonPidFile))
	}
	return d.Stop(context.Background())
}

// runDaemonStop signals a running daemon via its pid file and waits
// for the rendezvous to clear.
func runDaemonStop(args []string) error {
	fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dirs := installDirs()
	var runtime string
	if rt, ok := dirs.(interface{ RuntimeDir() (string, error) }); ok {
		if r, err := rt.RuntimeDir(); err == nil {
			runtime = daemon.RuntimeAddrDir(r)
		}
	}
	if runtime == "" {
		return fmt.Errorf("daemon stop: no runtime dir")
	}
	raw, err := os.ReadFile(filepath.Join(runtime, daemonPidFile))
	if err != nil {
		// No pid file: at most a stale addr file to clear.
		daemon.RemoveAddrFile(runtime)
		return fmt.Errorf("daemon stop: no running daemon")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("daemon stop: bad pid file")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("daemon stop: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("daemon stop: signal %d: %w", pid, err)
	}
	fmt.Printf("stopped daemon %d\n", pid)
	return nil
}
