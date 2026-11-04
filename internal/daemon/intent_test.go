package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestChatIntentActivationPerNewMessage(t *testing.T) {
	var requests []providers.ChatRequest
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		requests = append(requests, req)
		return textReply("Finished."), nil
	})
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	for _, tc := range []struct {
		request string
		want    bool
	}{
		{"explain this failing test", false},
		{"draw an architecture diagram of this repo", true},
		{"haz un diagrama de flujo", true},
		{"explain this failing test", false},
	} {
		_, runs, err := a.CompleteTurn(context.Background(), "conversation", master.Normalized{Text: tc.request})
		if err != nil || len(runs) != 1 {
			t.Fatalf("turn: %v %v", runs, err)
		}
		req := requests[len(requests)-1]
		var names []string
		for _, def := range req.Tools {
			names = append(names, def.Name)
		}
		for _, name := range []string{"diagram_render", "code_diagram"} {
			if slices.Contains(names, name) != tc.want {
				t.Errorf("%q: tool %s in %v, want %v", tc.request, name, names, tc.want)
			}
		}
		if tc.want && !slices.Contains(names, "load_skill") {
			t.Fatal("matching hint has no load_skill tool")
		}
		entries, err := s.Jrnl.List(context.Background(), runs[0])
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			if entry.Type != "tools_activated" {
				continue
			}
			count++
			var activation wireengine.ToolActivation
			if err := json.Unmarshal(entry.Payload, &activation); err != nil {
				t.Fatal(err)
			}
			if len(activation.Skills) != 1 {
				t.Fatalf("hints: %+v", activation)
			}
			var prompt strings.Builder
			for _, msg := range req.Messages {
				prompt.WriteString(msg.Content)
			}
			if !strings.Contains(prompt.String(), "load_skill with name="+activation.Skills[0].Name) {
				t.Fatal("hint absent from chat prompt")
			}
		}
		if tc.want && count != 1 {
			t.Fatalf("activation entries=%d", count)
		}
	}
}

func TestChatActivatedRendererPublishesArtifacts(t *testing.T) {
	output := filepath.Join(t.TempDir(), "diagram.svg")
	args, _ := json.Marshal(map[string]string{"source": `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`, "dialect": "svg", "output_format": "svg", "output": output})
	calls := 0
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if calls == 1 {
			found := false
			for _, def := range req.Tools {
				found = found || def.Name == "diagram_render"
			}
			if !found {
				t.Fatal("renderer absent from first tool list")
			}
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "render", Name: "diagram_render", Arguments: args}}}}, nil
		}
		return textReply("The diagram is ready."), nil
	})
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	_, _, err := a.CompleteTurn(context.Background(), "conversation", master.Normalized{Text: "draw an architecture diagram"})
	if err != nil {
		t.Fatal(err)
	}
	created, ready := 0, 0
	for _, event := range b.Bus.Replay(types.Cursor{}) {
		switch event.Type {
		case api.EventArtifactCreated:
			created++
		case api.EventArtifactReady:
			ready++
		}
	}
	if created != 1 || ready != 1 {
		t.Fatalf("created=%d ready=%d calls=%d", created, ready, calls)
	}
}

// The child uses the real engine builder and role gate without a parent model.
type intentChildModel struct{ requests []providers.ChatRequest }

func (m *intentChildModel) Chat(_ context.Context, _ types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	m.requests = append(m.requests, req)
	return textReply("Finished."), models.CallMeta{}, nil
}

func TestChatChildIntentRespectsExplicitToolGrant(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		t.Run(map[bool]string{false: "default grant", true: "explicit grant"}[restricted], func(t *testing.T) {
			model := &intentChildModel{}
			s, err := Assemble(AssembleOptions{Models: model})
			if err != nil {
				t.Fatal(err)
			}
			b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), &OrchControl{Orch: s.Orch, Store: s.Store}, telemetry.NewBus(nil))
			b.AttachServices(BackendServices{})
			WireAssistantTurns(b, s, model, "test")
			defer s.delegations.stop()
			card := orchestrator.DelegationCard{Name: "diagrammer", Description: "draws diagrams", Profile: agents.ProfileImplementer, Goal: "draw an architecture diagram of this repo", Deliverable: "diagram", Scope: types.GeneralScope(), Done: []string{"diagram exists"}, Report: "summary"}
			if restricted {
				card.Tools = []string{"read_file"}
			}
			_, rec, err := s.Orch.Spawn(s.masterAgentID, card)
			if err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(map[string]any{"card": map[string]any{"goal": card.Goal, "deliverable": card.Deliverable, "done_criteria": card.Done, "tools": card.Tools}})
			s.delegations.run("conversation", t.TempDir(), args, string(rec.Run.ID), false, nil, nil)
			if len(model.requests) == 0 {
				t.Fatal("child model never called")
			}
			var names []string
			for _, def := range model.requests[0].Tools {
				names = append(names, def.Name)
			}
			for _, name := range []string{"diagram_render", "code_diagram"} {
				if slices.Contains(names, name) == restricted {
					t.Errorf("%s presence in %v, restricted=%v", name, names, restricted)
				}
			}
		})
	}
}
