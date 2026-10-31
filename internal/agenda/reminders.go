package agenda

import (
	"fmt"
	"sort"
	"time"
)

// ReminderState tracks a reminder (21.2).
type ReminderState string

// Reminder states.
const (
	ReminderPending   ReminderState = "pending"
	ReminderDelivered ReminderState = "delivered"
	ReminderSnoozed   ReminderState = "snoozed"
	ReminderDone      ReminderState = "done"
	ReminderCanceled  ReminderState = "canceled"
)

// RepeatPolicy controls redelivery while the user does not respond
// (21.2: "si no hay respuesta, se repiten según la política").
type RepeatPolicy struct {
	// MaxRepeats caps redeliveries after the first one.
	MaxRepeats int `json:"max_repeats"`
	// Interval between repeats.
	Interval time.Duration `json:"interval"`
}

// DefaultRepeatPolicy is the initial policy to calibrate (Appendix B).
var DefaultRepeatPolicy = RepeatPolicy{MaxRepeats: 3, Interval: 30 * time.Minute}

// Reminder is one reminder (21.2, Appendix A: reminders).
type Reminder struct {
	ID string `json:"id"`
	// Text is the reminder body.
	Text string `json:"text"`
	// At is the first delivery wall time; Recur carries an optional
	// cron expression for repetition.
	At       time.Time `json:"at_utc"`
	Recur    string    `json:"recur,omitempty"`
	Timezone string    `json:"timezone"`
	// Channel is the preferred delivery channel.
	Channel string `json:"channel"`
	// TaskID optionally links the reminder to a task.
	TaskID string        `json:"task_id,omitempty"`
	State  ReminderState `json:"state"`
	// SnoozedUntil pauses delivery until this UTC time.
	SnoozedUntil time.Time `json:"snoozed_until_utc,omitempty"`
	// Deliveries counts completed deliveries (first + repeats).
	Deliveries int `json:"deliveries"`
	// Acked is set when the user responds.
	Acked       bool         `json:"acked"`
	Repeat      RepeatPolicy `json:"repeat"`
	NextFireUTC time.Time    `json:"next_fire_utc"`
}

// Validate checks a reminder definition.
func (r *Reminder) Validate() error {
	if r == nil {
		return fmt.Errorf("agenda: nil reminder")
	}
	if r.Text == "" {
		return fmt.Errorf("agenda: reminder %q needs text", r.ID)
	}
	if r.At.IsZero() {
		return fmt.Errorf("agenda: reminder %q needs a time", r.ID)
	}
	if r.Recur != "" {
		if _, err := ParseCron(r.Recur); err != nil {
			return err
		}
	}
	if r.Timezone != "" {
		if err := ValidateZone(r.Timezone); err != nil {
			return err
		}
	}
	return nil
}

// Snooze postpones the reminder to the given UTC time (21.2: posponer).
func (r *Reminder) Snooze(until time.Time) error {
	if until.IsZero() {
		return fmt.Errorf("agenda: reminder %q: snooze needs a time", r.ID)
	}
	r.SnoozedUntil = until.UTC()
	r.State = ReminderSnoozed
	r.NextFireUTC = until.UTC()
	return nil
}

// Complete marks the reminder done (21.2: completar).
func (r *Reminder) Complete() {
	r.State = ReminderDone
	r.Acked = true
	r.NextFireUTC = time.Time{}
}

// Cancel marks the reminder canceled (21.2: borrar).
func (r *Reminder) Cancel() {
	r.State = ReminderCanceled
	r.NextFireUTC = time.Time{}
}

// recordDelivery advances the reminder after one delivery. It returns
// the next fire time, or zero when the reminder lifecycle is over.
// Unacknowledged reminders repeat per policy; recurring reminders
// follow their cron expression.
func (r *Reminder) recordDelivery(now time.Time) time.Time {
	r.Deliveries++
	r.State = ReminderDelivered
	if r.Recur != "" {
		loc, err := LoadZone(r.Timezone)
		if err == nil {
			if spec, err := ParseCron(r.Recur); err == nil {
				if next, err := spec.Next(now, loc); err == nil {
					r.NextFireUTC = next.UTC()
					return r.NextFireUTC
				}
			}
		}
	}
	if !r.Acked && r.Deliveries <= r.Repeat.MaxRepeats {
		interval := r.Repeat.Interval
		if interval <= 0 {
			interval = DefaultRepeatPolicy.Interval
		}
		r.NextFireUTC = now.Add(interval).UTC()
		return r.NextFireUTC
	}
	if r.Recur == "" {
		r.State = ReminderDone
		r.NextFireUTC = time.Time{}
	}
	return r.NextFireUTC
}

// ReminderStore is the persistence seam for reminders. MemoryReminderStore
// is the in-process implementation; the daemon can back it with SQLite.
type ReminderStore interface {
	SaveReminder(r *Reminder) error
	Reminder(id string) (*Reminder, bool)
	DeleteReminder(id string) error
	DueReminders(now time.Time) []*Reminder
	ListReminders() []*Reminder
}

// MemoryReminderStore keeps reminders in RAM, keyed by ID.
type MemoryReminderStore struct {
	items map[string]*Reminder
}

// NewMemoryReminderStore returns an empty reminder store.
func NewMemoryReminderStore() *MemoryReminderStore {
	return &MemoryReminderStore{items: map[string]*Reminder{}}
}

// SaveReminder creates or updates a reminder.
func (m *MemoryReminderStore) SaveReminder(r *Reminder) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if m.items == nil {
		m.items = map[string]*Reminder{}
	}
	cp := *r
	m.items[r.ID] = &cp
	return nil
}

// Reminder returns a copy of one reminder.
func (m *MemoryReminderStore) Reminder(id string) (*Reminder, bool) {
	r, ok := m.items[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// DeleteReminder removes a reminder.
func (m *MemoryReminderStore) DeleteReminder(id string) error {
	if _, ok := m.items[id]; !ok {
		return fmt.Errorf("agenda: unknown reminder %q", id)
	}
	delete(m.items, id)
	return nil
}

// DueReminders lists pending/snoozed reminders whose fire time passed.
func (m *MemoryReminderStore) DueReminders(now time.Time) []*Reminder {
	var out []*Reminder
	for _, r := range m.items {
		if r.State != ReminderPending && r.State != ReminderSnoozed && r.State != ReminderDelivered {
			continue
		}
		if r.NextFireUTC.IsZero() || r.NextFireUTC.After(now) {
			continue
		}
		if r.State == ReminderSnoozed && !r.SnoozedUntil.IsZero() && r.SnoozedUntil.After(now) {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextFireUTC.Before(out[j].NextFireUTC) })
	return out
}

// ListReminders returns all reminders ordered by next fire.
func (m *MemoryReminderStore) ListReminders() []*Reminder {
	out := make([]*Reminder, 0, len(m.items))
	for _, r := range m.items {
		cp := *r
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
