package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// RetentionLoop drives the Section 4.7 retention pass from the daemon
// services: it polls the RetentionScheduler, which fires once per day
// inside its maintenance hour, and runs the phases in order. One
// safego.Go goroutine; a failing pass is logged and retried by the
// next scheduled window.
type RetentionLoop struct {
	Scheduler *RetentionScheduler
	Phases    RetentionPhases
	// Interval paces the due check; zero defaults to ten minutes.
	Interval time.Duration
	// Now overrides the clock in tests; nil uses time.Now.
	Now func() time.Time
	// OnReport observes each completed or failed pass. Nil drops it.
	OnReport func(RetentionReport, error)
}

// Start runs the loop until ctx ends. The returned channel closes when
// the goroutine exits.
func (l *RetentionLoop) Start(ctx context.Context) <-chan struct{} {
	interval := l.Interval
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	now := l.Now
	if now == nil {
		now = time.Now
	}
	done := make(chan struct{})
	safego.Go("daemon-retention", nil, func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				l.once(ctx, now())
			}
		}
	})
	return done
}

// once runs one due-check; exported behavior is covered through Start.
func (l *RetentionLoop) once(ctx context.Context, at time.Time) {
	if l.Scheduler == nil {
		return
	}
	rep, ran, err := l.Scheduler.MaybeRun(ctx, at, l.Phases)
	if !ran && err == nil {
		return
	}
	if err != nil {
		slog.Warn("retention pass failed; the next window retries", "error", err.Error())
	}
	if l.OnReport != nil {
		l.OnReport(rep, err)
	}
}
