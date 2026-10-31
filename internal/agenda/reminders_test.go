package agenda

import (
	"testing"
	"time"
)

// 21.10 row 2: reminders repeat while unanswered.

func TestReminderLifecycle(t *testing.T) {
	m := NewMemoryReminderStore()
	now := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	r := &Reminder{ID: "r1", Text: "standup", At: now, Timezone: "UTC",
		NextFireUTC: now, State: ReminderPending, Repeat: DefaultRepeatPolicy}
	if err := m.SaveReminder(r); err != nil {
		t.Fatal(err)
	}
	if len(m.DueReminders(now)) != 1 {
		t.Fatal("reminder due at its time")
	}
	saved, _ := m.Reminder("r1")
	saved.Snooze(now.Add(time.Hour))
	if err := m.SaveReminder(saved); err != nil {
		t.Fatal(err)
	}
	if len(m.DueReminders(now.Add(30*time.Minute))) != 0 {
		t.Fatal("snoozed reminder must not be due")
	}
	if len(m.DueReminders(now.Add(time.Hour))) != 1 {
		t.Fatal("snoozed reminder due after snooze")
	}
	saved.Complete()
	if saved.State != ReminderDone {
		t.Fatal("complete marks done")
	}
}

func TestReminderRepeatsWhileUnanswered(t *testing.T) {
	now := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	r := &Reminder{ID: "r2", Text: "ping", At: now, Timezone: "UTC",
		NextFireUTC: now, State: ReminderPending,
		Repeat: RepeatPolicy{MaxRepeats: 2, Interval: 10 * time.Minute}}
	next := r.recordDelivery(now)
	if next.IsZero() || !next.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("unacknowledged reminder repeats per policy: %v", next)
	}
	if r.Deliveries != 1 || r.State != ReminderDelivered {
		t.Fatalf("delivery counted: %+v", r)
	}
	r.Acked = true
	if next := r.recordDelivery(now); !next.IsZero() {
		t.Fatalf("acknowledged recurring-free reminder ends: %v", next)
	}
}

func TestReminderRecur(t *testing.T) {
	// The day before the spring-forward: after 09:00 EST the next
	// 09:00 local is 09:00 EDT, i.e. 13:00 UTC, not 14:00.
	now := time.Date(2026, time.March, 7, 15, 0, 0, 0, time.UTC)
	r := &Reminder{ID: "r3", Text: "daily", At: now, Timezone: "America/New_York",
		Recur: "0 9 * * *", NextFireUTC: now, State: ReminderPending, Repeat: DefaultRepeatPolicy}
	next := r.recordDelivery(now)
	want := time.Date(2026, time.March, 8, 13, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("want %v, have %v", want, next)
	}
}

func TestReminderDelete(t *testing.T) {
	m := NewMemoryReminderStore()
	now := time.Now().UTC()
	if err := m.SaveReminder(&Reminder{ID: "x", Text: "t", At: now, NextFireUTC: now}); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteReminder("x"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteReminder("x"); err == nil {
		t.Fatal("double delete must fail")
	}
}
