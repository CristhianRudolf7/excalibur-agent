package agenda

import (
	"fmt"
	"sort"
	"sync"
)

// AgendaStore persists schedules, delivered fire_ids and pending fires
// across restarts. MemoryAgendaStore is the in-process implementation;
// the daemon backs it with SQLite.
type AgendaStore interface {
	SaveSchedule(s *Schedule) error
	Schedule(id string) (*Schedule, bool)
	DeleteSchedule(id string) error
	ListSchedules() []*Schedule
	MarkDelivered(f Fire) error
	Delivered(fireID string) bool
	QueuePending(f Fire) error
	PendingFires() []Fire
	ClearPending(fireID string) error
}

// MemoryAgendaStore keeps agenda state in RAM.
type MemoryAgendaStore struct {
	mu        sync.Mutex
	schedules map[string]*Schedule
	delivered map[string]bool
	pending   map[string]Fire
}

// NewMemoryAgendaStore returns an empty store.
func NewMemoryAgendaStore() *MemoryAgendaStore {
	return &MemoryAgendaStore{
		schedules: map[string]*Schedule{},
		delivered: map[string]bool{},
		pending:   map[string]Fire{},
	}
}

// SaveSchedule creates or updates a schedule.
func (m *MemoryAgendaStore) SaveSchedule(s *Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.schedules[s.ID] = &cp
	return nil
}

// Schedule returns a copy of one schedule.
func (m *MemoryAgendaStore) Schedule(id string) (*Schedule, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.schedules[id]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// DeleteSchedule removes a schedule.
func (m *MemoryAgendaStore) DeleteSchedule(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[id]; !ok {
		return fmt.Errorf("agenda: unknown schedule %q", id)
	}
	delete(m.schedules, id)
	return nil
}

// ListSchedules returns all schedules ordered by next fire.
func (m *MemoryAgendaStore) ListSchedules() []*Schedule {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Schedule, 0, len(m.schedules))
	for _, s := range m.schedules {
		cp := *s
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextFireUTC.Equal(out[j].NextFireUTC) {
			return out[i].ID < out[j].ID
		}
		return out[i].NextFireUTC.Before(out[j].NextFireUTC)
	})
	return out
}

// MarkDelivered records a fire_id so a crash during delivery never
// duplicates it (17.4).
func (m *MemoryAgendaStore) MarkDelivered(f Fire) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delivered[f.FireID] = true
	delete(m.pending, f.FireID)
	return nil
}

// Delivered reports whether a fire_id already fired.
func (m *MemoryAgendaStore) Delivered(fireID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.delivered[fireID]
}

// QueuePending parks a fire while safe mode holds launches (17.3).
func (m *MemoryAgendaStore) QueuePending(f Fire) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[f.FireID] = f
	return nil
}

// PendingFires lists parked fires ordered by scheduled time.
func (m *MemoryAgendaStore) PendingFires() []Fire {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Fire, 0, len(m.pending))
	for _, f := range m.pending {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scheduled.Before(out[j].Scheduled) })
	return out
}

// ClearPending drops a parked fire.
func (m *MemoryAgendaStore) ClearPending(fireID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, fireID)
	return nil
}
