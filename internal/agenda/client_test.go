package agenda

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func reminderOp(t *testing.T, s *ReminderService, method string, p any) any {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Op(context.Background(), method, raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestPersistentReminderToolRestartAndFire(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rem.db")
	db := openAgendaDB(t, path)
	err := db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO principals(id,name,timezone,language) VALUES('local','Local','America/Lima','en')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &ReminderService{Store: OpenReminderStore(db), DB: db, Bus: telemetry.NewBus(nil)}
	reg := tools.NewMapRegistry()
	if err := tools.RegisterBuiltins(reg, nil); err != nil {
		t.Fatal(err)
	}
	original, _ := reg.Get("reminders")
	before, _ := json.Marshal(original.Definition())
	s.WireTool(reg)
	now := time.Now().UTC().Truncate(time.Second)
	args, _ := json.Marshal(map[string]any{"action": "create", "text": "persistent reminder", "when": now.Format(time.RFC3339), "repeat": "daily"})
	tool, _ := reg.Get("reminders")
	after, _ := json.Marshal(tool.Definition())
	if string(before) != string(after) {
		t.Fatal("tool definition changed")
	}
	result, err := tool.Invoke(ctx, tools.Invocation{Arguments: args})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatalf("create: %+v %v", result, err)
	}
	var created api.Reminder
	if err := json.Unmarshal([]byte(result.Output), &created); err != nil {
		t.Fatal(err)
	}
	if created.Timezone != "America/Lima" || created.CreatedAt.IsZero() {
		t.Fatal(created)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openAgendaDB(t, path)
	rs := OpenReminderStore(db2)
	launcher := &fakeLauncher{}
	ag := NewAgenda(OpenAgendaStore(db2), rs, launcher)
	bus := telemetry.NewBus(nil)
	ag.OnReminderFired = func(r Reminder) {
		_, _ = bus.Publish(api.EventReminderFired, nil, nil, api.ReminderEvent{Reminder: ReminderView(&r)})
	}
	restarted := &ReminderService{Store: rs, DB: db2, Agenda: ag, Bus: bus}
	listed := reminderOp(t, restarted, api.MethodRemindersList, map[string]any{}).(map[string]any)["reminders"].([]api.Reminder)
	if len(listed) != 1 || listed[0].ID != created.ID || !listed[0].CreatedAt.Equal(created.CreatedAt) {
		t.Fatal(listed)
	}
	ag.EnterSafeMode()
	_, fired := ag.Tick(ctx, now.Add(time.Second))
	if len(fired) != 0 {
		t.Fatal("safe mode delivered reminder")
	}
	ag.ExitSafeMode(ctx)
	_, fired = ag.Tick(ctx, now.Add(time.Second))
	if len(fired) != 1 || fired[0].LastFiredAt == nil || !ReminderView(&fired[0]).DueAt.After(now) || bus.Len() != 1 {
		t.Fatalf("fire: %+v %d", fired, bus.Len())
	}
	events := bus.Replay(types.Cursor{})
	if events[0].Type != api.EventReminderFired {
		t.Fatal(events)
	}
	_, fired = ag.Tick(ctx, now.Add(time.Second))
	if len(fired) != 0 {
		t.Fatal("duplicate delivery")
	}
	saved, ok := rs.Reminder(created.ID)
	if !ok || saved.Deliveries != 1 || saved.LastFiredAt == nil {
		t.Fatal(saved)
	}
}
func TestReminderCRUDValidationSnoozeAndEvents(t *testing.T) {
	db := openAgendaDB(t, filepath.Join(t.TempDir(), "crud.db"))
	rs := OpenReminderStore(db)
	ag := NewAgenda(nil, rs, &fakeLauncher{})
	bus := telemetry.NewBus(nil)
	s := &ReminderService{Store: rs, DB: db, Agenda: ag, Bus: bus}
	now := time.Now().UTC()
	r := reminderOp(t, s, api.MethodRemindersCreate, map[string]any{"text": "call", "due_at": now.Format(time.RFC3339)}).(api.Reminder)
	until := now.Add(time.Hour).Truncate(time.Second)
	snoozed := reminderOp(t, s, api.MethodRemindersUpdate, map[string]any{"id": r.ID, "status": "snoozed", "snooze_until": until.Format(time.RFC3339)}).(api.Reminder)
	if snoozed.Status != "snoozed" || !snoozed.DueAt.Equal(until) {
		t.Fatal(snoozed)
	}
	_, due := ag.Tick(context.Background(), now.Add(time.Minute))
	if len(due) != 0 {
		t.Fatal("snooze fired early")
	}
	_, due = ag.Tick(context.Background(), until)
	if len(due) != 1 {
		t.Fatal("snooze did not fire")
	}
	edited := reminderOp(t, s, api.MethodRemindersUpdate, map[string]any{"id": r.ID, "text": "edited", "repeat": "weekly"}).(api.Reminder)
	if edited.Text != "edited" || edited.Repeat == "" {
		t.Fatal(edited)
	}
	done := reminderOp(t, s, api.MethodRemindersUpdate, map[string]any{"id": r.ID, "status": "done"}).(api.Reminder)
	if done.Status != "done" {
		t.Fatal(done)
	}
	if list := reminderOp(t, s, api.MethodRemindersList, map[string]any{}).(map[string]any)["reminders"].([]api.Reminder); len(list) != 0 {
		t.Fatal(list)
	}
	if list := reminderOp(t, s, api.MethodRemindersList, map[string]any{"include_done": true}).(map[string]any)["reminders"].([]api.Reminder); len(list) != 1 {
		t.Fatal(list)
	}
	reminderOp(t, s, api.MethodRemindersUpdate, map[string]any{"id": r.ID, "status": "cancelled"})
	reminderOp(t, s, api.MethodRemindersDelete, map[string]any{"id": r.ID})
	if _, ok := rs.Reminder(r.ID); ok {
		t.Fatal("delete did not persist")
	}
	if bus.Len() != 6 {
		t.Fatalf("updated events %d", bus.Len())
	}
	for _, ev := range bus.Replay(types.Cursor{}) {
		if ev.Type != api.EventReminderUpdated {
			t.Fatal(ev)
		}
	}
	for _, tc := range []struct{ method, raw string }{
		{api.MethodRemindersCreate, `{}`}, {api.MethodRemindersCreate, `{"text":" ","due_at":"2026-01-01T00:00:00Z"}`}, {api.MethodRemindersCreate, `{"text":"x","due_at":"bad"}`}, {api.MethodRemindersCreate, `{"text":"x","due_at":"2026-01-01T00:00:00Z","timezone":"bad"}`}, {api.MethodRemindersCreate, `{"text":"x","due_at":"2026-01-01T00:00:00Z","repeat":"bad"}`}, {api.MethodRemindersUpdate, `{"id":"missing","status":"done"}`}, {api.MethodRemindersDelete, `{}`}, {api.MethodRemindersList, `null`}, {api.MethodRemindersList, `[]`},
	} {
		if _, err := s.Op(context.Background(), tc.method, json.RawMessage(tc.raw)); err == nil {
			t.Fatalf("accepted %s", tc.raw)
		}
	}
	r = reminderOp(t, s, api.MethodRemindersCreate, map[string]any{"text": "valid", "due_at": now.Format(time.RFC3339)}).(api.Reminder)
	for _, p := range []map[string]any{{"id": r.ID, "status": "invalid"}, {"id": r.ID, "status": "snoozed"}, {"id": r.ID, "status": "snoozed", "snooze_until": now.Add(-time.Hour).Format(time.RFC3339)}, {"id": r.ID, "text": ""}} {
		raw, _ := json.Marshal(p)
		if _, err := s.Op(context.Background(), api.MethodRemindersUpdate, raw); err == nil {
			t.Fatal(p)
		}
	}
}
