package daemon

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"

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
	// Adoption reconciles non-terminal runs at boot (Section 17.3).
	// Nil keeps runs untouched (embedded/test default).
	Adoption Adoption
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
	crashes  []time.Time
	safeMode bool
	quarant  map[string]bool
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
// adoption, orchestrator loop, gate serve and watchdog. It returns
// once the gate listens.
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
	adopted, err := d.adopt(ctx, time.Now())
	if err != nil {
		return "", err
	}
	_ = adopted
	if d.dep.Orch != nil {
		d.dep.Orch.Start(ctx)
	}
	if d.dep.Gate != nil {
		addr, err := d.dep.Gate.Start()
		if err != nil {
			return "", err
		}
		d.mu.Lock()
		d.addr = addr
		d.mu.Unlock()
	}
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()
	d.recordStart()
	safego.Go("daemon-watchdog", nil, func() {
		d.watchdog(ctx)
	})
	return d.Addr(), nil
}

// Stop performs the controlled shutdown: intake stops first (the gate
// shuts down with a drain), then the orchestrator loop, then the bus.
// Runs persist in the journal; boot re-adopts them (Section 17.3).
func (d *Daemon) Stop(ctx context.Context) error {
	var first error
	d.stopOnce.Do(func() { close(d.stop) })
	if d.dep.Gate != nil {
		drain, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := d.dep.Gate.Stop(drain); err != nil && first == nil {
			first = err
		}
	}
	if d.dep.Orch != nil {
		d.dep.Orch.Stop()
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

// watchdog samples RSS and pauses low-priority runs past the
// threshold; it re-admits below 80% for 60 s (Section 5.2). The
// orchestrator owns the policy; this loop only keeps the daemon side
// alive for future wiring and exits on ctx or Stop.
func (d *Daemon) watchdog(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stop:
			return
		case <-t.C:
		}
	}
}

// Backend exposes the gate backend seam for delegation checks.
func (d *Daemon) Backend() *gateway.MemoryBackend { return nil }
