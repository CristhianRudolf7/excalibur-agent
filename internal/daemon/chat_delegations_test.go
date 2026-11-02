package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
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
	mu          sync.Mutex
	scope       string
	release     chan struct{}
	started     chan struct{}
	childCalls  int
	startedOnce sync.Once
	interrupted chan struct{}
	sawMessage  bool
	language    string
	sawLanguage bool
	fail        bool
}

func (m *delegationModel) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	if role == types.RoleInterlocutor {
		card := map[string]any{"name": "writer", "description": "writes a file", "profile": "implementer", "goal": "Create hello.txt", "deliverable": "hello.txt", "done_criteria": []string{"file exists"}, "report_contract": "summary", "scope": m.scope}
		args, _ := json.Marshal(map[string]any{"action": "spawn", "card": card})
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "spawn", Name: "subagent", Arguments: args}}}}, models.CallMeta{}, nil
	}
	m.mu.Lock()
	for _, msg := range req.Messages {
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
	for _, mode := range []string{"success", "failure", "pause", "cancel", "shutdown", "queued", "sql", "spanish"} {
		t.Run(mode, func(t *testing.T) {
			fail := mode == "failure"
			t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
			ctx := context.Background()
			root := t.TempDir()
			projects := workspace.NewRegistry()
			project, err := projects.Register(workspace.Project{Name: "fixture", Roots: []string{root}})
			if err != nil {
				t.Fatal(err)
			}
			model := &delegationModel{scope: project.Scope().String(), release: make(chan struct{}), started: make(chan struct{}), fail: fail, interrupted: make(chan struct{}, 1)}
			model.language = "English"
			if mode == "spanish" {
				model.language = "Spanish"
			}
			opts := AssembleOptions{Models: model, Projects: projects}
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
			if mode == "queued" {
				for i := 0; i < 4; i++ {
					_, rec, err := s.Orch.Spawn(s.masterAgentID, orchestrator.DelegationCard{Name: fmt.Sprintf("blocker-%d", i), Description: "occupies admission", Profile: agents.ProfileCoordinator, Goal: "wait", Deliverable: "done", Scope: project.Scope(), Done: []string{"done"}, Report: "summary"})
					if err != nil {
						t.Fatal(err)
					}
					blockers = append(blockers, rec.Run.ID)
				}
			}
			b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), &OrchControl{Orch: s.Orch, Store: s.Store}, telemetry.NewBus(nil))
			b.AttachServices(BackendServices{History: s.History, Projects: projects})
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
			if mode == "queued" {
				if queued := s.Store.RunsByState(types.RunQueued); len(queued) != 1 {
					t.Fatalf("expected queued chat child: %+v", queued)
				}
				select {
				case <-model.started:
					t.Fatal("queued child called model before admission")
				default:
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
			model.mu.Unlock()
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
