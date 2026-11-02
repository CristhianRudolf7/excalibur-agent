package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestTaskReminderRoutes(t *testing.T) {
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "api.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b := NewDaemonBackend(nil, nil, nil)
	bus := telemetry.NewBus(nil)
	queries := NewTaskQueries(db, bus)
	reminders := &agenda.ReminderService{Store: agenda.OpenReminderStore(db), DB: db}
	b.AttachServices(BackendServices{TaskQueries: queries, Reminders: reminders})
	now := time.Now().UTC()
	if err := queries.CreateTask(types.Task{ID: "tsk_api", Code: "T1", Scope: types.GeneralScope(), State: types.TaskOpen, Level: types.TaskN1, Request: types.RequestFile{Original: "API task"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := queries.SetTaskRun("tsk_api", "run_api", types.TaskInProgress); err != nil {
		t.Fatal(err)
	}
	events := bus.Replay(types.Cursor{})
	if len(events) != 2 || events[1].Type != api.EventTaskUpdated || events[1].TaskID == nil || *events[1].TaskID != "tsk_api" {
		t.Fatalf("task events: %+v", events)
	}
	var changed api.TaskUpdatedEvent
	if err := json.Unmarshal(events[1].Payload, &changed); err != nil || changed.Task.Status != "in_progress" {
		t.Fatalf("task payload: %+v %v", changed, err)
	}

	call := func(method string, p any) any {
		t.Helper()
		raw, _ := json.Marshal(p)
		v, err := b.Passthrough(ctx, method, raw)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	page := call(api.MethodTasksQuery, map[string]any{}).(api.TasksQueryResult)
	if len(page.Tasks) != 1 {
		t.Fatal(page)
	}
	detail := call(api.MethodTasksGet, map[string]any{"id": "tsk_api"}).(api.TaskDetail)
	if detail.Request != "API task" {
		t.Fatal(detail)
	}
	r := call(api.MethodRemindersCreate, map[string]any{"text": "remember", "due_at": now.Format(time.RFC3339)}).(api.Reminder)
	call(api.MethodRemindersUpdate, map[string]any{"id": r.ID, "status": "done"})
	call(api.MethodRemindersList, map[string]any{"include_done": true})
	call(api.MethodRemindersDelete, map[string]any{"id": r.ID})
	for _, tc := range []struct {
		method, params string
		code           int
	}{{api.MethodTasksGet, `{}`, api.CodeInvalidParams}, {api.MethodTasksGet, `{"id":"missing"}`, api.CodeUnknownTarget}, {api.MethodTasksQuery, `{"limit":201}`, api.CodeInvalidParams}, {api.MethodRemindersCreate, `{}`, api.CodeInvalidParams}} {
		_, err := b.Passthrough(ctx, tc.method, json.RawMessage(tc.params))
		ae, ok := err.(*api.Error)
		if !ok || ae.Code != tc.code {
			t.Fatalf("%s: %v", tc.method, err)
		}
	}
	// Exercise JSON-RPC dispatch as well as direct backend routing.
	server := gateway.New(gateway.DefaultConfig(), b, nil, nil)
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tasks.query","params":{}}`))
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "tsk_api") {
		t.Fatalf("rpc %d %s", w.Code, w.Body.String())
	}
}

func TestReminderToolRestartDeliversMasterNotice(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(root, "blobs")
	path := filepath.Join(root, "assistant.db")
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &agenda.ReminderService{Store: agenda.OpenReminderStore(db), DB: db}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Reminders: service})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	now := time.Now().UTC().Truncate(time.Second)
	args, _ := json.Marshal(map[string]any{"action": "create", "text": "Master reminder", "when": now.Format(time.RFC3339)})
	result, err := emb.Exec.Execute(ctx, "reminders", args, tools.ExecuteOptions{IdempotencyKey: "persistent-create"})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatalf("tool: %+v %v", result, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	retryService := &agenda.ReminderService{Store: agenda.OpenReminderStore(restarted), DB: restarted}
	retryEmb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Reminders: retryService})
	if err != nil {
		t.Fatal(err)
	}
	defer retryEmb.Close()
	retry, err := retryEmb.Exec.Execute(ctx, "reminders", args, tools.ExecuteOptions{IdempotencyKey: "persistent-create"})
	if err != nil || retry.Status != tools.StatusOK {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	var first, second api.Reminder
	if err := json.Unmarshal([]byte(result.Output), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(retry.Output), &second); err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("keyed creation changed on retry: %+v %+v", first, second)
	}

	reminders := agenda.OpenReminderStore(restarted)
	if got := reminders.ListReminders(); len(got) != 1 || got[0].Text != "Master reminder" {
		t.Fatalf("restart: %+v", got)
	}
	orch, _ := testOrchestrator()
	notices := master.NewNotifier(master.NotifierConfig{})
	launcher, err := NewOrchestratorLauncher(orch, notices)
	if err != nil {
		t.Fatal(err)
	}
	ag := agenda.NewAgenda(agenda.OpenAgendaStore(restarted), reminders, launcher)
	bus := telemetry.NewBus(nil)
	ag.OnReminderFired = func(r agenda.Reminder) {
		_, _ = bus.Publish(api.EventReminderFired, nil, nil, api.ReminderEvent{Reminder: agenda.ReminderView(&r)})
	}
	_, fired := ag.Tick(ctx, now.Add(time.Second))
	if len(fired) != 1 || notices.PendingCount() != 1 || bus.Len() != 1 {
		t.Fatalf("delivery: fired=%d notices=%d events=%d", len(fired), notices.PendingCount(), bus.Len())
	}
	ag.Tick(ctx, now.Add(time.Second))
	if notices.PendingCount() != 1 || bus.Len() != 1 {
		t.Fatal("duplicate reminder delivery")
	}
}
