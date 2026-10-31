// Watchdog tests: 25.2 supervision — pet only when the interactive
// path answers, ready/stop lifecycle, hung path stays silent.
package daemon

import (
	"testing"
	"time"
)

type notifyLog struct {
	sent []string
	err  error
}

func (l *notifyLog) send(_, msg string) error {
	if l.err != nil {
		return l.err
	}
	l.sent = append(l.sent, msg)
	return nil
}

func TestWatchdogPetsWhenAlive(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{SocketPath: "/run/notify"}, func() bool { return true })
	log := &notifyLog{}
	w.notify = log.send
	petted, err := w.Tick()
	if err != nil || !petted || len(log.sent) != 1 || log.sent[0] != "WATCHDOG=1" {
		t.Errorf("Tick = %v %v %v, want one WATCHDOG=1", petted, err, log.sent)
	}
}

func TestWatchdogSilentWhenHung(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{SocketPath: "/run/notify"}, func() bool { return false })
	log := &notifyLog{}
	w.notify = log.send
	petted, err := w.Tick()
	if err != nil || petted || len(log.sent) != 0 {
		t.Errorf("hung path must stay silent for supervisor restart: %v %v %v", petted, err, log.sent)
	}
}

func TestWatchdogSilentWithoutSocket(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{}, func() bool { return true })
	log := &notifyLog{}
	w.notify = log.send
	petted, err := w.Tick()
	if err != nil || petted {
		t.Errorf("no socket means unmanaged: Tick = %v %v", petted, err)
	}
	if err := w.Ready(); err != nil {
		t.Errorf("Ready without socket is a no-op: %v", err)
	}
}

func TestWatchdogLifecycle(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{SocketPath: "/run/notify"}, func() bool { return true })
	log := &notifyLog{}
	w.notify = log.send
	if err := w.Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := w.Stopping(); err != nil {
		t.Fatalf("Stopping: %v", err)
	}
	if len(log.sent) != 2 || log.sent[0] != "READY=1" || log.sent[1] != "STOPPING=1" {
		t.Errorf("lifecycle = %v", log.sent)
	}
}

func TestWatchdogStartStopViaSafego(t *testing.T) {
	w := NewWatchdog(WatchdogConfig{SocketPath: "/run/notify", Interval: 5 * time.Millisecond}, func() bool { return true })
	log := &notifyLog{}
	w.notify = log.send
	failed := make(chan string, 1)
	w.Start(func(owner string, _ any, _ []byte) { failed <- owner })
	time.Sleep(30 * time.Millisecond)
	w.Stop()
	select {
	case owner := <-failed:
		t.Fatalf("watchdog loop panicked: %s", owner)
	default:
	}
	if len(log.sent) == 0 {
		t.Error("loop must pet while alive")
	}
}
