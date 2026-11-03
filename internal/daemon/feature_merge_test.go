package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	toolfake "github.com/CristhianRudolf7/excalibur-agent/internal/tools/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestClassroomTurnRetainsRobotLabAndArtifactDelivery(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	calls := 0
	app := &api.AppContext{App: "classroom", CourseID: "course", TopicID: "topic"}
	b, s := assistantTestWiring(t, false, func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if got := api.AppContextFrom(ctx); got == nil || *got != *app {
			t.Fatalf("message context lost: %+v", got)
		}
		for _, name := range []string{"classroom", "load_skill", "robot_lab"} {
			if !slices.ContainsFunc(req.Tools, func(def providers.ToolDefinition) bool { return def.Name == name }) {
				t.Errorf("combined assistant surface lost %s", name)
			}
		}
		if calls == 1 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "lab-call", Name: "robot_lab", Arguments: json.RawMessage(`{}`)}}}}, nil
		}
		return textReply("The simulation artifact is ready for review."), nil
	})
	fake := toolfake.New("robot_lab")
	fake.Results = []tools.Result{{Status: tools.StatusOK, Output: "rendered", Artifacts: []tools.ArtifactRef{artifactFixture(t, "simulation")}}}
	s.Tools.Unregister("robot_lab")
	if err := s.Tools.Register(fake); err != nil {
		t.Fatal(err)
	}
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	if _, _, err := a.CompleteTurn(context.Background(), ClassroomConversation, master.Normalized{Text: "Explain the simulation", Context: app}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("model calls = %d, want 2", calls)
	}
	created, ready := 0, 0
	for _, event := range b.Bus.Replay(types.Cursor{}) {
		switch event.Type {
		case api.EventArtifactCreated:
			created++
		case api.EventArtifactReady:
			ready++
			var payload api.ArtifactReadyEvent
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ConversationID != ClassroomConversation || len(payload.Artifacts) != 1 || payload.Artifacts[0].ToolCallID != "lab-call" {
				t.Fatalf("artifact provenance lost: %+v", payload)
			}
		}
	}
	if created != 1 || ready != 1 {
		t.Fatalf("artifact events: created=%d ready=%d", created, ready)
	}
}
