package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestClassroomStudyIntentExposesToolAndSkill(t *testing.T) {
	for _, request := range []string{"quiero aprender algebra", "teach me algebra"} {
		t.Run(request, func(t *testing.T) {
			calls := 0
			b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
				calls++
				names := []string{}
				for _, def := range req.Tools {
					names = append(names, def.Name)
				}
				if !slices.Contains(names, "classroom") || !slices.Contains(names, "whiteboard") || !slices.Contains(names, "load_skill") {
					t.Fatalf("missing classroom or skill loader: %v", names)
				}
				if calls == 1 {
					return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "load", Name: "load_skill", Arguments: json.RawMessage(`{"name":"course-creation"}`)}}}}, nil
				}
				found := false
				for _, msg := range req.Messages {
					if msg.ToolResult != nil && strings.Contains(msg.ToolResult.Output, "write_feedback") {
						found = true
					}
				}
				if !found {
					t.Fatal("course skill did not resolve")
				}
				return textReply("What do you want to achieve?"), nil
			})
			a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
			if _, _, err := a.CompleteTurn(context.Background(), ClassroomConversation, master.Normalized{Text: request}); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("model calls %d", calls)
			}
		})
	}
}
func TestClassroomGenerationEventsExecuteTrustedTool(t *testing.T) {
	for _, tc := range []struct {
		kind types.EventType
		args string
		want string
	}{
		{"classroom.class_requested", `{"action":"write_class","course_id":"c","topic_id":"t","format":"chat","content":"# Equations"}`, "write_class"},
		{"classroom.quiz_requested", `{"action":"create_quiz","course_id":"c","topic_id":"t","questions":[{"kind":"single","prompt":"Which balances?","options":["A","B"],"correct":[0],"explanation":"Balance both sides."}]}`, "create_quiz"},
		{"classroom.attempt_graded", `{"action":"write_feedback","attempt_id":"a","feedback":"Review balance with a different example."}`, "write_feedback"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			calls := 0
			b, s := assistantTestWiring(t, false, func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
				calls++
				if app := api.AppContextFrom(ctx); app == nil || app.App != "classroom" || app.CourseID != "c" || app.TopicID != "t" {
					t.Errorf("system turn context lost: %+v", app)
				}
				for _, name := range []string{"classroom", "whiteboard", "load_skill"} {
					if !slices.ContainsFunc(req.Tools, func(def providers.ToolDefinition) bool { return def.Name == name }) {
						t.Errorf("system turn did not offer %s", name)
					}
				}
				if calls == 1 {
					system := false
					for _, msg := range req.Messages {
						if msg.Role == providers.RoleSystem && strings.Contains(msg.Content, tc.want) {
							system = true
						}
					}
					if !system {
						t.Error("generation instruction missing system origin")
					}
					return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "generate", Name: "classroom", Arguments: json.RawMessage(tc.args)}}}}, nil
				}
				found := false
				for _, msg := range req.Messages {
					if msg.ToolResult != nil {
						found = true
						if strings.Contains(msg.ToolResult.Output, "unavailable") {
							t.Error(msg.ToolResult.Output)
						}
					}
				}
				if !found {
					t.Error("tool result missing")
				}
				return textReply("Prepared."), nil
			})
			agent := contextService()
			tool, err := s.Tools.Get("classroom")
			if err != nil {
				t.Fatal(err)
			}
			tool.(*classroom.Tool).Service = agent
			s.ClassroomAgent = agent
			ev := classroomEvent("event", tc.kind, "t", false)
			for i := 0; i < 3; i++ {
				if err := b.handleClassroomEvent(context.Background(), ev, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			awaitAssistant(t, b, ClassroomConversation, 1)
			count := 0
			for _, call := range agent.SnapshotCalls() {
				if call == tc.want {
					count++
				}
			}
			if count != 1 || calls != 2 {
				t.Fatalf("tool calls %+v, model calls %d", agent.SnapshotCalls(), calls)
			}
		})
	}
}

func TestClassroomToolSelectionFollowsOnlyCurrentContext(t *testing.T) {
	_, s := assistantTestWiring(t, false, nil)
	for _, app := range []*api.AppContext{{App: "classroom"}, nil, {App: "future"}} {
		activation := s.chatToolActivation("Go ahead with the agreed plan", "test", app)
		if got, want := slices.Contains(activation.Names, "classroom"), app != nil && app.App == "classroom"; got != want {
			t.Fatalf("context %+v: tools %v", app, activation.Names)
		}
	}
}

type classroomDelegationModel struct {
	t *testing.T
}

func (m classroomDelegationModel) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	for _, name := range []string{"classroom", "whiteboard", "load_skill"} {
		if !slices.ContainsFunc(req.Tools, func(def providers.ToolDefinition) bool { return def.Name == name }) {
			m.t.Errorf("role %s did not offer %s", role, name)
		}
	}
	app := api.AppContextFrom(ctx)
	if app == nil || app.App != "classroom" || app.CourseID != "c" || app.TopicID != "t" {
		m.t.Errorf("role %s lost context: %+v", role, app)
	}
	if role == types.RoleInterlocutor {
		args, _ := json.Marshal(map[string]any{"action": "spawn", "card": map[string]any{"name": "teacher", "description": "Prepare next step", "profile": agents.ProfileImplementer, "scope": "general", "goal": "Continue the agreed plan", "deliverable": "next step", "done_criteria": []string{"prepared"}, "report_contract": "summary"}})
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "spawn", Name: "subagent", Arguments: args}}}}, models.CallMeta{}, nil
	}
	for _, msg := range req.Messages {
		if msg.ToolResult != nil {
			if msg.ToolResult.IsError || !strings.Contains(msg.ToolResult.Output, "Algebra") {
				m.t.Errorf("child did not use shared classroom service: %+v", msg.ToolResult)
			}
			return textReply("Prepared."), models.CallMeta{}, nil
		}
	}
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "read", Name: "classroom", Arguments: json.RawMessage(`{"action":"get_course","course_id":"c"}`)}}}}, models.CallMeta{}, nil
}

func TestClassroomDelegationInheritsMessageContextAndService(t *testing.T) {
	b, _ := assistantTestWiring(t, false, nil)
	model := classroomDelegationModel{t: t}
	s, err := Assemble(AssembleOptions{Models: model, ClassroomAgent: contextService()})
	if err != nil {
		t.Fatal(err)
	}
	WireAssistantTurns(b, s, model, "test")
	defer s.delegations.stop()
	if _, _, err := b.Master.ReceiveWithContext(context.Background(), "test", "delegate", ClassroomConversation, "Continue the agreed plan", nil, &api.AppContext{App: "classroom", CourseID: "c", TopicID: "t"}); err != nil {
		t.Fatal(err)
	}
	awaitAssistant(t, b, ClassroomConversation, 2)
}
func TestClassroomAssembleUsesInjectedService(t *testing.T) {
	agent := contextService()
	s, err := Assemble(AssembleOptions{ClassroomAgent: agent})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := s.Tools.Get("classroom")
	if err != nil {
		t.Fatal(err)
	}
	if tool.(*classroom.Tool).Service != agent || s.ClassroomAgent != agent {
		t.Fatal("daemon did not wire shared classroom service")
	}
}
