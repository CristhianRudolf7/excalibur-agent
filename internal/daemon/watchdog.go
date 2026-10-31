// Supervisor watchdog (Section 25.2).
//
// systemd supervises the daemon with Restart=on-failure, growing
// RestartSec and WatchdogSec. The daemon pets WATCHDOG=1 through
// sd_notify from a goroutine on the interactive path that only ticks
// while that path answers: a hung daemon is detected and restarted.
// The supervisor only restarts; crash-loop protection is safe mode
// (17.3), never the supervisor.
//
// NotifySocket from $NOTIFY_SOCKET uses the sd_notify datagram
// protocol over a unix socket. The sender is injectable so tests run
// without systemd.
package daemon

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// WatchdogConfig tunes the watchdog.
type WatchdogConfig struct {
	// SocketPath overrides $NOTIFY_SOCKET (tests, containers).
	SocketPath string
	// Interval paces WATCHDOG=1; systemd WatchdogSec must exceed it.
	Interval time.Duration
}

// Notifier sends one sd_notify datagram.
type Notifier func(socketPath, message string) error

// SendNotify delivers message to socketPath over unixgram.
func SendNotify(socketPath, message string) error {
	if socketPath == "" {
		return fmt.Errorf("daemon: notify: no socket")
	}
	addr, err := net.ResolveUnixAddr("unixgram", socketPath)
	if err != nil {
		return err
	}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(message)); err != nil {
		return err
	}
	return nil
}

// Watchdog pets the supervisor while the interactive path answers.
type Watchdog struct {
	cfg      WatchdogConfig
	notify   Notifier
	alive    func() bool
	stop     chan struct{}
	stopped  chan struct{}
	ownerTag string
}

// NewWatchdog returns a watchdog that calls alive on the interactive
// path before every tick: only a responsive path pets the supervisor.
func NewWatchdog(cfg WatchdogConfig, alive func() bool) *Watchdog {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	return &Watchdog{
		cfg:      cfg,
		notify:   SendNotify,
		alive:    alive,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
		ownerTag: "daemon/watchdog",
	}
}

// SocketPath resolves the notify socket: explicit config first, then
// $NOTIFY_SOCKET.
func (w *Watchdog) SocketPath() string {
	if w.cfg.SocketPath != "" {
		return w.cfg.SocketPath
	}
	return os.Getenv("NOTIFY_SOCKET")
}

// Tick sends one WATCHDOG=1 when the interactive path answers and
// reports whether it petted. A silent tick means the path is hung:
// the supervisor then restarts the daemon.
func (w *Watchdog) Tick() (petted bool, err error) {
	if w.alive != nil && !w.alive() {
		return false, nil
	}
	sock := w.SocketPath()
	if sock == "" {
		return false, nil
	}
	if w.notify == nil {
		return false, nil
	}
	if err := w.notify(sock, "WATCHDOG=1"); err != nil {
		return false, err
	}
	return true, nil
}

// Ready notifies service readiness (Type=notify units start only
// after READY=1).
func (w *Watchdog) Ready() error {
	sock := w.SocketPath()
	if sock == "" || w.notify == nil {
		return nil
	}
	return w.notify(sock, "READY=1")
}

// Stopping notifies clean shutdown.
func (w *Watchdog) Stopping() error {
	sock := w.SocketPath()
	if sock == "" || w.notify == nil {
		return nil
	}
	return w.notify(sock, "STOPPING=1")
}

// Start launches the tick loop on the interactive path via safego.Go
// so a panic marks the owner failed instead of killing the daemon.
// onFail fires only if the loop itself panics, never on hung ticks.
func (w *Watchdog) Start(onFail safego.FailFunc) {
	safego.Go(w.ownerTag, onFail, func() {
		t := time.NewTicker(w.cfg.Interval)
		defer t.Stop()
		defer close(w.stopped)
		for {
			select {
			case <-w.stop:
				return
			case <-t.C:
				_, _ = w.Tick()
			}
		}
	})
}

// Stop ends the loop.
func (w *Watchdog) Stop() {
	select {
	case <-w.stopped:
		return
	default:
	}
	close(w.stop)
	<-w.stopped
}
