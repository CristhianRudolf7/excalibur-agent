package agenda

import (
	"context"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// Launcher delivers due fires and reminders. The daemon wires it to
// the orchestrator; tests use a recording fake. Launch calls must be
// quick (enqueue work, never run it) because Tick invokes them
// synchronously to preserve fire order.
type Launcher interface {
	LaunchFire(ctx context.Context, fire Fire) error
	LaunchReminder(ctx context.Context, r Reminder) error
}

// Agenda keeps the next firing in memory (a timer over the minimum
// next_fire_utc) and persists every firing with its fire_id (21.1,
// 17.4). In safe mode it registers due firings but launches nothing;
// they stay pending until safe mode ends (17.3).
type Agenda struct {
	store     AgendaStore
	reminders ReminderStore
	launcher  Launcher
	now       func() time.Time

	mu       sync.Mutex
	safeMode bool
	stopCh   chan struct{}
	wakeCh   chan struct{}
	stopped  chan struct{}
	started  bool
}

// NewAgenda returns an agenda over the given store. A nil store gets
// an in-memory one; a nil launcher drops launches (registration still
// happens, which is exactly safe-mode behavior).
func NewAgenda(store AgendaStore, reminders ReminderStore, launcher Launcher) *Agenda {
	if store == nil {
		store = NewMemoryAgendaStore()
	}
	if reminders == nil {
		reminders = NewMemoryReminderStore()
	}
	return &Agenda{
		store:     store,
		reminders: reminders,
		launcher:  launcher,
		now:       time.Now,
		stopCh:    make(chan struct{}),
		wakeCh:    make(chan struct{}, 1),
		stopped:   make(chan struct{}),
	}
}

// AddSchedule validates, computes the first fire and registers the
// schedule, waking the timer.
func (a *Agenda) AddSchedule(s *Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}
	now := a.now().UTC()
	if s.NextFireUTC.IsZero() && s.Kind != ScheduleAfter {
		loc, err := LoadZone(s.Timezone)
		if err != nil {
			return err
		}
		next, err := s.NextAfter(now, loc)
		if err != nil {
			return err
		}
		s.NextFireUTC = next.UTC()
	}
	if err := a.store.SaveSchedule(s); err != nil {
		return err
	}
	a.wake()
	return nil
}

// RemoveSchedule deletes a schedule.
func (a *Agenda) RemoveSchedule(id string) error {
	if err := a.store.DeleteSchedule(id); err != nil {
		return err
	}
	a.wake()
	return nil
}

// SaveReminder creates or updates a reminder, waking the timer. The
// master turn files user reminders through here; Tick delivers them.
func (a *Agenda) SaveReminder(r *Reminder) error {
	if err := a.reminders.SaveReminder(r); err != nil {
		return err
	}
	a.wake()
	return nil
}

// SafeMode reports whether launches are held.
func (a *Agenda) SafeMode() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.safeMode
}

// EnterSafeMode holds launches; due fires park as pending (17.3).
func (a *Agenda) EnterSafeMode() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.safeMode = true
}

// ExitSafeMode resumes launches and flushes parked fires in order.
func (a *Agenda) ExitSafeMode(ctx context.Context) []error {
	a.mu.Lock()
	a.safeMode = false
	a.mu.Unlock()
	var errs []error
	for _, f := range a.store.PendingFires() {
		if err := a.launchFire(ctx, f); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	a.wake()
	return errs
}

// ReconcileBoot recomputes the schedules that became due during a
// downtime window [downSince, now] (17.3 step 5, 17.4). Fires are
// delivered (or parked in safe mode) exactly once per fire_id.
func (a *Agenda) ReconcileBoot(ctx context.Context, downSince, now time.Time) ([]Fire, error) {
	var delivered []Fire
	for _, s := range a.store.ListSchedules() {
		cp := *s
		fires, err := cp.ReconcileMissed(downSince, now)
		if err != nil {
			return delivered, err
		}
		if err := a.store.SaveSchedule(&cp); err != nil {
			return delivered, err
		}
		for _, f := range fires {
			if a.store.Delivered(f.FireID) {
				continue
			}
			if a.inSafeMode() {
				if err := a.store.QueuePending(f); err != nil {
					return delivered, err
				}
				continue
			}
			if err := a.launchFire(ctx, f); err != nil {
				return delivered, err
			}
			delivered = append(delivered, f)
		}
	}
	a.wake()
	return delivered, nil
}

// Tick fires everything due at now: schedules and reminders. It is the
// single firing path used by the timer loop and by tests.
func (a *Agenda) Tick(ctx context.Context, now time.Time) ([]Fire, []Reminder) {
	var fired []Fire
	for _, s := range a.store.ListSchedules() {
		if s.Done || s.Kind == ScheduleAfter {
			continue
		}
		if s.NextFireUTC.IsZero() || s.NextFireUTC.After(now) {
			continue
		}
		f := Fire{FireID: newFireID(), ScheduleID: s.ID, Scheduled: s.NextFireUTC.UTC(), FiredAt: now.UTC(), Payload: s.Payload}
		cp := *s
		loc, err := LoadZone(cp.Timezone)
		if err == nil {
			if next, nerr := cp.NextAfter(now, loc); nerr == nil {
				cp.NextFireUTC = next.UTC()
			} else if cp.Kind == ScheduleAt {
				cp.Done = true
			}
		}
		_ = a.store.SaveSchedule(&cp)
		if a.inSafeMode() {
			_ = a.store.QueuePending(f)
			continue
		}
		if err := a.launchFire(ctx, f); err != nil {
			_ = a.store.QueuePending(f)
			continue
		}
		fired = append(fired, f)
	}
	var dueReminders []Reminder
	for _, r := range a.reminders.DueReminders(now) {
		cp := *r
		next := cp.recordDelivery(now)
		_ = a.reminders.SaveReminder(&cp)
		if a.launcher != nil {
			_ = a.launcher.LaunchReminder(ctx, cp)
		}
		_ = next
		dueReminders = append(dueReminders, cp)
	}
	a.wake()
	return fired, dueReminders
}

// AfterComplete fires a ScheduleAfter whose dependency finished.
func (a *Agenda) AfterComplete(ctx context.Context, dependencyID string, now time.Time) []Fire {
	var fired []Fire
	for _, s := range a.store.ListSchedules() {
		if s.Kind != ScheduleAfter || s.AfterID != dependencyID || s.Done {
			continue
		}
		cp := *s
		cp.Done = true
		_ = a.store.SaveSchedule(&cp)
		f := Fire{FireID: newFireID(), ScheduleID: s.ID, Scheduled: now.UTC(), FiredAt: now.UTC(), Payload: s.Payload}
		if a.inSafeMode() {
			_ = a.store.QueuePending(f)
			continue
		}
		if err := a.launchFire(ctx, f); err != nil {
			_ = a.store.QueuePending(f)
			continue
		}
		fired = append(fired, f)
	}
	return fired
}

func (a *Agenda) launchFire(ctx context.Context, f Fire) error {
	if a.store.Delivered(f.FireID) {
		return nil
	}
	if a.launcher != nil {
		if err := a.launcher.LaunchFire(ctx, f); err != nil {
			return err
		}
	}
	return a.store.MarkDelivered(f)
}

func (a *Agenda) inSafeMode() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.safeMode
}

func (a *Agenda) wake() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}

// Start runs the timer loop until Stop: the loop sleeps until the
// minimum next_fire_utc across schedules and reminders (21.1).
func (a *Agenda) Start() {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return
	}
	a.started = true
	a.stopCh = make(chan struct{})
	a.stopped = make(chan struct{})
	a.mu.Unlock()
	safego.Go("agenda-timer", nil, func() {
		defer close(a.stopped)
		for {
			wait := a.untilNext()
			timer := time.NewTimer(wait)
			a.mu.Lock()
			stopCh := a.stopCh
			a.mu.Unlock()
			select {
			case <-stopCh:
				timer.Stop()
				return
			case <-a.wakeCh:
				timer.Stop()
			case <-timer.C:
				a.Tick(context.Background(), a.now().UTC())
			}
		}
	})
}

// Running reports whether the timer loop is started and not yet
// stopped.
func (a *Agenda) Running() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

// Stop halts the timer loop.
func (a *Agenda) Stop() {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	close(a.stopCh)
	<-a.stopped
	a.mu.Lock()
	a.started = false
	a.mu.Unlock()
}

func (a *Agenda) untilNext() time.Duration {
	now := a.now().UTC()
	best := 30 * time.Second
	for _, s := range a.store.ListSchedules() {
		if s.Done || s.Kind == ScheduleAfter || s.NextFireUTC.IsZero() {
			continue
		}
		if d := s.NextFireUTC.Sub(now); d < best {
			best = d
		}
	}
	for _, r := range a.reminders.ListReminders() {
		if r.NextFireUTC.IsZero() {
			continue
		}
		if d := r.NextFireUTC.Sub(now); d < best {
			best = d
		}
	}
	if best < 0 {
		return 0
	}
	return best
}
