package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestPersonalAgentRPCAndEvents(t *testing.T) {
	ctx := context.Background()
	b, r := personalBackend(t, nil)
	server := httptest.NewServer(gateway.New(gateway.Config{}, b, gateway.NewMemoryBlobs(), b.Bus))
	defer server.Close()
	rpc := func(method string, params any, out any, code int) {
		t.Helper()
		req, err := api.NewRequest("agents", method, params)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(req)
		resp, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result api.Response
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			if result.Error == nil || result.Error.Code != code {
				t.Fatalf("%s: expected %d, got %+v", method, code, result)
			}
			return
		}
		if result.Error != nil {
			t.Fatalf("%s: %v", method, result.Error)
		}
		if out != nil {
			if err = json.Unmarshal(result.Result, out); err != nil {
				t.Fatal(err)
			}
		}
	}
	first := r.Favorite()
	rpc(api.MethodAgentsArchive, api.AgentIDParams{ID: first.ID}, nil, api.CodeConflict)
	rpc(api.MethodAgentsCreate, map[string]any{"name": "Invalid", "avatar": map[string]any{"variant": "bad"}}, nil, api.CodeInvalidParams)
	var a api.Agent
	rpc(api.MethodAgentsCreate, api.AgentsCreateParams{Name: "Ada", Personality: "Exact fractions"}, &a, 0)
	title := "Tutor"
	rpc(api.MethodAgentsUpdate, api.AgentsUpdateParams{ID: a.ID, Title: &title}, &a, 0)
	if a.Title != title || a.ConversationID == first.ConversationID {
		t.Fatal(a)
	}
	rpc(api.MethodAgentsGet, api.AgentIDParams{ID: a.ID}, &a, 0)
	var favorite api.AgentsFavoriteResult
	rpc(api.MethodAgentsSetFavorite, api.AgentIDParams{ID: a.ID}, &favorite, 0)
	if favorite.FavoriteID != a.ID {
		t.Fatal(favorite)
	}
	rpc(api.MethodAgentsArchive, api.AgentIDParams{ID: a.ID}, &favorite, 0)
	if favorite.FavoriteID != first.ID {
		t.Fatal(favorite)
	}
	var list api.AgentsListResult
	rpc(api.MethodAgentsList, api.AgentsListParams{}, &list, 0)
	if len(list.Agents) != 1 || list.FavoriteID != first.ID {
		t.Fatal(list)
	}
	rpc(api.MethodAgentsList, api.AgentsListParams{IncludeArchived: true}, &list, 0)
	if len(list.Agents) != 2 {
		t.Fatal(list)
	}
	rpc(api.MethodAgentsRestore, api.AgentIDParams{ID: a.ID}, &a, 0)
	if a.Archived || a.Favorite {
		t.Fatal(a)
	}
	if _, err := b.Master.SendAssistant(ctx, a.ConversationID, "A new answer", nil); err != nil {
		t.Fatal(err)
	}
	var read api.AgentsMarkReadResult
	rpc(api.MethodAgentsMarkRead, api.AgentIDParams{ID: a.ID}, &read, 0)
	if read.Unread != 0 {
		t.Fatal(read)
	}
	rpc(api.MethodAgentsGet, api.AgentIDParams{ID: a.ID}, &a, 0)
	if a.Unread != 0 || a.LastPreview != "A new answer" || a.LastMessageAt == nil {
		t.Fatal(a)
	}
	rpc(api.MethodAgentsGet, api.AgentIDParams{ID: "missing"}, nil, api.CodeUnknownTarget)
	rpc(api.MethodAgentsUpdate, map[string]any{}, nil, api.CodeInvalidParams)
	seen := map[types.EventType]bool{}
	for _, ev := range b.Bus.Replay(types.Cursor{}) {
		seen[ev.Type] = true
	}
	for _, kind := range []types.EventType{api.EventAgentsCreated, api.EventAgentsUpdated, api.EventAgentsArchived, api.EventAgentsRestored, api.EventAgentsFavoriteChanged, api.EventAgentsUnread} {
		if !seen[kind] {
			t.Errorf("missing event %s", kind)
		}
	}
}

func TestPersonalAgentRelayNormalTurnOnceAndNoLoops(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "relay.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, registry := personalBackend(t, db)
	sender := registry.Favorite()
	target, err := registry.Create(ctx, agents.CreateParams{Name: "Ada", Personality: "Use exact fractions"})
	if err != nil {
		t.Fatal(err)
	}
	model := models.NewGateway(models.GatewayConfig{MaxAttempts: 1, DisableBudgetRetries: true, Roles: map[types.ModelRole]models.RoleEntry{types.RoleInterlocutor: {Primary: models.ModelRef{Provider: "agent-test", Model: "test"}}}})
	defer model.Close()
	var calls atomic.Int32
	model.Register(&turnProvider{FakeProvider: fake.New("agent-test"), chat: func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls.Add(1)
		var all strings.Builder
		for _, m := range req.Messages {
			all.WriteString(m.Content)
		}
		if !strings.Contains(all.String(), "Use exact fractions") || strings.Contains(all.String(), "Private sender history") {
			t.Error("relay persona or history isolation")
		}
		for _, def := range req.Tools {
			if def.Name == "send_to_agent" {
				t.Error("relay exposed send_to_agent")
			}
		}
		return textReply("One half."), nil
	}})
	s, err := Assemble(AssembleOptions{Models: model})
	if err != nil {
		t.Fatal(err)
	}
	WireAssistantTurns(b, s, model, "test")
	defer s.delegations.stop()
	if _, err := b.Master.SendAssistant(ctx, sender.ConversationID, "Private sender history", nil); err != nil {
		t.Fatal(err)
	}
	relay := &personalAgentTool{backend: b, name: "send_to_agent"}
	args, _ := json.Marshal(map[string]string{"agent": target.ID, "message": "Send to agent: what is half?"})
	turn := withAgentTurn(ctx, sender.ConversationID, false)
	for i := 0; i < 2; i++ {
		result, err := relay.Invoke(turn, tools.Invocation{Arguments: args, IdempotencyKey: "same-relay"})
		if err != nil || result.Status != tools.StatusOK {
			t.Fatal(result, err)
		}
	}
	history := awaitAssistant(t, b, target.ConversationID, 1)
	if len(history) != 2 || history[0].Role != "user" || history[0].RelayedBy != sender.ID || history[1].Role != "assistant" {
		t.Fatalf("relay history: %+v", history)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one target model call, got %d", calls.Load())
	}
	got, _ := registry.Get(target.ID)
	if got.Unread != 2 {
		t.Fatalf("relay plus answer unread: %+v", got)
	}
	messages := 0
	for _, ev := range b.Bus.Replay(types.Cursor{}) {
		if ev.Type != types.EventMessageCreated {
			continue
		}
		var msg api.MessageEvent
		if err := json.Unmarshal(ev.Payload, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.ConversationID == types.ConversationID(target.ConversationID) && msg.Role == "user" {
			messages++
			if msg.RelayedBy != sender.ID {
				t.Fatal(msg)
			}
		}
	}
	if messages != 1 {
		t.Fatalf("user-rendered relay events: %d", messages)
	}
	denied, err := relay.Invoke(withAgentTurn(ctx, target.ConversationID, true), tools.Invocation{Arguments: args})
	if err != nil || denied.Status != tools.StatusDenied {
		t.Fatal(denied, err)
	}
	// Reopening the durable receipt boundary must not enqueue another turn.
	_, cs, err := OpenAgentConversations(ctx, db, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reboot := master.New(master.Config{}, cs, nil, nil, nil, nil, nil)
	id, duplicate, err := reboot.ReceiveRelay(ctx, "same-relay", target.ConversationID, "retry", sender.ID)
	if err != nil || !duplicate || id != history[0].ID || reboot.Turns().Active(target.ConversationID) {
		t.Fatal(id, duplicate, err)
	}
}

func TestPersonalAgentToolManagementSearchAndDeferredSurface(t *testing.T) {
	ctx := context.Background()
	b, r := personalBackend(t, nil)
	s, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := s.Catalog.PrefixHash()
	if err = wireAgentTools(b, s); err != nil {
		t.Fatal(err)
	}
	if s.Catalog.PrefixHash() != before {
		t.Fatal("deferred tools changed the stable core prefix")
	}
	for _, name := range []string{"manage_agents", "send_to_agent", "search_agent_chats"} {
		tool, err := s.Tools.Get(name)
		if err != nil || !s.Catalog.IsDeferred(name) || slices.Contains(tool.Definition().Profiles, "eval") {
			t.Fatalf("tool %s: %v", name, err)
		}
	}
	for _, request := range []string{"Hi, can you create an additional agent, please?", "Hola, ¿puedes crear un agente adicional, por favor?"} {
		activation := s.chatToolActivation(request, "test", nil)
		if !slices.Contains(activation.Names, "manage_agents") {
			t.Fatalf("missing agent intent for %q: %v", request, activation.Names)
		}
	}
	manage := &personalAgentTool{backend: b, name: "manage_agents"}
	result, err := manage.Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"action":"create","name":"Ada"}`)})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatal(result, err)
	}
	var a agents.PersonalAgent
	if err = json.Unmarshal([]byte(result.Output), &a); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if _, err = b.Master.SendAssistant(ctx, a.ConversationID, strings.Repeat("İ", 700)+"needle", nil); err != nil {
			t.Fatal(err)
		}
	}
	search, err := b.searchAgentChats("needle", "Ada")
	if err != nil || len(search.Snippets) != 20 || !search.Truncated {
		t.Fatal(search, err)
	}
	for _, hit := range search.Snippets {
		if hit.AgentID != a.ID || hit.AgentName != "Ada" || hit.At.IsZero() || len([]rune(hit.Text)) > 600 || !strings.Contains(hit.Text, "needle") {
			t.Fatal(hit)
		}
	}
	if _, err = b.searchAgentChats("", ""); err == nil {
		t.Fatal("accepted empty query")
	}
	if _, err = b.searchAgentChats("needle", "missing"); err == nil {
		t.Fatal("accepted missing agent")
	}
	if _, err = r.Archive(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := b.searchAgentChats("needle", a.ID); err != nil || len(got.Snippets) != 20 {
		t.Fatal(got, err)
	}
	if eval.DefaultEvalProfile().Hash() != evalProfileHash {
		t.Fatal("eval profile changed")
	}
	if _, _, err = b.Master.ReceiveRelay(ctx, "", a.ConversationID, "message", r.Favorite().ID); err == nil {
		t.Fatal("accepted empty receipt")
	}
}
