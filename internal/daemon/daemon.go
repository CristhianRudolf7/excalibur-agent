package daemon

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

// Config carries the daemon surface of Sections 3.2, 17.3 and 25.2.
type Config struct {
	// Listen is the loopback bind (default 127.0.0.1:0 in tests).
	Listen string
	// DataDir holds crash history, lock file and blobs.
	DataDir string
	// RuntimeDir holds the addr rendezvous file for CLI/MCP/ACP
	// discovery. Empty disables publication (embedded default).
	RuntimeDir string
	// ReservedCPUs stays free for the interactive path (Appendix B: 2).
	ReservedCPUs int
	// RAMThresholdBytes trips the RAM watchdog (Section 5.2); zero
	// disables the threshold check in tests.
	RAMThresholdBytes uint64
	// MemoryLimitBytes sets GOMEMLIMIT; zero leaves the runtime alone.
	MemoryLimitBytes uint64
}

// DefaultConfig binds loopback with the Appendix B reservations.
func DefaultConfig() Config {
	return Config{Listen: "127.0.0.1:0", ReservedCPUs: platform.DefaultReservedCPUs}
}

// Deps wires the H8 engine under the daemon: the gate backend, the
// master intake and the orchestrator (Section H9).
type Deps struct {
	Gate   *gateway.Server
	Master *master.Master
	Orch   *orchestrator.Orchestrator
	Bus    *telemetry.Bus
	// Backend is the composed gate backend served by Gate. Backend()
	// exposes it for delegation checks; nil keeps the gate behavior
	// the server was built with.
	Backend *DaemonBackend
	// Services is the assembled daemon stack (agenda, triggers,
	// capabilities, memory, review loop, watchdog). Start launches
	// its loops; Stop parks them.
	Services *Services
	// Adoption reconciles non-terminal runs at boot (Section 17.3).
	// Nil keeps runs untouched (embedded/test default).
	Adoption Adoption
	// BootStore, when set, runs the full Section 17.3 Boot with the
	// reconciler and agenda catch-up instead of the narrow Adoption
	// pass above.
	BootStore RunStore
	// Reconciler rebuilds one run from its journal at boot. Nil
	// skips per-run reconciliation (decisions still park runs).
	Reconciler RunReconciler
	// AgendaBoot recalculates missed firings at boot (17.4). Nil
	// skips agenda catch-up.
	AgendaBoot AgendaCatchUp
	// DownSince anchors agenda catch-up; zero means now.
	DownSince time.Time
	// OnBoot receives the boot report for the single master notice
	// (17.3 step 6). Nil drops it.
	OnBoot func(*BootReport)
	// Crashed runs when a writer goroutine fails: the daemon exits
	// cleanly and lets the supervisor restart it (Section 3.2).
	Crashed func(owner string)
}

// Daemon is the Section 3.1 resident process: one gate, one master
// actor, the H8 orchestrator, the admission semaphore for background
// work, and the boot/shutdown lifecycle. The interactive path (gate,
// intake, fast lane, master turns, read-only status) never blocks on
// admission; engines, the orchestrator and integrated tools do.
type Daemon struct {
	cfg Config
	dep Deps

	admit *platform.Admission

	mu       sync.Mutex
	running  bool
	addr     string
	stop     chan struct{}
	stopOnce sync.Once
	// svcCancel ends the service loops; svcDone closes as each exits.
	svcCancel context.CancelFunc
	svcDone   []<-chan struct{}
	crashes   []time.Time
	safeMode  bool
	quarant   map[string]bool
}

// New returns a daemon. Admission sizes to NumCPU-ReservedCPUs with
// at least one slot (Section 3.2).
func New(cfg Config, dep Deps) *Daemon {
	if cfg.ReservedCPUs <= 0 {
		cfg.ReservedCPUs = platform.DefaultReservedCPUs
	}
	return &Daemon{
		cfg:     cfg,
		dep:     dep,
		admit:   platform.DefaultAdmission(cfg.ReservedCPUs),
		stop:    make(chan struct{}),
		quarant: map[string]bool{},
	}
}

// Admission exposes the background semaphore for engines and tools.
func (d *Daemon) Admission() *platform.Admission { return d.admit }

// Addr returns the bound gate address after Start.
func (d *Daemon) Addr() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.addr
}

// SafeMode reports whether boot entered Section 17.3 safe mode.
func (d *Daemon) SafeMode() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.safeMode
}

// Quarantined reports runs held out of admission as a likely crash
// cause (Section 17.3).
func (d *Daemon) Quarantined(runID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.quarant[runID]
}

// Start boots the daemon: crash-history check (safe mode), boot
// adoption, orchestrator loop, services loops (agenda, triggers,
// review, watchdog), gate serve and watchdog. It returns once the
// gate listens.
func (d *Daemon) Start(ctx context.Context) (string, error) {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return "", fmt.Errorf("daemon: already running")
	}
	d.mu.Unlock()
	if d.cfg.MemoryLimitBytes > 0 {
		debug.SetMemoryLimit(int64(d.cfg.MemoryLimitBytes))
	}
	if d.cfg.DataDir != "" {
		if err := os.MkdirAll(d.cfg.DataDir, 0o755); err != nil {
			return "", fmt.Errorf("daemon: mkdir data: %w", err)
		}
	}
	d.checkCrashHistory(time.Now())
	now := time.Now()
	cl := d.crashLog()
	if cl != nil {
		// Structured history is the boot authority: 30 quiet minutes
		// exit safe mode, and quarantine survives restarts. The JSONL
		// trail stays as the audit log.
		if _, err := cl.MaybeExitSafeMode(now); err == nil {
			d.mu.Lock()
			if cl.InSafeMode() {
				d.safeMode = true
			}
			for _, q := range cl.Quarantined() {
				d.quarant[q] = true
			}
			d.mu.Unlock()
		}
	}
	if d.dep.BootStore != nil {
		downSince := d.dep.DownSince
		if downSince.IsZero() {
			downSince = now
		}
		rep, err := Boot(ctx, d.dep.BootStore, d.dep.Reconciler, d.dep.AgendaBoot, cl, now, downSince)
		if err != nil {
			return "", err
		}
		if d.dep.OnBoot != nil {
			d.dep.OnBoot(rep)
		}
	} else {
		adopted, err := d.adopt(ctx, now)
		if err != nil {
			return "", err
		}
		_ = adopted
	}
	// Service loops end with the daemon, not with the caller's ctx
	// alone: Stop cancels them and waits for each to exit.
	svcCtx, svcCancel := context.WithCancel(ctx)
	if d.dep.Services != nil && d.dep.Services.Library != nil {
		if err := d.dep.Services.Library.Start(svcCtx); err != nil {
			svcCancel()
			return "", fmt.Errorf("daemon: library start: %w", err)
		}
	}
	if d.dep.Orch != nil {
		d.dep.Orch.Start(ctx)
	}
	svcDone := d.startServices(svcCtx)
	d.mu.Lock()
	d.svcCancel = svcCancel
	d.svcDone = svcDone
	d.mu.Unlock()
	if d.dep.Gate != nil {
		addr, err := d.dep.Gate.Start()
		if err != nil {
			svcCancel()
			waitDone(svcDone, serviceStopTimeout)
			return "", err
		}
		d.mu.Lock()
		d.addr = addr
		d.mu.Unlock()
		if d.cfg.RuntimeDir != "" {
			_ = WriteAddrFile(d.cfg.RuntimeDir, addr)
		}
	}
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()
	d.recordStart()
	return d.Addr(), nil
}

// crashLog returns the structured crash history beside the
// databases, or nil without a data dir (embedded/test default).
func (d *Daemon) crashLog() *CrashLog {
	if d.cfg.DataDir == "" {
		return nil
	}
	return NewCrashLog(d.cfg.DataDir)
}

// serviceStopTimeout bounds how long Stop waits for service loops.
const serviceStopTimeout = 5 * time.Second

// startServices launches the assembled service loops: the agenda
// timer, the trigger pump, the memory review loop, the retention loop
// and the RAM watchdog. Nil services are skipped. It returns one done
// channel per loop that runs in its own goroutine.
func (d *Daemon) startServices(ctx context.Context) []<-chan struct{} {
	svc := d.dep.Services
	if svc == nil {
		return nil
	}
	var done []<-chan struct{}
	if svc.Robot != nil {
		svc.Robot.Start(ctx)
		done = append(done, svc.Robot.Done())
	}
	if svc.RobotLab != nil && svc.RobotLab.Sidecar != nil {
		stopped := make(chan struct{})
		safego.Go("robotlab-lifecycle", nil, func() { defer close(stopped); <-ctx.Done(); _ = svc.RobotLab.Close() })
		done = append(done, stopped)
	}
	if svc.Library != nil {
		done = append(done, svc.Library.Done())
	}
	if svc.Automations != nil {
		svc.Automations.Start(ctx)
	}
	if svc.Agenda != nil {
		svc.Agenda.Start()
	}
	if svc.Triggers != nil {
		var sources []<-chan agenda.Event
		if svc.Events != nil {
			sources = append(sources, svc.Events)
		}
		done = append(done, svc.Triggers.Run(ctx, sources...))
	}
	if svc.Review != nil {
		done = append(done, svc.Review.Start(ctx, svc.ReviewScopes))
	}
	if svc.Retention != nil {
		done = append(done, svc.Retention.Start(ctx))
	}
	if svc.Dog != nil {
		done = append(done, svc.Dog.Start(ctx))
	}
	return done
}

// waitDone blocks until every channel closes or the timeout passes.
// It reports whether all loops exited in time.
func waitDone(chans []<-chan struct{}, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for _, c := range chans {
		select {
		case <-c:
		case <-deadline.C:
			return false
		}
	}
	return true
}

// Stop performs the controlled shutdown: intake stops first (the gate
// shuts down with a drain), then the orchestrator loop, then the bus.
// Runs persist in the journal; boot re-adopts them (Section 17.3).
func (d *Daemon) Stop(ctx context.Context) error {
	if d.dep.Services != nil {
		defer func() { _ = d.dep.Services.BrowserWeb.Close() }()
		defer func() { _ = d.dep.Services.RobotLab.Close() }()
	}
	var first error
	if d.dep.Services != nil && d.dep.Services.Robot != nil {
		first = d.dep.Services.Robot.Close()
	}
	d.stopOnce.Do(func() { close(d.stop) })
	if d.dep.Gate != nil {
		drain, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := d.dep.Gate.Stop(drain); err != nil && first == nil {
			first = err
		}
	}
	if d.dep.Master != nil {
		turnCtx, cancelTurns := context.WithTimeout(ctx, 5*time.Second)
		if err := d.dep.Master.Turns().Stop(turnCtx); err != nil && first == nil {
			first = err
		}
		cancelTurns()
	}
	if svc := d.dep.Services; svc != nil && svc.delegations != nil {
		svc.delegations.stop()
	}
	if svc := d.dep.Services; svc != nil && svc.Automations != nil {
		svc.Automations.Stop()
	}
	if d.dep.Orch != nil {
		d.dep.Orch.Stop()
	}
	if svc := d.dep.Services; svc != nil && svc.Agenda != nil {
		svc.Agenda.Stop()
	}
	d.mu.Lock()
	cancel, done := d.svcCancel, d.svcDone
	d.svcCancel, d.svcDone = nil, nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
		if !waitDone(done, serviceStopTimeout) && first == nil {
			first = fmt.Errorf("daemon: service loops did not stop within %s", serviceStopTimeout)
		}
	}
	if d.cfg.RuntimeDir != "" {
		RemoveAddrFile(d.cfg.RuntimeDir)
	}
	d.mu.Lock()
	d.running = false
	d.mu.Unlock()
	return first
}

// Running reports whether Start succeeded without a later Stop.
func (d *Daemon) Running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

// Backend exposes the composed gate backend for delegation checks.
func (d *Daemon) Backend() *DaemonBackend { return d.dep.Backend }
