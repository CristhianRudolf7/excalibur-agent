// SQL agenda persistence: schedules, reminders, triggers,
// workflows and sessions over a real temporary SQLite database, plus
// crash/restart survival (Sections 17.3, 17.4, 17.6, 21.10).
package agenda

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func openAgendaDB(t *testing.T, path string) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLAgendaStoreRoundTrip(t *testing.T) {
	db := openAgendaDB(t, filepath.Join(t.TempDir(), "agenda.db"))
	s := OpenAgendaStore(db)

	now := time.Now().UTC().Truncate(time.Second)
	sched := &Schedule{ID: "sch_1", Kind: ScheduleEvery, Timezone: "UTC",
		Interval: time.Hour, NextFireUTC: now.Add(time.Hour), Missed: MissedRunOnceNow,
		Payload: "hello", ScopeID: "general"}
	if err := s.SaveSchedule(sched); err != nil {
		t.Fatal(err)
	}
	back, ok := s.Schedule("sch_1")
	if !ok || back.Interval != time.Hour || back.Payload != "hello" || back.Missed != MissedRunOnceNow {
		t.Fatalf("schedule round trip: %+v %v", back, ok)
	}
	if !back.NextFireUTC.Equal(now.Add(time.Hour)) {
		t.Fatalf("next fire: %v", back.NextFireUTC)
	}
	fire := Fire{FireID: "fire_1", ScheduleID: "sch_1", Scheduled: now, FiredAt: now, Payload: "hello"}
	if s.Delivered("fire_1") {
		t.Fatal("fire delivered before marking")
	}
	if err := s.MarkDelivered(fire); err != nil {
		t.Fatal(err)
	}
	if !s.Delivered("fire_1") {
		t.Fatal("fire not delivered after marking")
	}
	// Marking twice is idempotent, never a duplicate fire.
	if err := s.MarkDelivered(fire); err != nil {
		t.Fatal(err)
	}
	pending := Fire{FireID: "fire_p", ScheduleID: "sch_1", Scheduled: now, FiredAt: now}
	if err := s.QueuePending(pending); err != nil {
		t.Fatal(err)
	}
	if fires := s.PendingFires(); len(fires) != 1 || fires[0].FireID != "fire_p" {
		t.Fatalf("pending: %+v", fires)
	}
	if err := s.ClearPending("fire_p"); err != nil {
		t.Fatal(err)
	}
	if fires := s.PendingFires(); len(fires) != 0 {
		t.Fatalf("pending after clear: %+v", fires)
	}
	if err := s.DeleteSchedule("sch_1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Schedule("sch_1"); ok {
		t.Fatal("schedule survived delete")
	}
	if err := s.DeleteSchedule("sch_nope"); err == nil {
		t.Fatal("expected unknown schedule error")
	}
}

func TestSQLAgendaSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agenda-crash.db")
	db := openAgendaDB(t, path)
	s := OpenAgendaStore(db)
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.SaveSchedule(&Schedule{ID: "sch_c", Kind: ScheduleEvery, Timezone: "UTC",
		Interval: time.Hour, NextFireUTC: now.Add(time.Hour), Missed: MissedRunAll}); err != nil {
		t.Fatal(err)
	}
	fire := Fire{FireID: "fire_c", ScheduleID: "sch_c", Scheduled: now, FiredAt: now, Late: true}
	if err := s.MarkDelivered(fire); err != nil {
		t.Fatal(err)
	}
	if err := s.QueuePending(Fire{FireID: "fire_q", ScheduleID: "sch_c", Scheduled: now, FiredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openAgendaDB(t, path)
	s2 := OpenAgendaStore(db2)
	if _, ok := s2.Schedule("sch_c"); !ok {
		t.Fatal("schedule lost across reopen")
	}
	if !s2.Delivered("fire_c") {
		t.Fatal("delivered mark lost across reopen")
	}
	if fires := s2.PendingFires(); len(fires) != 1 {
		t.Fatalf("pending lost across reopen: %+v", fires)
	}
	// Recovery continues on the reopened store: reconciling the
	// downtime marks the fire without duplicating it.
	if err := s2.MarkDelivered(Fire{FireID: "fire_c", ScheduleID: "sch_c", Scheduled: now, FiredAt: now}); err != nil {
		t.Fatal(err)
	}
	if fires := s2.PendingFires(); len(fires) != 1 {
		t.Fatalf("pending changed by redelivery: %+v", fires)
	}
}

func TestSQLReminderStore(t *testing.T) {
	db := openAgendaDB(t, filepath.Join(t.TempDir(), "rem.db"))
	s := OpenReminderStore(db)
	now := time.Now().UTC().Truncate(time.Second)

	r := &Reminder{ID: "rem_1", Text: "drink water", At: now.Add(-time.Minute),
		Timezone: "UTC", Channel: "test", State: ReminderPending,
		NextFireUTC: now.Add(-time.Minute), Repeat: DefaultRepeatPolicy}
	if err := s.SaveReminder(r); err != nil {
		t.Fatal(err)
	}
	if due := s.DueReminders(now); len(due) != 1 {
		t.Fatalf("due: %+v", due)
	}
	back, ok := s.Reminder("rem_1")
	if !ok {
		t.Fatal("reminder missing")
	}
	if err := back.Snooze(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReminder(back); err != nil {
		t.Fatal(err)
	}
	if due := s.DueReminders(now); len(due) != 0 {
		t.Fatalf("due after snooze: %+v", due)
	}
	back.Complete()
	if err := s.SaveReminder(back); err != nil {
		t.Fatal(err)
	}
	if rems := s.ListReminders(); len(rems) != 1 || rems[0].State != ReminderDone {
		t.Fatalf("list: %+v", rems)
	}
	if err := s.DeleteReminder("rem_1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Reminder("rem_1"); ok {
		t.Fatal("reminder survived delete")
	}
}

func TestSQLTriggerStore(t *testing.T) {
	db := openAgendaDB(t, filepath.Join(t.TempDir(), "trig.db"))
	s := OpenTriggerStore(db)
	now := time.Now().UTC().Truncate(time.Second)

	e := NewTriggerEngine()
	e.AttachTriggerStore(s)
	rule := &TriggerRule{ID: "rule_1", Filter: TriggerFilter{Types: []string{"mail"}},
		Action: ActionNotify, Target: "user", MaxPerHour: 1, DedupWindow: time.Hour, Enabled: true}
	if err := e.AddRule(rule); err != nil {
		t.Fatal(err)
	}
	ev := Event{ID: "ev_1", Type: "mail", Source: "inbox", At: now}
	if hits := e.Ingest(ev, now); len(hits) != 1 {
		t.Fatalf("hits: %+v", hits)
	}
	// Same event inside the dedup window: no hit, even restored.
	if hits := e.Ingest(ev, now.Add(time.Minute)); len(hits) != 0 {
		t.Fatalf("dedup failed: %+v", hits)
	}
	if !s.SeenSince("rule_1", "ev_1", time.Hour, now.Add(time.Minute)) {
		t.Fatal("hit not persisted")
	}
	if n := s.HitsSince("rule_1", now.Add(-time.Hour)); n != 1 {
		t.Fatalf("hits since: %d", n)
	}
	// Over the rate limit: no hit.
	if hits := e.Ingest(Event{ID: "ev_2", Type: "mail", Source: "inbox", At: now}, now); len(hits) != 0 {
		t.Fatalf("rate limit failed: %+v", hits)
	}

	// Restart: rules, dedup marks and the rate count come back.
	e2 := NewTriggerEngine()
	e2.AttachTriggerStore(s)
	e2.RestoreTriggers(now.Add(2 * time.Minute))
	if len(e2.Rules()) != 1 {
		t.Fatalf("rules after restore: %+v", e2.Rules())
	}
	if hits := e2.Ingest(ev, now.Add(2*time.Minute)); len(hits) != 0 {
		t.Fatalf("dedup lost across restart: %+v", hits)
	}
	if hits := e2.Ingest(Event{ID: "ev_3", Type: "mail", Source: "inbox"}, now.Add(2*time.Minute)); len(hits) != 0 {
		t.Fatalf("rate limit lost across restart: %+v", hits)
	}
	if err := e2.RemoveRule("rule_1"); err != nil {
		t.Fatal(err)
	}
	e3 := NewTriggerEngine()
	e3.AttachTriggerStore(s)
	e3.RestoreTriggers(now)
	if len(e3.Rules()) != 0 {
		t.Fatalf("removed rule came back: %+v", e3.Rules())
	}
}

func TestSQLWorkflowStore(t *testing.T) {
	db := openAgendaDB(t, filepath.Join(t.TempDir(), "wf.db"))
	s := OpenWorkflowStore(db)

	def := &WorkflowDef{ID: "wf_1", Name: "ship", Version: 1,
		Nodes: []Node{{ID: "n1", Type: "tool", Tool: "exec"}, {ID: "n2", Type: "confirm", ConfirmPrompt: "ok?"}},
		Edges: []Edge{{From: "n1", To: "n2"}}}
	if err := s.SaveDef(def); err != nil {
		t.Fatal(err)
	}
	def2 := *def
	def2.Version = 2
	def2.Nodes = append(append([]Node(nil), def.Nodes...), Node{ID: "n3", Type: "notify"})
	if err := s.SaveDef(&def2); err != nil {
		t.Fatal(err)
	}
	v1, err := s.GetDef("wf_1", 1)
	if err != nil || len(v1.Nodes) != 2 {
		t.Fatalf("version 1: %+v %v", v1, err)
	}
	latest, err := s.LatestDef("wf_1")
	if err != nil || latest.Version != 2 || len(latest.Nodes) != 3 {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	in := &WorkflowInstance{ID: "wfr_1", WorkflowID: "wf_1", VersionPinned: 1,
		State: InstanceRunning, Inputs: map[string]any{"x": 1.0}, Completed: []string{"n1"}}
	if err := s.SaveInstance(in); err != nil {
		t.Fatal(err)
	}
	back, err := s.GetInstance("wfr_1")
	if err != nil || back.VersionPinned != 1 || len(back.Completed) != 1 {
		t.Fatalf("instance: %+v %v", back, err)
	}
	in.State = InstanceWaitingConfirm
	in.PendingNode = "n2"
	if err := s.SaveInstance(in); err != nil {
		t.Fatal(err)
	}
	// Publishing v3 leaves the pinned instance on v1 (21.4).
	def3 := *def
	def3.Version = 3
	if err := s.SaveDef(&def3); err != nil {
		t.Fatal(err)
	}
	back, err = s.GetInstance("wfr_1")
	if err != nil || back.VersionPinned != 1 || back.State != InstanceWaitingConfirm || back.PendingNode != "n2" {
		t.Fatalf("instance after republish: %+v %v", back, err)
	}
	if list := s.ListInstances("wf_1"); len(list) != 1 {
		t.Fatalf("instances: %+v", list)
	}
	if _, err := s.GetDef("wf_nope", 1); err == nil {
		t.Fatal("expected unknown workflow error")
	}
}

func TestSQLSessionStore(t *testing.T) {
	db := openAgendaDB(t, filepath.Join(t.TempDir(), "sess.db"))
	s := OpenSessionStore(db)
	now := time.Now().UTC().Truncate(time.Second)

	sn := &Session{ID: "ses_1", Initiative: "big-refactor", Milestone: "m1",
		State: SessionRunning, BudgetUSD: 5, StartupCmds: []string{"go build ./..."}, Heartbeat: now}
	if err := s.Save(sn); err != nil {
		t.Fatal(err)
	}
	sn.Beat(now.Add(time.Minute))
	sn.Handoff = "half done"
	if err := s.Save(sn); err != nil {
		t.Fatal(err)
	}
	back, err := s.Get("ses_1")
	if err != nil || back.Handoff != "half done" || len(back.StartupCmds) != 1 {
		t.Fatalf("session: %+v %v", back, err)
	}
	if list := s.ListByInitiative("big-refactor"); len(list) != 1 {
		t.Fatalf("sessions: %+v", list)
	}
	if err := s.Delete("ses_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("ses_1"); err == nil {
		t.Fatal("expected unknown session error")
	}
}

// openSQLTestDB opens a migrated database and runs EnsureSQLSchema,
// proving the daemon-spelling bootstrap is a safe no-op on migrated
// databases (wave 4 merge compatibility).
func openSQLTestDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "agenda.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSQLSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestSQLAgendaStoreCompatAliases covers the daemon-spelling
// constructors over the migration-aligned tables: schedule CRUD,
// fire_id dedup across restarts (17.4) and safe-mode pending fires.
func TestSQLAgendaStoreCompatAliases(t *testing.T) {
	db := openSQLTestDB(t)
	s := NewSQLAgendaStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	sch := &Schedule{ID: "s1", Kind: ScheduleAt, Timezone: "UTC", At: now.Add(time.Hour), NextFireUTC: now.Add(time.Hour), Missed: MissedRunOnceNow}
	if err := s.SaveSchedule(sch); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := s.Schedule("s1")
	if !ok || got.ID != "s1" {
		t.Fatalf("schedule not found: %v %v", got, ok)
	}
	if len(s.ListSchedules()) != 1 {
		t.Fatalf("want 1 schedule, have %d", len(s.ListSchedules()))
	}
	f := Fire{FireID: "fire-1", ScheduleID: "s1", Scheduled: now}
	if s.Delivered("fire-1") {
		t.Fatal("fire reported delivered before firing")
	}
	if err := s.QueuePending(f); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if len(s.PendingFires()) != 1 {
		t.Fatalf("want 1 pending fire, have %d", len(s.PendingFires()))
	}
	if err := s.MarkDelivered(f); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if !s.Delivered("fire-1") {
		t.Fatal("fire not marked delivered (17.4 idempotency)")
	}
	if len(s.PendingFires()) != 0 {
		t.Fatal("delivered fire still pending")
	}
	if err := s.DeleteSchedule("s1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteSchedule("s1"); err == nil {
		t.Fatal("double delete should fail")
	}
}

// TestSQLReminderStoreRoundTrip covers reminder persistence through
// the daemon-spelling constructor.
func TestSQLReminderStoreRoundTrip(t *testing.T) {
	db := openSQLTestDB(t)
	s := NewSQLReminderStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	r := &Reminder{ID: "r1", Text: "call Ana", At: now.Add(-time.Minute), Timezone: "UTC", Channel: "test", State: ReminderPending, NextFireUTC: now.Add(-time.Minute)}
	if err := s.SaveReminder(r); err != nil {
		t.Fatalf("save: %v", err)
	}
	if due := s.DueReminders(now); len(due) != 1 {
		t.Fatalf("want 1 due reminder, have %d", len(due))
	}
	if future := s.DueReminders(now.Add(-time.Hour)); len(future) != 0 {
		t.Fatalf("nothing due an hour ago, have %d", len(future))
	}
	if err := s.DeleteReminder("r1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := s.Reminder("r1"); ok {
		t.Fatal("deleted reminder still present")
	}
}

// TestSQLTriggerStoreRoundTrip covers trigger rule persistence (21.3)
// through the daemon-spelling Save/Load/Delete methods.
func TestSQLTriggerStoreRoundTrip(t *testing.T) {
	db := openSQLTestDB(t)
	s := NewSQLTriggerStore(db)
	rule := &TriggerRule{ID: "t1", Filter: TriggerFilter{Types: []string{"mail.received"}}, Action: "notify", Target: "inbox", Enabled: true}
	if err := s.Save(rule); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded := s.Load()
	if len(loaded) != 1 || loaded[0].ID != "t1" {
		t.Fatalf("unexpected load: %+v", loaded)
	}
	if err := s.Delete("t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(s.Load()) != 0 {
		t.Fatal("deleted rule still present")
	}
}
