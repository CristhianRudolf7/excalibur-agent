package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
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
				if !slices.Contains(names, "classroom") || !slices.Contains(names, "load_skill") {
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
			b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
				calls++
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
