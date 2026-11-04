package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

type delegationModel struct {
	mu             sync.Mutex
	scope          string
	release        chan struct{}
	started        chan struct{}
	childCalls     int
	startedOnce    sync.Once
	interrupted    chan struct{}
	sawMessage     bool
	language       string
	sawLanguage    bool
	fail           bool
	attachmentID   string
	childPrompt    string
	sawLibraryText bool
}

func (m *delegationModel) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	if role == types.RoleInterlocutor {
		card := map[string]any{"name": "writer", "description": "writes a file", "profile": "implementer", "goal": "Create hello.txt", "deliverable": "hello.txt", "done_criteria": []string{"file exists"}, "report_contract": "summary"}
		if m.scope != "" {
			card["scope"] = m.scope
		}
		args, _ := json.Marshal(map[string]any{"action": "spawn", "card": card})
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "spawn", Name: "subagent", Arguments: args}}}}, models.CallMeta{}, nil
	}
	m.mu.Lock()
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, "Goal:") {
			m.childPrompt = msg.Content
		}
		if msg.Role == providers.RoleTool && strings.Contains(msg.Content, "Track inventory and client sales") {
			m.sawLibraryText = true
		}
		if strings.Contains(msg.Content, "Answer in "+m.language+".") {
			m.sawLanguage = true
		}
	}
	m.childCalls++
	n := m.childCalls
	m.mu.Unlock()
	if n == 1 {
		m.startedOnce.Do(func() { close(m.started) })
		select {
		case <-m.release:
		case <-ctx.Done():
			m.mu.Lock()
			m.childCalls--
			m.mu.Unlock()
			select {
			case m.interrupted <- struct{}{}:
			default:
			}
			return providers.ChatResponse{}, models.CallMeta{}, ctx.Err()
		}
		if m.fail {
			return providers.ChatResponse{}, models.CallMeta{}, fmt.Errorf("simulated provider failure")
		}
		found := false
		for _, def := range req.Tools {
			if def.Name == "write_file" {
				found = true
			}
		}
		if !found {
			return providers.ChatResponse{}, models.CallMeta{}, fmt.Errorf("missing file tools")
		}
		if m.attachmentID != "" {
			for _, name := range []string{"library_view", "library_search"} {
				found := false
				for _, def := range req.Tools {
					found = found || def.Name == name
				}
				if !found {
					return providers.ChatResponse{}, models.CallMeta{}, fmt.Errorf("missing attachment tool %s", name)
				}
			}
			args, _ := json.Marshal(map[string]string{"document_id": m.attachmentID, "pages": "1"})
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "view", Name: "library_view", Arguments: args}}}}, models.CallMeta{}, nil
		}
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "write", Name: "write_file", Arguments: json.RawMessage(`{"path":"hello.txt","content":"hello\n"}`)}}}}, models.CallMeta{}, nil
	}
	if n == 2 && m.attachmentID != "" {
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "write", Name: "write_file", Arguments: json.RawMessage(`{"path":"hello.txt","content":"hello\n"}`)}}}}, models.CallMeta{}, nil
	}
	m.mu.Lock()
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, "Please verify the file") {
			m.sawMessage = true
		}
	}
	m.mu.Unlock()
	text := "Created hello.txt and verified the file."
	if m.language == "Spanish" {
		text = "Creé hello.txt y verifiqué el archivo."
	}
	return textReply(text), models.CallMeta{}, nil
}

func TestChatDelegationRunsAndPostsChanges(t *testing.T) {
	for _, mode := range []string{"success", "failure", "pause", "cancel", "shutdown", "queued", "sql", "spanish", "attachments", "attachments-queued", "workspace-empty", "workspace-existing", "workspace-general", "workspace-queued"} {
		t.Run(mode, func(t *testing.T) {
			fail := mode == "failure"
			attached := strings.HasPrefix(mode, "attachments")
			queued := strings.Contains(mode, "queued")
			managed := strings.HasPrefix(mode, "workspace-")
			t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
			ctx := context.Background()
			root := t.TempDir()
			projects := workspace.NewRegistry()
			project, err := projects.Register(workspace.Project{Name: "fixture", Roots: []string{root}})
			if err != nil {
				t.Fatal(err)
			}
			model := &delegationModel{scope: project.Scope().String(), release: make(chan struct{}), started: make(chan struct{}), fail: fail, interrupted: make(chan struct{}, 1)}
			if managed {
				model.scope = ""
				if mode == "workspace-general" {
					model.scope = "general"
				}
				if mode == "workspace-empty" {
					projects = workspace.NewRegistry()
				}
			}
			model.language = "English"
			if mode == "spanish" {
				model.language = "Spanish"
			}
			opts := AssembleOptions{Models: model, Projects: projects}
			dataDir := t.TempDir()
			if managed {
				opts.DataDir = dataDir
			}
			blobs := gateway.NewMemoryBlobs()
			if attached {
				opts.Memory = citationTestBackend(t).Services.Memory
				original := "%PDF-client-TDR-fixture"
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(original)))
				if err := blobs.Put(digest, strings.NewReader(original)); err != nil {
					t.Fatal(err)
				}
				doc, _, err := opts.Memory.CreatePendingDocument(ctx, "general", memory.DocMeta{Title: "Client TDR", SourceName: "TDR.pdf", BlobDigest: digest})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := opts.Memory.IngestExtracted(ctx, "general", doc.ID, docextract.Document{Format: "pdf", Pages: []docextract.Page{{Number: 1, Method: "native", Quality: 1, Blocks: []docextract.Block{{Kind: "paragraph", Text: "Track inventory and client sales"}}}}}); err != nil {
					t.Fatal(err)
				}
				model.attachmentID = doc.ID
			}
			if mode == "sql" {
				db, err := store.Open(ctx, filepath.Join(t.TempDir(), "chat.db"), store.DefaultOptions(), migrations.Main())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				opts.Store = orchestrator.OpenSQLStore(db)
			}
			s, err := Assemble(opts)
			if err != nil {
				t.Fatal(err)
			}
			var blockers []types.RunID
			if queued {
				for i := 0; i < 4; i++ {
					_, rec, err := s.Orch.Spawn(s.masterAgentID, orchestrator.DelegationCard{Name: fmt.Sprintf("blocker-%d", i), Description: "occupies admission", Profile: agents.ProfileCoordinator, Goal: "wait", Deliverable: "done", Scope: project.Scope(), Done: []string{"done"}, Report: "summary"})
					if err != nil {
						t.Fatal(err)
					}
					blockers = append(blockers, rec.Run.ID)
				}
			}
			historyStore := master.NewMemStore()
			b := NewDaemonBackend(master.New(master.Config{}, historyStore, nil, nil, nil, nil, nil), &OrchControl{Orch: s.Orch, Store: s.Store}, telemetry.NewBus(nil))
			b.AttachServices(BackendServices{History: s.History, Projects: projects, Memory: opts.Memory, FileBlobs: blobs})
			WireAssistantTurns(b, s, model, "test")
			t.Cleanup(func() {
				s.delegations.stop()
				stopCtx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				_ = b.Master.Turns().Stop(stopCtx)
			})
			request := "Create hello.txt in the fixture project"
			if mode == "spanish" {
				request = "Crea hello.txt en el proyecto de prueba"
			}
			if attached {
				request += "\nAttached: TDR.pdf [document:" + model.attachmentID + "]"
			}
			if _, _, err = b.ConversationSend(ctx, "test", "first", "cnv_delegation", request); err != nil {
				t.Fatal(err)
			}
			defer close(model.release)
			history := awaitAssistant(t, b, "cnv_delegation", 1)
			ack := "I started a subagent"
			if mode == "spanish" {
				ack = "Inicié un subagente"
			}
			if !strings.Contains(history[len(history)-1].Text, ack) {
				t.Fatalf("acknowledgement: %+v", history)
			}
			if queued {
				if queued := s.Store.RunsByState(types.RunQueued); len(queued) != 1 {
					t.Fatalf("expected queued chat child: %+v", queued)
				}
				select {
				case <-model.started:
					t.Fatal("queued child called model before admission")
				default:
				}
				// A queued child must retain attachments selected at spawn even
				// when later conversation turns move them out of the lookback.
				for i := 0; i < 6; i++ {
					if _, err := historyStore.SaveMessage(ctx, "cnv_delegation", master.Normalized{Role: "user", Text: "Later unrelated turn"}); err != nil {
						t.Fatal(err)
					}
				}
				for _, id := range blockers {
					if err := s.Orch.Finish(id, types.RunCompleted, "slot released"); err != nil {
						t.Fatal(err)
					}
				}
			}
			select {
			case <-model.started:
			case <-time.After(5 * time.Second):
				t.Fatal("child never called model")
			}
			if managed {
				var fresh *workspace.Project
				for _, candidate := range projects.List() {
					if candidate.ID != project.ID {
						if fresh != nil {
							t.Fatal("spawn created multiple workspaces")
						}
						fresh = candidate
					}
				}
				if fresh == nil || !strings.HasPrefix(fresh.Name, "writer (") || fresh.Description != "Create hello.txt" || fresh.VCS != "git" {
					t.Fatalf("missing managed project: %+v", fresh)
				}
				root = fresh.Roots[0]
				if filepath.Dir(root) != filepath.Join(dataDir, "workspaces") {
					t.Fatalf("unmanaged root: %s", root)
				}
				if _, err := os.Stat(filepath.Join(root, ".git", "HEAD")); err != nil {
					t.Fatal(err)
				}
				for _, rec := range s.Store.ListRuns() {
					agent, _ := s.Store.GetAgent(rec.Run.AgentID)
					if agent.Name == "writer" && (rec.Scope != fresh.Scope() || agent.Scope != fresh.Scope()) {
						t.Fatalf("child scope does not match managed project: %+v %+v", rec, agent)
					}
				}
				if _, err := os.Stat(filepath.Join(project.Roots[0], "hello.txt")); !os.IsNotExist(err) {
					t.Fatal("unscoped spawn used the unrelated existing project")
				}
			}
			runs := s.Store.ListRuns()
			id := ""
			for _, rec := range runs {
				agent, _ := s.Store.GetAgent(rec.Run.AgentID)
				if agent.Name == "writer" {
					id = string(rec.Run.ID)
				}
			}
			if id == "" {
				t.Fatalf("missing writer run: %+v", runs)
			}
			runner := &assistantToolRunner{daemonToolRunner: daemonToolRunner{ex: s.Exec, reg: s.Tools}, allowed: map[string]bool{"subagent": true}, runID: "master", root: s.masterAgentID}
			for _, input := range []string{
				fmt.Sprintf(`{"action":"status","run_id":%q}`, id),
				fmt.Sprintf(`{"action":"message","run_id":%q,"text":"Please verify the file"}`, id),
				fmt.Sprintf(`{"action":"message","to":%q,"message_type":"report","text":"Continue"}`, id),
			} {
				result, err := runner.Execute(ctx, "subagent", json.RawMessage(input), tools.ExecuteOptions{})
				if err != nil || result.Status != tools.StatusOK {
					t.Fatalf("%s: %+v %v", input, result, err)
				}
			}
			if mode == "shutdown" {
				s.delegations.stop()
				if len(b.Master.History("cnv_delegation", 0)) != 2 {
					t.Fatal("shutdown posted a spurious completion")
				}
				if _, err := os.Stat(filepath.Join(root, "hello.txt")); !os.IsNotExist(err) {
					t.Fatal("shutdown child wrote file")
				}
				return
			}
			if mode == "pause" {
				if err := s.Orch.Pause(types.RunID(id), types.PauseUser, ""); err != nil {
					t.Fatal(err)
				}
				select {
				case <-model.interrupted:
				case <-time.After(5 * time.Second):
					t.Fatal("pause did not interrupt child")
				}
				if err := s.Orch.Resume(types.RunID(id), ""); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cancel" {
				if err := s.Orch.Cancel(types.RunID(id)); err != nil {
					t.Fatal(err)
				}
			} else {
				model.release <- struct{}{}
			}
			history = awaitAssistant(t, b, "cnv_delegation", 2)
			report := history[len(history)-1]
			if mode == "success" {
				var parent, turn string
				var called, edited, completed bool
				for _, event := range b.Bus.Replay(types.Cursor{}) {
					var payload struct {
						Conversation string `json:"conversation_id"`
						Run          string `json:"run_id"`
						Parent       string `json:"parent_run_id"`
						Turn         string `json:"turn_id"`
						Tool         string `json:"tool"`
						State        string `json:"state"`
					}
					_ = json.Unmarshal(event.Payload, &payload)
					if payload.Run != id {
						continue
					}
					if payload.Conversation != "cnv_delegation" || payload.Parent == "" || payload.Turn == "" {
						t.Fatalf("missing child association: %s", event.Payload)
					}
					parent, turn = payload.Parent, payload.Turn
					called = called || (event.Type == types.EventToolCalled && payload.Tool == "write_file")
					edited = edited || event.Type == types.EventFileEdited
					completed = completed || (event.Type == types.EventRunProgress && payload.State == "completed")
				}
				if parent != turn || !called || !edited {
					t.Fatalf("missing delegated activity: parent=%s turn=%s called=%v edited=%v", parent, turn, called, edited)
				}
				// The final progress event is deferred until after the report has been delivered.
				deadline := time.Now().Add(time.Second)
				for !completed && time.Now().Before(deadline) {
					for _, event := range b.Bus.Replay(types.Cursor{}) {
						var p struct {
							Run   string `json:"run_id"`
							State string `json:"state"`
						}
						_ = json.Unmarshal(event.Payload, &p)
						completed = completed || (event.Type == types.EventRunProgress && p.Run == id && p.State == "completed")
					}
					time.Sleep(time.Millisecond)
				}
				if !completed {
					t.Fatal("missing final child progress")
				}
			}
			rec, err := s.Store.GetRun(types.RunID(id))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" {
				if rec.Run.State != types.RunCanceled || !strings.Contains(report.Text, "canceled") {
					t.Fatalf("cancellation: %+v %+v", rec, report)
				}
				if _, err := os.Stat(filepath.Join(root, "hello.txt")); !os.IsNotExist(err) {
					t.Fatal("canceled child wrote file")
				}
				return
			}
			if fail {
				if rec.Run.State != types.RunFailed || !strings.Contains(report.Text, "did not complete") {
					t.Fatalf("failure: %+v %+v", rec, report)
				}
				return
			}
			if rec.Run.State != types.RunCompleted {
				t.Fatalf("state: %+v; report: %+v", rec, report)
			}
			if !managed && len(projects.List()) != 1 {
				t.Fatal("explicit project scope created an extra workspace")
			}
			data, err := os.ReadFile(filepath.Join(root, "hello.txt"))
			if err != nil || string(data) != "hello\n" {
				t.Fatalf("file: %q %v", data, err)
			}
			if len(report.Changes) != 1 || report.Changes[0].Path != "hello.txt" || report.Changes[0].Added != 1 {
				t.Fatalf("changes: %+v", report)
			}
			model.mu.Lock()
			sawLanguage := model.sawLanguage
			sawMessage := model.sawMessage
			childPrompt := model.childPrompt
			sawLibraryText := model.sawLibraryText
			model.mu.Unlock()
			if attached {
				for _, want := range []string{"Attached documents", "Client TDR", model.attachmentID, "Track inventory and client sales", "docs/TDR.pdf", "docs/TDR.md"} {
					if !strings.Contains(childPrompt, want) {
						t.Errorf("child prompt omitted %q", want)
					}
				}
				if !sawLibraryText {
					t.Error("child could not read attached document through library_view")
				}
				for name, want := range map[string]string{"TDR.pdf": "%PDF-client-TDR-fixture", "TDR.md": "Track inventory and client sales\n\n"} {
					data, err := os.ReadFile(filepath.Join(root, "docs", name))
					if err != nil || string(data) != want {
						t.Errorf("attachment %s: %q, %v", name, data, err)
					}
				}
			} else if strings.Contains(childPrompt, "Attached documents") {
				t.Error("no-attachment delegation gained an attachment section")
			}
			if !sawLanguage {
				t.Fatal("child context omitted the user language instruction")
			}
			if mode == "spanish" && report.Text != "Creé hello.txt y verifiqué el archivo." {
				t.Fatalf("report language: %q", report.Text)
			}
			if !sawMessage {
				t.Fatal("parent message was not included in the child context")
			}
			entries, err := s.Jrnl.List(ctx, id)
			if err != nil || len(entries) == 0 {
				t.Fatalf("journal: %v %v", entries, err)
			}
			if len(b.Master.History("cnv_unrelated", 0)) != 0 {
				t.Fatal("report leaked into another conversation")
			}
		})
	}
}

func TestLooksSpanishAcknowledgement(t *testing.T) {
	for text, want := range map[string]bool{
		"En el proyecto demo, agrega despedir": true,
		"Please edit app.py and add a README":  false,
		"¿Puedes revisar esto?":                true,
	} {
		if got := looksSpanish(text); got != want {
			t.Errorf("looksSpanish(%q) = %v, want %v", text, got, want)
		}
	}
}
