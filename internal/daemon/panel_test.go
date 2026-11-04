package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf/testpdf"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	orchfake "github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
	"github.com/coder/websocket"
)

func panelRPC(t *testing.T, gate *gateway.Server, method string, params any) api.Response {
	t.Helper()
	req, err := api.NewRequest("panel", method, params)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	gate.ServeHTTP(w, httptest.NewRequest("POST", "/rpc", bytes.NewReader(raw)))
	var response api.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("%s: %s", err, w.Body.String())
	}
	return response
}

func TestFilesOpenWorkspaceAndKinds(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	reg := workspace.NewRegistry()
	project, err := reg.Register(workspace.Project{Name: "test", Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	blobs := gateway.NewMemoryBlobs()
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Projects: reg, FileBlobs: blobs})
	gate := gateway.New(gateway.Config{AllowRemote: true}, b, blobs, nil)
	for _, tc := range []struct {
		name string
		data []byte
		kind string
	}{
		{"empty.txt", nil, "text"}, {"code.go", []byte("package main\n"), "text"}, {"bad.txt", []byte{255, 0}, "binary"}, {"embedded-nul.txt", []byte("a\x00b"), "binary"}, {"large.txt", bytes.Repeat([]byte("x"), (2<<20)+1), "binary"}, {"image.png", []byte{137, 80, 78, 71, 13, 10, 26, 10}, "image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, tc.name), tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			response := panelRPC(t, gate, api.MethodFilesOpen, api.FilesOpenParams{Path: tc.name, ProjectID: string(project.ID)})
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			var got api.FilesOpenResult
			if err := json.Unmarshal(response.Result, &got); err != nil {
				t.Fatal(err)
			}
			wantPath, err := filepath.EvalSymlinks(filepath.Join(root, tc.name))
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tc.kind || got.Size != int64(len(tc.data)) || got.Path != wantPath {
				t.Fatalf("%+v", got)
			}
			if tc.kind == "text" && (got.Content == nil || *got.Content != string(tc.data)) {
				t.Fatalf("content: %+v", got)
			}
			if tc.kind == "image" {
				w := httptest.NewRecorder()
				gate.ServeHTTP(w, httptest.NewRequest("GET", got.BlobURL, nil))
				if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), tc.data) {
					t.Fatalf("blob %d", w.Code)
				}
			}
		})
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{secret, "../" + filepath.Base(outside) + "/secret.txt", "escape"} {
		response := panelRPC(t, gate, api.MethodFilesOpen, api.FilesOpenParams{Path: path})
		if response.Error == nil || response.Error.Message != "outside workspace" {
			t.Fatalf("%s: %+v", path, response)
		}
	}
	for _, params := range []any{map[string]any{}, map[string]any{"path": "code.go", "extra": true}, nil} {
		if response := panelRPC(t, gate, api.MethodFilesOpen, params); response.Error == nil {
			t.Fatalf("accepted %+v", params)
		}
	}
	b.AttachServices(BackendServices{WorkspaceRoots: func(context.Context) []string { return []string{root} }})
	if response := panelRPC(t, gate, api.MethodFilesOpen, api.FilesOpenParams{Path: "code.go"}); response.Error != nil {
		t.Fatal(response.Error)
	}
}

func TestFilesOpenLibraryReadyDedupeAndCells(t *testing.T) {
	f := newLibraryE2E(t, nil, true)
	root := t.TempDir()
	reg := workspace.NewRegistry()
	if _, err := reg.Register(workspace.Project{Name: "files", Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	blobs := gateway.StoreBlobs{Store: f.db.Blob()}
	b := NewDaemonBackend(nil, nil, f.bus)
	b.AttachServices(BackendServices{Projects: reg, FileBlobs: blobs, Library: LibraryRoutesFor(f.mem, f.services.Library, []string{"general"})})
	gate := gateway.New(gateway.Config{AllowRemote: true}, b, blobs, f.bus)
	// The same native PDF worker exercised by library.ingest handles workspace files.
	pdfData := testpdf.Build([]testpdf.Page{{Lines: []testpdf.Line{{Text: "Workspace PDF contains searchable native text for this document.", X: 50, Y: 700}}}}, testpdf.Options{})
	for _, tc := range []struct {
		name string
		data []byte
		kind string
		cell string
	}{
		{"table.csv", []byte("Name,Value\nAlpha,42\n"), "sheet", "Sheet1!B2"},
		{"table.tsv", []byte("Name\tValue\nAlpha\t42\n"), "sheet", "Sheet1!B2"},
		{"paper.pdf", pdfData, "document", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, tc.name), tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			var previous string
			for i := 0; i < 2; i++ {
				response := panelRPC(t, gate, api.MethodFilesOpen, api.FilesOpenParams{Path: tc.name})
				if response.Error != nil {
					t.Fatal(response.Error)
				}
				var got api.FilesOpenResult
				_ = json.Unmarshal(response.Result, &got)
				if got.DocumentID == "" || got.Kind != tc.kind {
					t.Fatalf("%+v", got)
				}
				if i > 0 && got.DocumentID != previous {
					t.Fatal("digest was not deduplicated")
				}
				previous = got.DocumentID
				response = panelRPC(t, gate, api.MethodLibraryGet, api.LibraryIDParams{ID: got.DocumentID})
				if response.Error != nil {
					t.Fatal(response.Error)
				}
				response = panelRPC(t, gate, api.MethodLibraryPages, api.LibraryPagesParams{ID: got.DocumentID, From: 1, To: 1})
				if response.Error != nil {
					t.Fatal(response.Error)
				}
				var pages api.LibraryPagesResult
				_ = json.Unmarshal(response.Result, &pages)
				if len(pages.Pages) == 0 {
					t.Fatal("open returned before extraction")
				}
				if tc.cell != "" {
					found := false
					for _, block := range pages.Pages[0].Blocks {
						if block.Kind == docextract.KindCell && block.Anchor == tc.cell && block.Text == "42" {
							found = true
						}
					}
					if !found {
						t.Fatalf("missing cells: %+v", pages)
					}
				}
			}
		})
	}
}

func TestActivityAttributionOrderingAndLimit(t *testing.T) {
	store := orchestrator.NewMemoryStore()
	now := time.Now().UTC()
	parent := types.AgentID("master")
	child := types.AgentID("child")
	grandchild := types.AgentID("grandchild")
	other := types.AgentID("other")
	for _, agent := range []types.Agent{{ID: parent, Name: "master"}, {ID: child, ParentID: &parent, Name: "Worker", Profile: "implementer", Description: strings.Repeat("g", 400)}, {ID: grandchild, ParentID: &child, Name: "Nested"}, {ID: other, ParentID: &parent, Name: "Unrelated"}} {
		if err := store.SaveAgent(agent); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []types.Run{{ID: "seed", AgentID: child, State: types.RunRunning, CreatedAt: now, UpdatedAt: now}, {ID: "nested", AgentID: grandchild, State: types.RunCompleted, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)}, {ID: "old", AgentID: grandchild, State: types.RunFailed, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-31 * time.Minute)}, {ID: "unrelated", AgentID: other, State: types.RunRunning, CreatedAt: now, UpdatedAt: now}} {
		if err := store.CreateRun(orchestrator.RunRecord{Run: r}); err != nil {
			t.Fatal(err)
		}
	}
	b := NewDaemonBackend(nil, &OrchControl{Store: store}, telemetry.NewBus(nil))
	b.trackActivity("conversation", "run", "seed")
	gate := gateway.New(gateway.Config{AllowRemote: true}, b, nil, b.Bus)
	response := panelRPC(t, gate, api.MethodActivityList, api.ActivityListParams{ConversationID: "conversation"})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	var result api.ActivityListResult
	_ = json.Unmarshal(response.Result, &result)
	if len(result.Items) != 2 || result.Items[0].ID != "seed" || result.Items[1].ID != "nested" || result.Items[1].ParentID != "seed" || len(result.Items[0].Goal) != 300 {
		t.Fatalf("%+v", result)
	}
	items, err := b.activityItems(context.Background(), "unrelated-conversation", now)
	if err != nil || len(items) != 0 {
		t.Fatalf("cross conversation: %+v %v", items, err)
	}
	if response := panelRPC(t, gate, api.MethodActivityList, map[string]string{}); response.Error == nil {
		t.Fatal("missing conversation accepted")
	}
	for i := 0; i < 60; i++ {
		r := types.Run{ID: types.NewRunID(), AgentID: grandchild, State: types.RunQueued, CreatedAt: now, UpdatedAt: now}
		_ = store.CreateRun(orchestrator.RunRecord{Run: r})
	}
	items, err = b.activityItems(context.Background(), "conversation", now)
	if err != nil || len(items) != 50 {
		t.Fatalf("limit %d %v", len(items), err)
	}
}

func TestActivityThrottleRetainsTerminal(t *testing.T) {
	b := NewDaemonBackend(nil, nil, telemetry.NewBus(nil))
	now := time.Now()
	item := api.ActivityItem{ID: "run", State: "running", Step: "one"}
	b.publishActivity(item, now)
	item.Step = "two"
	b.publishActivity(item, now.Add(100*time.Millisecond))
	item.State = "completed"
	b.publishActivity(item, now.Add(400*time.Millisecond))
	if b.Bus.Len() != 1 {
		t.Fatal("throttle exceeded")
	}
	b.publishActivity(item, now.Add(500*time.Millisecond))
	b.publishActivity(item, now.Add(time.Second))
	if b.Bus.Len() != 2 {
		t.Fatal("missing terminal or duplicate event")
	}
	events := b.Bus.Replay(types.Cursor{})
	var got api.ActivityUpdatedEvent
	if err := json.Unmarshal(events[1].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if events[1].Type != api.EventActivityUpdated || got.Item.State != "completed" || got.Item.Step != "two" {
		t.Fatalf("%+v", got)
	}
}

func TestAutomationActivity(t *testing.T) {
	now := time.Now()
	finished := now.Add(time.Second)
	a := automations.Automation{Name: "Daily", Description: "Goal", Steps: []automations.Step{{ID: "s", Title: "Read file"}}}
	r := automations.Run{ID: "automation-run", Status: automations.RunRunning, StartedAt: now, Steps: []automations.StepRun{{StepID: "s", Status: automations.StepRunning, StartedAt: &now}}}
	item := automationActivity(a, r)
	if item.Kind != "automation" || item.Step != "Read file" || item.Progress.Total != 1 {
		t.Fatalf("%+v", item)
	}
	r.Status = automations.RunSucceeded
	r.FinishedAt = &finished
	r.Steps[0].Status = automations.StepSucceeded
	item = automationActivity(a, r)
	if item.State != "completed" || item.FinishedAt == "" || item.Progress.Done != 1 {
		t.Fatalf("%+v", item)
	}
}

func TestActivityTaskService(t *testing.T) {
	store := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), store, nil)
	tasks, err := NewTaskService(orch, store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tasks.Start(api.TaskStartParams{Request: "Implement a feature"})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := tasks.RunOf(result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	b := NewDaemonBackend(nil, &OrchControl{Store: store, Orch: orch}, nil)
	b.AttachServices(BackendServices{Tasks: tasks})
	b.trackActivity("conversation", "run", string(runID))
	items, err := b.activityItems(context.Background(), "conversation", time.Now())
	if err != nil || len(items) != 1 {
		t.Fatalf("%+v %v", items, err)
	}
	if items[0].ID != string(result.TaskID) || items[0].Kind != "task" || items[0].Name != "Implement a feature" {
		t.Fatalf("%+v", items)
	}
}

func TestActivityMonitorAndWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := orchestrator.NewMemoryStore()
	now := time.Now().UTC()
	_ = store.SaveAgent(types.Agent{ID: "worker", Name: "Worker"})
	_ = store.CreateRun(orchestrator.RunRecord{Run: types.Run{ID: "run", AgentID: "worker", State: types.RunRunning, CreatedAt: now, UpdatedAt: now}})
	orch := orchestrator.New(orchestrator.Config{}, store, nil)
	runner := orchfake.NewRunner("run", "worker")
	orch.AttachRunner(runner)
	b := NewDaemonBackend(nil, &OrchControl{Store: store, Orch: orch}, telemetry.NewBus(nil))
	b.trackActivity("conv", "run", "run")
	gate := gateway.New(gateway.Config{Listen: "127.0.0.1:0"}, b, nil, b.Bus)
	addr, err := gate.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Stop(context.Background()) }()
	conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	req, _ := api.NewRequest("subscribe", api.MethodConversationSubscribe, api.ConversationSubscribeParams{Types: []string{string(api.EventActivityUpdated)}})
	raw, _ := json.Marshal(req)
	if err = conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatal(err)
	}
	if _, _, err = conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	b.StartActivity(ctx)
	read := func() api.ActivityItem {
		t.Helper()
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var frame struct {
			Method string      `json:"method"`
			Params types.Event `json:"params"`
		}
		if err = json.Unmarshal(raw, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Method != "event" || frame.Params.Type != api.EventActivityUpdated {
			t.Fatalf("%s", raw)
		}
		var payload api.ActivityUpdatedEvent
		if err = json.Unmarshal(frame.Params.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Item
	}
	if item := read(); item.ID != "run" || item.State != "running" || item.Step != "Thinking…" {
		t.Fatalf("%+v", item)
	}
	runner.SetTool("exec", `{"command":"python3 app.py"}`, types.CancelImmediate, nil, time.Minute, now)
	if item := read(); item.Step != "Running python3 app.py" {
		t.Fatalf("%+v", item)
	}
	rec, _ := store.GetRun("run")
	rec.Run.State = types.RunCompleted
	rec.Run.LastProgress = "Finished"
	rec.Run.UpdatedAt = time.Now().UTC()
	_ = store.UpdateRun(rec)
	if item := read(); item.State != "completed" || item.Step != "Completed" {
		t.Fatalf("%+v", item)
	}
}

type activityAutomations struct {
	automations.Service
	list []automations.Automation
	runs map[string][]automations.Run
}

func (s *activityAutomations) List(context.Context) ([]automations.Automation, error) {
	return s.list, nil
}
func (s *activityAutomations) Runs(_ context.Context, id string, _ int) ([]automations.Run, error) {
	return s.runs[id], nil
}
func TestActivityAutomationAttribution(t *testing.T) {
	now := time.Now().UTC()
	svc := &activityAutomations{list: []automations.Automation{{ID: "owned", Name: "Owned"}, {ID: "other", Name: "Other"}}, runs: map[string][]automations.Run{
		"owned": {{ID: "scheduled", Status: automations.RunRunning, StartedAt: now}},
		"other": {{ID: "manual", Status: automations.RunRunning, StartedAt: now}, {ID: "unrelated", Status: automations.RunRunning, StartedAt: now}},
	}}
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Automations: svc})
	b.trackActivity("conv", "automation", "owned")
	b.trackActivity("conv", "automation_run", "manual")
	items, err := b.activityItems(context.Background(), "conv", now)
	if err != nil || len(items) != 2 {
		t.Fatalf("%+v %v", items, err)
	}
	for _, item := range items {
		if item.ID == "unrelated" {
			t.Fatal("unrelated run attributed")
		}
	}
	items, err = b.activityItems(context.Background(), "other", now)
	if err != nil || len(items) != 0 {
		t.Fatalf("cross conversation: %+v %v", items, err)
	}
}
func TestActivityStateMapping(t *testing.T) {
	for state, want := range map[types.RunState]string{types.RunCreated: "queued", types.RunQueued: "queued", types.RunRunning: "running", types.RunWaiting: "waiting", types.RunVerifying: "running", types.RunPaused: "paused", types.RunBlocked: "waiting", types.RunRecovering: "running", types.RunCompleted: "completed", types.RunCompletedPending: "completed", types.RunFailed: "failed", types.RunCanceled: "cancelled"} {
		if got := activityRunState(state); got != want {
			t.Errorf("%s: %s, want %s", state, got, want)
		}
	}
}
func TestFilesOpenExtractionFailureAndCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "paper.pdf"), []byte("%PDF fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"failed", "canceled", "pending"} {
		t.Run(status, func(t *testing.T) {
			routes, fake := libraryFixture()
			fake.doc.Status = status
			b := NewDaemonBackend(nil, nil, nil)
			b.AttachServices(BackendServices{Library: routes, FileBlobs: gateway.NewMemoryBlobs(), WorkspaceRoots: func(context.Context) []string { return []string{root} }})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := b.filesOpen(ctx, json.RawMessage(`{"path":"paper.pdf"}`))
			if err == nil {
				t.Fatal("unready document opened")
			}
			rpc, ok := err.(*api.Error)
			if !ok || rpc.Code != api.CodeConflict {
				t.Fatalf("%v", err)
			}
		})
	}
}
