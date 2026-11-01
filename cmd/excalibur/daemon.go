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
	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// daemonPidFile is the process rendezvous beside daemon.addr.
const daemonPidFile = "daemon.pid"

// vaultPassphraseFileEnv names the passphrase file that unlocks the
// vault at daemon start (0600, Section 20.1 unattended start).
const vaultPassphraseFileEnv = "EXCALIBUR_VAULT_PASSPHRASE_FILE"

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

	// The secrets vault opens first: provider keys and tool secrets
	// resolve through it (Section 20.1). A declared passphrase file
	// unlocks it for unattended starts; otherwise the keyring does when
	// reachable, and a locked vault leaves the harness running.
	var v *vault.Vault
	if data != "" {
		pf := strings.TrimSpace(os.Getenv(vaultPassphraseFileEnv))
		var kr platform.Keyring
		if pf == "" {
			kr = daemonKeyring()
		}
		var err error
		v, err = daemon.OpenVault(ctx, daemon.VaultOpenOptions{
			Path: filepath.Join(data, "vault.db"), Keyring: kr, PassphraseFile: pf,
		})
		if err != nil {
			return fmt.Errorf("daemon start: %w", err)
		}
		defer func() { _ = v.Close() }()
		if v.IsLocked() {
			fmt.Fprintln(os.Stderr, "daemon: vault locked; secrets wait until `excalibur vault unlock`")
		} else {
			fmt.Fprintf(os.Stderr, "daemon: vault open (%s)\n", v.KEKKind())
		}
	}

	bus := telemetry.NewBus(nil)
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	ctrls := &daemon.OrchControl{Orch: orch, Store: orchStore}
	notifier := master.NewNotifier(master.NotifierConfig{})

	// Production model wiring (Sections 19.1, 19.2, 19.4): the
	// intake decider and the memory embedder build from the layered
	// config, environment, credentials file and secret:// vault refs
	// through the providers registry. Unconfigured operators keep
	// the deterministic seams (nil decider, keyless embedder) with
	// a notice instead of failing to start; the eval profile never
	// applies here. Master turns and daemon run engines keep their
	// H9-owned model seams.
	modelSettings, layerCfg, modelErr := cli.ResolveSettings(config.LoadOptions{})
	if modelErr != nil {
		return fmt.Errorf("daemon start: model wiring: %w", modelErr)
	}
	var modelGateway *models.Gateway
	var choiceDec daemon.ChoiceDecider
	switch {
	case modelSettings.Stub() || !modelSettings.Configured():
		fmt.Fprintln(os.Stderr, "daemon: no model provider configured; intake and memory run deterministic")
	default:
		p, berr := cli.BuildProvider(ctx, modelSettings, daemon.VaultSecretResolver(v))
		switch {
		case berr != nil && v != nil && v.IsLocked():
			// A provider key held in the vault cannot resolve while the
			// vault is locked: the daemon starts deterministic and the
			// provider needs a restart after `excalibur vault unlock`.
			fmt.Fprintf(os.Stderr, "daemon: model provider waits for the vault: %v\n", berr)
		case berr != nil:
			return fmt.Errorf("daemon start: %w", berr)
		default:
			defer func() { _ = p.Close() }()
			modelGateway = cli.BuildGateway(modelSettings, layerCfg, p)
			defer modelGateway.Close()
			choiceDec = cli.BuildDecider(modelSettings, modelGateway)
			fmt.Fprintf(os.Stderr, "daemon: models %s\n", modelSettings.Describe())
		}
	}
	decider := daemon.NewModelDecider(choiceDec, []string{"general"})
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
	// One orchestrator serves the Services stack, the agenda and the
	// trigger launchers, so every component sees the same runs.
	// Memory with the provider embedder only when configured; without
	// keys it degrades to lexical/temporal/relational retrieval
	// (Section 10.8) instead of failing to start. Embedding requests
	// never carry reasoning effort.
	var memSvc *memory.Service
	if db != nil {
		memSvc = memory.NewService(db)
		if emb, eerr := cli.BuildEmbedder(modelSettings, nil); eerr != nil {
			fmt.Fprintf(os.Stderr, "daemon: embeddings disabled: %v\n", eerr)
		} else if emb != nil {
			memSvc = memory.NewService(db, memory.WithEmbedder(emb))
			fmt.Fprintf(os.Stderr, "daemon: embeddings via %s\n", emb.Model())
		}
	}

	ix := skills.NewIndex(skills.Roots{}, skills.BuiltinFS())
	_ = ix.Scan()

	// Projects persist in the main database (Section 9.2): the registry
	// reloads them after every restart.
	projects, err := daemon.OpenProjectRegistry(db)
	if err != nil {
		return fmt.Errorf("daemon start: %w", err)
	}
	// Daemon runs step through the configured gateway; without one the
	// engine stays unconfigured and its steps fail with a clear error.
	var endpoint engine.ModelEndpoint
	if modelGateway != nil {
		endpoint = modelGateway
	}
	svc, err := daemon.Assemble(daemon.AssembleOptions{
		Store: orchStore, Orch: orch, Jrnl: jrnl, Bus: bus,
		Models: endpoint, ModelID: modelSettings.Model,
		Memory: memSvc, Skills: ix, Vault: v, Projects: projects,
	})
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
	// Daemon-side event adapters push normalized events here (21.3).
	events := make(chan agenda.Event, 64)

	tasks, err := daemon.NewTaskService(orch, orchStore)
	if err != nil {
		return err
	}
	backend.AttachServices(daemon.BackendServices{
		Tasks:    tasks,
		Caps:     daemon.NewCapabilityRoutes(nil),
		Memory:   memSvc,
		Skills:   ix,
		Projects: projects,
		History:  workspace.NewHistory(),
		Config:   &daemon.FileConfig{},
		Metrics:  &daemon.MetricsStore{},
		Vault:    v,
	})

	// Services that run for the daemon lifetime (Sections 5.2, 10.6,
	// 21.1-21.3, 21.9, 4.7). Each is armed here, so Start launches it
	// and Stop waits for it.
	svc.Agenda = ag
	svc.Triggers = pump
	svc.Events = events
	svc.Dog = &daemon.RAMWatchdog{Sampler: daemonSampler(), Orch: orch, Threshold: daemonRAMThreshold()}
	if memSvc != nil {
		svc.Review = &daemon.MemoryReviewLoop{Service: memSvc}
		svc.ReviewScopes = []string{"general"}
	}
	if db != nil {
		svc.Retention = daemonRetention(db, data)
	}

	gcfg := gateway.DefaultConfig()
	gcfg.Listen = *listen
	g := gateway.New(gcfg, backend, gateway.NewMemoryBlobs(), bus)
	d := daemon.New(daemon.Config{Listen: *listen, DataDir: data, RuntimeDir: runtime},
		daemon.Deps{
			Gate: g, Master: m, Orch: orch, Bus: bus, Backend: backend,
			BootStore:  &daemon.OrchestratorRunStore{Store: orchStore},
			Reconciler: &daemon.EngineReconciler{Engine: svc.Engine, Jrnl: svc.Jrnl, Store: orchStore},
			AgendaBoot: ag,
			Services:   svc,
			OnBoot:     func(rep *daemon.BootReport) { fmt.Fprintln(os.Stderr, "boot:", rep.Notice) },
		})
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

// daemonRAMThreshold returns the RSS level that trips the RAM watchdog
// (Section 5.2). EXCALIBUR_RAM_THRESHOLD_MB sets it in MiB; otherwise
// it is three quarters of physical RAM where the platform reports it,
// and zero (watchdog inert) elsewhere.
func daemonRAMThreshold() uint64 {
	if raw := strings.TrimSpace(os.Getenv("EXCALIBUR_RAM_THRESHOLD_MB")); raw != "" {
		if mb, err := strconv.ParseUint(raw, 10, 64); err == nil && mb > 0 {
			return mb << 20
		}
	}
	if total := physicalRAMBytes(); total > 0 {
		return total / 4 * 3
	}
	return 0
}

// daemonRetention arms the Section 4.7 pass over the main database:
// journals and tool blobs past their horizons, then a VACUUM INTO
// backup in the rotation (4.6). Telemetry is not a separate database
// in this daemon yet, so its trim does not run here.
func daemonRetention(db *store.DB, data string) *daemon.RetentionLoop {
	layout := store.NewLayout(data)
	return &daemon.RetentionLoop{
		Scheduler: daemon.NewRetentionScheduler(3),
		Phases: daemon.RetentionPhases{
			CompactJournals: func(ctx context.Context) error {
				return store.ApplyRetention(ctx, db, nil, db.Blob(), store.DefaultRetention())
			},
			BackupAll: func(ctx context.Context) error {
				_, err := store.Backup(ctx, db.Path(), layout.BackupsDir(), layout.MainBackupPrefix())
				return err
			},
		},
		OnReport: func(rep daemon.RetentionReport, err error) {
			if err == nil {
				fmt.Fprintf(os.Stderr, "daemon: retention pass done in %d ms\n", rep.DurationMillis)
			}
		},
	}
}
