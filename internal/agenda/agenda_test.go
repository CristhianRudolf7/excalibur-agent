package agenda

import (
	"context"
	"testing"
	"time"
)

type fakeLauncher struct {
	fires []Fire
	rems  []Reminder
	err   error
}

func (f *fakeLauncher) LaunchFire(_ context.Context, fire Fire) error {
	if f.err != nil {
		return f.err
	}
	f.fires = append(f.fires, fire)
	return nil
}

func (f *fakeLauncher) LaunchReminder(_ context.Context, r Reminder) error {
	f.rems = append(f.rems, r)
	return nil
}

func TestAgendaTickFires(t *testing.T) {
	fl := &fakeLauncher{}
	a := NewAgenda(nil, nil, fl)
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	s := &Schedule{ID: "tick", Kind: ScheduleEvery, Interval: time.Hour, Missed: MissedSkip, Timezone: "UTC", NextFireUTC: now}
	if err := a.AddSchedule(s); err != nil {
		t.Fatal(err)
	}
	fired, _ := a.Tick(context.Background(), now)
	if len(fired) != 1 || len(fl.fires) != 1 {
		t.Fatalf("want one fire, have %d/%d", len(fired), len(fl.fires))
	}
	if a.store.Delivered(fired[0].FireID) != true {
		t.Fatal("fire_id must be recorded delivered")
	}
	// Second tick at the same instant fires nothing (advanced past).
	fired, _ = a.Tick(context.Background(), now)
	if len(fired) != 0 {
		t.Fatalf("no double fire, have %d", len(fired))
	}
}

func TestAgendaSafeModeParks(t *testing.T) {
	fl := &fakeLauncher{}
	a := NewAgenda(nil, nil, fl)
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	s := &Schedule{ID: "sm", Kind: ScheduleEvery, Interval: time.Hour, Missed: MissedSkip, Timezone: "UTC", NextFireUTC: now}
	if err := a.AddSchedule(s); err != nil {
		t.Fatal(err)
	}
	a.EnterSafeMode()
	fired, _ := a.Tick(context.Background(), now)
	if len(fired) != 0 || len(fl.fires) != 0 {
		t.Fatal("safe mode must register but not launch")
	}
	if len(a.store.PendingFires()) != 1 {
		t.Fatalf("want 1 pending fire, have %d", len(a.store.PendingFires()))
	}
	if errs := a.ExitSafeMode(context.Background()); len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(fl.fires) != 1 || len(a.store.PendingFires()) != 0 {
		t.Fatal("exit must flush parked fires in order")
	}
}

func TestAgendaBootReconcile(t *testing.T) {
	fl := &fakeLauncher{}
	a := NewAgenda(nil, nil, fl)
	s := &Schedule{ID: "boot", Kind: ScheduleEvery, Interval: time.Hour, Missed: MissedRunAll, Timezone: "UTC"}
	s.NextFireUTC = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := a.AddSchedule(s); err != nil {
		t.Fatal(err)
	}
	down := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := down.Add(2 * time.Hour)
	delivered, err := a.ReconcileBoot(context.Background(), down, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 || len(fl.fires) != 2 {
		t.Fatalf("boot must deliver missed fires, have %d/%d", len(delivered), len(fl.fires))
	}
}

func TestAgendaAfterComplete(t *testing.T) {
	fl := &fakeLauncher{}
	a := NewAgenda(nil, nil, fl)
	s := &Schedule{ID: "dep", Kind: ScheduleAfter, AfterID: "tsk_1", Missed: MissedRunOnceNow}
	if err := a.AddSchedule(s); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if got := a.AfterComplete(context.Background(), "other", now); len(got) != 0 {
		t.Fatal("unrelated completion must not fire")
	}
	if got := a.AfterComplete(context.Background(), "tsk_1", now); len(got) != 1 {
		t.Fatalf("dependency completion must fire once, have %d", len(got))
	}
	if got := a.AfterComplete(context.Background(), "tsk_1", now); len(got) != 0 {
		t.Fatal("after fires exactly once")
	}
}

func TestAgendaTimerLoop(t *testing.T) {
	fl := &fakeLauncher{}
	a := NewAgenda(nil, nil, fl)
	a.now = func() time.Time { return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC) }
	a.Start()
	a.Stop()
	// Second stop is a no-op; restart works.
	a.Start()
	a.Stop()
}
