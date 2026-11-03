package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Only inference is simulated: creation, avatar validation, relay intake,
// durable histories, unread events and favorite handover use production wiring.
func TestPersonalAgentsAvatarRelayAndFavoriteEndToEnd(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "agents.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, registry := personalBackend(t, db)
	favorite := registry.Favorite()
	model := models.NewGateway(models.GatewayConfig{MaxAttempts: 1, DisableBudgetRetries: true, Roles: map[types.ModelRole]models.RoleEntry{types.RoleInterlocutor: {Primary: models.ModelRef{Provider: "personal-integration", Model: "test"}}}})
	defer model.Close()
	var favoriteCalls, targetCalls atomic.Int32
	designed := agents.DefaultAvatar("Pablito")
	designed.Accessory = "glasses"
	toolReply := func(name string, args any) providers.ChatResponse {
		raw, _ := json.Marshal(args)
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: name, Name: name, Arguments: raw}}}}
	}
	model.Register(&turnProvider{FakeProvider: fake.New("personal-integration"), chat: func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, "Agent name: Pablito") {
				targetCalls.Add(1)
				for _, def := range req.Tools {
					if def.Name == "send_to_agent" {
						t.Error("relayed turn can relay again")
					}
				}
				return textReply("Budget reviewed."), nil
			}
		}
		switch favoriteCalls.Add(1) {
		case 1:
			return toolReply("design_avatar", map[string]any{"description": "Pablito with glasses", "spec": designed}), nil
		case 2:
			var result struct {
				Spec agents.AvatarSpec `json:"spec"`
			}
			for _, msg := range req.Messages {
				if msg.ToolResult != nil && msg.ToolResult.Name == "design_avatar" {
					if err := json.Unmarshal([]byte(msg.ToolResult.Output), &result); err != nil {
						t.Error(err)
					}
				}
			}
			if result.Spec != designed {
				t.Error("avatar result did not reach the model")
			}
			return toolReply("manage_agents", map[string]any{"action": "create", "name": "Pablito", "avatar": result.Spec}), nil
		case 3:
			return textReply("Pablito is ready."), nil
		case 4:
			return toolReply("send_to_agent", map[string]string{"agent": "Pablito", "message": "revisa el presupuesto"}), nil
		default:
			return textReply("Delivered."), nil
		}
	}})
	services, err := Assemble(AssembleOptions{Models: model})
	if err != nil {
		t.Fatal(err)
	}
	WireAssistantTurns(b, services, model, "test")
	defer services.delegations.stop()
	defer services.Credentials.Close()
	gate := gateway.New(gateway.DefaultConfig(), b, gateway.NewMemoryBlobs(), b.Bus)
	addr, err := gate.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Stop(ctx)
	client := gateway.Dial(addr, "personal-integration")
	call := func(method string, params any, out any) {
		t.Helper()
		raw, err := client.Call(ctx, method, method, params)
		if err != nil {
			t.Fatal(err)
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatal(err)
			}
		}
	}
	call(api.MethodConversationSend, api.ConversationSendParams{ConversationID: favorite.ConversationID, ClientMsgID: "create", Text: "Create agent Pablito and design an avatar with glasses"}, nil)
	awaitAssistant(t, b, favorite.ConversationID, 1)
	var list api.AgentsListResult
	call(api.MethodAgentsList, api.AgentsListParams{}, &list)
	if len(list.Agents) != 2 {
		t.Fatalf("agents.list: %+v", list)
	}
	target, err := registry.Resolve("Pablito")
	if err != nil {
		t.Fatal(err)
	}
	if target.Avatar != designed {
		t.Fatal("created avatar differs from design_avatar result")
	}
	var listed bool
	for _, agent := range list.Agents {
		if agent.ID == target.ID && agent.Avatar == designed {
			listed = true
		}
	}
	if !listed {
		t.Fatal("agents.list omitted the designed avatar")
	}
	call(api.MethodConversationSend, api.ConversationSendParams{ConversationID: favorite.ConversationID, ClientMsgID: "relay", Text: "Send to agent Pablito: revisa el presupuesto"}, nil)
	awaitAssistant(t, b, favorite.ConversationID, 2)
	history := awaitAssistant(t, b, target.ConversationID, 1)
	if len(history) != 2 || history[0].Role != "user" || history[0].Text != "revisa el presupuesto" || history[0].RelayedBy != favorite.ID || history[1].Role != "assistant" || targetCalls.Load() != 1 {
		t.Fatalf("relay did not run one isolated user turn: %+v", history)
	}
	target, _ = registry.Get(target.ID)
	if target.Unread != 2 {
		t.Fatalf("relay and answer unread = %d", target.Unread)
	}
	var handover api.AgentsFavoriteResult
	call(api.MethodAgentsArchive, api.AgentIDParams{ID: favorite.ID}, &handover)
	if handover.FavoriteID != target.ID || b.SystemConversation("") != target.ConversationID || b.SystemConversation(favorite.ConversationID) != target.ConversationID {
		t.Fatal("archiving favorite did not hand over default routing")
	}
}
