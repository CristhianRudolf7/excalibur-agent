package agenda

import (
	"context"
	"testing"
	"time"
)

type recordTriggerLauncher struct {
	tasks    []string
	flows    []string
	notifies []string
	sessions []string
}

func (r *recordTriggerLauncher) StartTask(_ context.Context, target string, _ Event) error {
	r.tasks = append(r.tasks, target)
	return nil
}

func (r *recordTriggerLauncher) LaunchFlow(_ context.Context, target string, _ Event) error {
	r.flows = append(r.flows, target)
	return nil
}

func (r *recordTriggerLauncher) Notify(_ context.Context, target string, _ Event) error {
	r.notifies = append(r.notifies, target)
	return nil
}

func (r *recordTriggerLauncher) StartSession(_ context.Context, target string, _ Event) error {
	r.sessions = append(r.sessions, target)
	return nil
}

type recordLauncher struct {
	fires     []Fire
	reminders []Reminder
}

func (r *recordLauncher) LaunchFire(_ context.Context, f Fire) error {
	r.fires = append(r.fires, f)
	return nil
}

func (r *recordLauncher) LaunchReminder(_ context.Context, rem Reminder) error {
	r.reminders = append(r.reminders, rem)
	return nil
}

// TestTriggerPumpDispatch covers the Section 21.3 wiring row: every
// normalized source kind matches its rule and dispatches to the right
// launcher action, with duplicate event ids deduped.
func TestTriggerPumpDispatch(t *testing.T) {
	eng := NewTriggerEngine()
	launcher := &recordTriggerLauncher{}
	pump := NewTriggerPump(eng, launcher)
	rules := []TriggerRule{
		{ID: "mail-rule", Filter: TriggerFilter{Types: []string{"mail.received"}}, Action: ActionNotify, Target: "inbox", Enabled: true},
		{ID: "file-rule", Filter: TriggerFilter{Types: []string{"file.changed"}}, Action: ActionStartTask, Target: "reindex", Enabled: true},
		{ID: "cap-rule", Filter: TriggerFilter{Types: []string{"capability.completed"}}, Action: ActionLaunchFlow, Target: "post", Enabled: true},
		{ID: "cal-rule", Filter: TriggerFilter{Types: []string{"calendar.event"}}, Action: ActionStartSession, Target: "standup", Enabled: true},
	}
	for i := range rules {
		if err := eng.AddRule(&rules[i]); err != nil {
			t.Fatalf("add rule: %v", err)
		}
	}
	ctx := context.Background()
	now := time.Now().UTC()
	pump.Ingest(ctx, NormalizeMail("m1", "ana@example.com", "hello", now))
	pump.Ingest(ctx, NormalizeFileChange("/repo/main.go", "write", now))
	pump.Ingest(ctx, NormalizeCapabilityTask("browser", "task-1", "completed", now))
	pump.Ingest(ctx, NormalizeCalendar("c1", "standup", now.Add(time.Hour), now))
	pump.Ingest(ctx, NormalizeChannelMessage("w1", "telegram", "ana", "hi", now))
	pump.Ingest(ctx, NormalizeWebhook("h1", "deploy", map[string]any{"env": "prod"}, now))
	if len(launcher.notifies) != 1 || launcher.notifies[0] != "inbox" {
		t.Fatalf("notify not dispatched: %+v", launcher.notifies)
	}
	if len(launcher.tasks) != 1 || launcher.tasks[0] != "reindex" {
		t.Fatalf("start_task not dispatched: %+v", launcher.tasks)
	}
	if len(launcher.flows) != 1 || launcher.flows[0] != "post" {
		t.Fatalf("launch_workflow not dispatched: %+v", launcher.flows)
	}
	if len(launcher.sessions) != 1 || launcher.sessions[0] != "standup" {
		t.Fatalf("start_session not dispatched: %+v", launcher.sessions)
	}
	// Duplicate event ids dedupe by default window.
	before := len(launcher.notifies)
	pump.Ingest(ctx, NormalizeMail("m1", "ana@example.com", "hello", now))
	if len(launcher.notifies) != before {
		t.Fatal("duplicate event id fired twice")
	}
}

// TestCatchUpDefersInSafeMode covers the 17.3 boot row: with
// deferLaunch the agenda records due fires without launching, and
// without it the fires launch through the single firing path.
func TestCatchUpDefersInSafeMode(t *testing.T) {
	newAgenda := func() (*Agenda, *recordLauncher) {
		launcher := &recordLauncher{}
		a := NewAgenda(NewMemoryAgendaStore(), NewMemoryReminderStore(), launcher)
		return a, launcher
	}
	now := time.Now().UTC().Truncate(time.Second)
	downSince := now.Add(-time.Hour)
	mkSchedule := &Schedule{ID: "s1", Kind: ScheduleEvery, Timezone: "UTC", Interval: time.Minute,
		NextFireUTC: downSince.Add(time.Minute), Missed: MissedRunAll, Payload: "work"}

	a, launcher := newAgenda()
	if err := a.AddSchedule(mkSchedule); err != nil {
		t.Fatalf("add: %v", err)
	}
	fired, deferred, err := a.CatchUp(downSince, now, true)
	if err != nil {
		t.Fatalf("catchup: %v", err)
	}
	if fired != 0 {
		t.Fatalf("safe-mode catchup launched %d fires", fired)
	}
	if deferred == 0 {
		t.Fatal("safe-mode catchup recorded no pending fires")
	}
	if len(launcher.fires) != 0 {
		t.Fatal("launcher ran during safe mode")
	}

	a2, launcher2 := newAgenda()
	if err := a2.AddSchedule(mkSchedule); err != nil {
		t.Fatalf("add: %v", err)
	}
	fired2, deferred2, err := a2.CatchUp(downSince, now, false)
	if err != nil {
		t.Fatalf("catchup: %v", err)
	}
	if deferred2 != 0 {
		t.Fatalf("live catchup deferred %d fires", deferred2)
	}
	if fired2 == 0 || len(launcher2.fires) == 0 {
		t.Fatal("live catchup launched nothing")
	}
}
