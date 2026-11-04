package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/google/uuid"
)

const timeFormat = time.RFC3339Nano

// Config supplies daemon dependencies. Nil DB selects volatile storage.
type Config struct {
	DB          *store.DB
	AgentRunner AgentRunner
	ToolInvoker ToolInvoker
	Notifier    Notifier
	WorkDir     string
	Now         func() time.Time
	Language    string
}

type delivery struct {
	event     Event
	listeners []func(Event)
}
type activeRun struct {
	id     string
	cancel context.CancelFunc
}

// Manager owns scheduled automations and their durable execution history.
// Call Stop before closing DB. Subscribe callbacks may call Service methods.
type Manager struct {
	eventStop   chan struct{}
	eventWG     sync.WaitGroup
	runDuration time.Duration
	cfg         Config
	mu          sync.Mutex
	records     map[string]*record
	running     map[string]activeRun
	listeners   map[int]func(Event)
	listenerID  int
	queue       []delivery
	wake        chan struct{}
	eventWake   chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	started     bool
	stopped     bool
	detach      func() bool
}

var _ Service = (*Manager)(nil)

// NewManager restores persisted state; Start enables scheduling. Manual runs
// and subscriptions are available immediately. Stop releases all goroutines.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.WorkDir == "" {
		dir, err := defaultWorkDir()
		if err != nil {
			return nil, err
		}
		cfg.WorkDir = dir
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{cfg: cfg, records: map[string]*record{}, running: map[string]activeRun{}, listeners: map[int]func(Event){}, wake: make(chan struct{}, 1), eventWake: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
	m.eventStop = make(chan struct{})
	m.runDuration = MaxRunDuration
	if err := m.load(ctx); err != nil {
		cancel()
		return nil, err
	}
	// A process that disappeared cannot still own a running run.
	for _, r := range m.records {
		dirty := false
		for i := range r.Runs {
			if r.Runs[i].Status == RunRunning {
				now := m.now()
				r.Runs[i].Status = RunCancelled
				r.Runs[i].Error = "daemon restarted"
				r.Runs[i].FinishedAt = &now
				for j := range r.Runs[i].Steps {
					s := &r.Runs[i].Steps[j]
					if s.Status == StepRunning {
						s.Status = StepFailed
						s.Error = "daemon restarted"
						s.FinishedAt = &now
					} else if s.Status == StepPending {
						s.Status = StepSkipped
						s.FinishedAt = &now
					}
				}
				dirty = true
			}
		}
		if dirty {
			if len(r.Runs) > 0 {
				last := clone(r.Runs[0])
				r.Automation.LastRun = &last
			}
			if err := m.persistLocked(ctx, r); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	m.eventWG.Add(1)
	safego.Go("automation-events", nil, func() { defer m.eventWG.Done(); m.dispatch() })
	return m, nil
}

func (m *Manager) now() time.Time { return m.cfg.Now().UTC() }
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
func (m *Manager) emitLocked(e Event) {
	if len(m.listeners) == 0 {
		return
	}
	d := delivery{event: clone(e)}
	for _, fn := range m.listeners {
		d.listeners = append(d.listeners, fn)
	}
	m.queue = append(m.queue, d)
	signal(m.eventWake)
}
func (m *Manager) updatedLocked(a Automation) {
	m.emitLocked(Event{Kind: EventUpdated, Automation: &a})
	signal(m.wake)
}
func (m *Manager) dispatch() {
	for {
		m.mu.Lock()
		batch := m.queue
		m.queue = nil
		m.mu.Unlock()
		for _, d := range batch {
			for _, fn := range d.listeners {
				func() { defer func() { _ = recover() }(); fn(clone(d.event)) }()
			}
		}
		select {
		case <-m.eventWake:
		case <-m.eventStop:
			m.mu.Lock()
			empty := len(m.queue) == 0
			m.mu.Unlock()
			if empty {
				return
			}
		}
	}
}
func (m *Manager) Subscribe(fn func(Event)) func() {
	m.mu.Lock()
	m.listenerID++
	id := m.listenerID
	if fn != nil {
		m.listeners[id] = fn
	}
	m.mu.Unlock()
	return func() { m.mu.Lock(); delete(m.listeners, id); m.mu.Unlock() }
}

func (m *Manager) List(ctx context.Context) ([]Automation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []Automation{}
	for _, r := range m.records {
		out = append(out, clone(r.Automation))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
func (m *Manager) Get(ctx context.Context, id string) (Automation, []Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Automation{}, nil, err
	}
	r := m.records[id]
	if r == nil {
		return Automation{}, nil, ErrNotFound
	}
	versions := []Version{}
	for _, v := range r.Versions {
		versions = append(versions, v.Version)
	}
	return clone(r.Automation), versions, nil
}
func (m *Manager) Runs(ctx context.Context, id string, limit int) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := m.records[id]
	if r == nil {
		return nil, ErrNotFound
	}
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100, len(r.Runs))
	out := clone(r.Runs[:limit])
	if out == nil {
		out = []Run{}
	}
	return out, nil
}

func normalize(s Spec) Spec {
	s = clone(s)
	if s.Schedule.At != nil {
		at := s.Schedule.At.UTC()
		s.Schedule.At = &at
	}
	if s.Schedule.Timezone == "" {
		s.Schedule.Timezone = "UTC"
	}
	if s.Edges == nil {
		s.Edges = []Edge{}
	}
	return s
}
func applySpec(a *Automation, s Spec) {
	a.Name = s.Name
	a.Description = s.Description
	a.Schedule = s.Schedule
	a.Steps = s.Steps
	a.Edges = s.Edges
}

// duplicateWindow bounds create deduplication.
const duplicateWindow = 10 * time.Minute

// repeatWindow catches a model re-issuing create with reworded text: same
// name, schedule and step types within this window count as the same automation.
const repeatWindow = 3 * time.Minute

func sameShape(a, b Spec) bool {
	sa, _ := json.Marshal(a.Schedule)
	sb, _ := json.Marshal(b.Schedule)
	if !strings.EqualFold(strings.TrimSpace(a.Name), strings.TrimSpace(b.Name)) || string(sa) != string(sb) || len(a.Steps) != len(b.Steps) || len(a.Edges) != len(b.Edges) {
		return false
	}
	for i := range a.Steps {
		if a.Steps[i].Type != b.Steps[i].Type {
			return false
		}
	}
	return true
}

func (m *Manager) Create(ctx context.Context, s Spec) (Automation, error) {
	if err := ValidateSpec(s); err != nil {
		return Automation{}, err
	}
	s = normalize(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Automation{}, err
	}
	now := m.now()
	// Models occasionally repeat a create call in one turn; an identical spec
	// created moments ago returns the existing automation instead of a copy.
	if want, err := json.Marshal(s); err == nil {
		for _, r := range m.records {
			if now.Sub(r.Automation.CreatedAt) > duplicateWindow || len(r.Versions) == 0 {
				continue
			}
			if got, err := json.Marshal(r.Versions[0].Spec); err == nil && string(got) == string(want) {
				return clone(r.Automation), nil
			}
			if now.Sub(r.Automation.CreatedAt) <= repeatWindow && sameShape(r.Versions[0].Spec, s) {
				return clone(r.Automation), nil
			}
		}
	}
	next, err := nextScheduled(s.Schedule, now, false)
	if err != nil {
		return Automation{}, err
	}
	a := Automation{ID: uuid.NewString(), Status: StatusActive, Version: 1, NextRunAt: next, CreatedAt: now, UpdatedAt: now}
	applySpec(&a, s)
	r := &record{Automation: a, Versions: []storedVersion{{Version: Version{Version: 1, CreatedAt: now}, Spec: s}}, Runs: []Run{}}
	if err = m.persistLocked(ctx, r); err != nil {
		return Automation{}, err
	}
	m.updatedLocked(a)
	return clone(a), nil
}
func (m *Manager) updateLocked(ctx context.Context, id string, s Spec, note string) (Automation, error) {
	r := m.records[id]
	if r == nil {
		return Automation{}, ErrNotFound
	}
	r = copyRecord(r)
	a := &r.Automation
	applySpec(a, s)
	a.Version++
	a.UpdatedAt = m.now()
	a.NextRunAt = nil
	if a.Status == StatusActive {
		next, err := nextScheduled(s.Schedule, m.now(), false)
		if err != nil {
			return Automation{}, err
		}
		a.NextRunAt = next
	}
	r.Versions = append(r.Versions, storedVersion{Version: Version{Version: a.Version, CreatedAt: a.UpdatedAt, Note: note}, Spec: s})
	if err := m.persistLocked(ctx, r); err != nil {
		return Automation{}, err
	}
	m.updatedLocked(*a)
	return clone(*a), nil
}
func (m *Manager) Update(ctx context.Context, id string, s Spec, note string) (Automation, error) {
	if err := ValidateSpec(s); err != nil {
		return Automation{}, err
	}
	s = normalize(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Automation{}, err
	}
	return m.updateLocked(ctx, id, s, note)
}
func (m *Manager) RestoreVersion(ctx context.Context, id string, version int) (Automation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Automation{}, err
	}
	r := m.records[id]
	if r == nil {
		return Automation{}, ErrNotFound
	}
	for _, v := range r.Versions {
		if v.Version.Version == version {
			return m.updateLocked(ctx, id, clone(v.Spec), fmt.Sprintf("restored v%d", version))
		}
	}
	return Automation{}, fmt.Errorf("version not found")
}
func (m *Manager) Pause(ctx context.Context, id, reason string) (Automation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Automation{}, err
	}
	r := m.records[id]
	if r == nil {
		return Automation{}, ErrNotFound
	}
	r = copyRecord(r)
	a := &r.Automation
	a.Status = StatusPaused
	if reason == "" {
		reason = "user"
	}
	a.PausedReason = reason
	a.NextRunAt = nil
	a.UpdatedAt = m.now()
	if err := m.persistLocked(ctx, r); err != nil {
		return Automation{}, err
	}
	if run, ok := m.running[id]; ok {
		run.cancel()
	}
	m.updatedLocked(*a)
	return clone(*a), nil
}
func (m *Manager) Resume(ctx context.Context, id string) (Automation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Automation{}, err
	}
	r := m.records[id]
	if r == nil {
		return Automation{}, ErrNotFound
	}
	r = copyRecord(r)
	a := &r.Automation
	consumed := a.LastRun != nil && a.LastRun.Version == a.Version
	next, err := nextScheduled(a.Schedule, m.now(), consumed)
	if err != nil {
		return Automation{}, err
	}
	a.Status = StatusActive
	a.PausedReason = ""
	a.ConsecutiveFailures = 0
	a.NextRunAt = next
	a.UpdatedAt = m.now()
	if err = m.persistLocked(ctx, r); err != nil {
		return Automation{}, err
	}
	m.updatedLocked(*a)
	return clone(*a), nil
}
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.records[id] == nil {
		return ErrNotFound
	}
	if m.cfg.DB != nil {
		if err := m.cfg.DB.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
			// The shared DB does not enable SQLite foreign-key enforcement.
			for _, table := range []string{"automation_runs", "automation_versions"} {
				if _, err := ex.Exec(ctx, "DELETE FROM "+table+" WHERE automation_id=?", id); err != nil {
					return err
				}
			}
			_, err := ex.Exec(ctx, `DELETE FROM automations WHERE id=?`, id)
			return err
		}); err != nil {
			return err
		}
	}
	if run, ok := m.running[id]; ok {
		run.cancel()
	}
	delete(m.records, id)
	m.emitLocked(Event{Kind: EventDeleted, ID: id})
	signal(m.wake)
	return nil
}

// Start starts the timer once, reconciling overdue schedules as one catch-up.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.ctx.Err() != nil {
		return fmt.Errorf("automation manager stopped")
	}
	if m.started {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.started = true
	m.detach = context.AfterFunc(ctx, m.cancel)
	overdue := map[string]bool{}
	for id, r := range m.records {
		a := r.Automation
		if a.Status == StatusActive && a.NextRunAt != nil && !a.NextRunAt.After(m.now()) {
			overdue[id] = true
		}
	}
	m.wg.Add(1)
	safego.Go("automation-scheduler", nil, func() { defer m.wg.Done(); m.scheduleLoop(overdue) })
	return nil
}
func (m *Manager) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.cancel()
	if m.detach != nil {
		m.detach()
	}
	for _, r := range m.running {
		r.cancel()
	}
	m.mu.Unlock()
	signal(m.eventWake)
	m.wg.Wait()
	m.mu.Lock()
	select {
	case <-m.eventStop:
	default:
		close(m.eventStop)
	}
	m.mu.Unlock()
	m.eventWG.Wait()
}
func (m *Manager) scheduleLoop(overdue map[string]bool) {
	for {
		m.mu.Lock()
		if m.ctx.Err() != nil {
			m.mu.Unlock()
			return
		}
		now := m.now()
		wait := 24 * time.Hour
		for id, r := range m.records {
			a := r.Automation
			if a.Status != StatusActive || a.NextRunAt == nil {
				continue
			}
			if _, ok := m.running[id]; ok {
				continue
			}
			if !a.NextRunAt.After(now) {
				trigger := TriggerSchedule
				if overdue[id] {
					trigger = TriggerCatchUp
				}
				if _, err := m.runLocked(id, trigger); err != nil {
					wait = min(wait, time.Second)
				}
				delete(overdue, id)
			} else {
				wait = min(wait, a.NextRunAt.Sub(now))
			}
		}
		m.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
