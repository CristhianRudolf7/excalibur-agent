package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestDelegatedPublicPreviewUsesSharedManagerAndUserAuthorization(t *testing.T) {
	fakePreviewBinary(t, `printf 'https://test-preview.trycloudflare.com\n' >&2
exec sleep 120`)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<title>Sales and inventory</title>App fixture")
	}))
	defer app.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(app.URL, "http://"))
	number, _ := strconv.Atoi(port)
	calls := 0
	fake := timingEndpoint{chat: func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if calls == 1 {
			found := false
			for _, tool := range req.Tools {
				if tool.Name == "public_preview" {
					found = true
				}
			}
			if !found {
				t.Error("authorized delegated registry lacks public_preview")
			}
			args, _ := json.Marshal(previewRequest{Action: "publish", Port: number})
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "publish-app", Name: "public_preview", Arguments: args}}}}, nil
		}
		var output string
		for _, message := range req.Messages {
			if message.ToolResult != nil && message.ToolResult.CallID == "publish-app" {
				output = message.ToolResult.Output
			}
		}
		if !strings.Contains(output, "https://test-preview.trycloudflare.com") {
			t.Error("delegated publish failed", output)
		}
		return textReply("App verified and published."), nil
	}}
	b, s := assistantTestWiring(t, false, nil)
	s.delegations.owner.endpoint = fake
	s.previews.dataDir = t.TempDir()
	s.previews.client = &http.Client{Transport: previewTransport(func(*http.Request) (*http.Response, error) { return http.Get(app.URL) })}
	t.Cleanup(s.previews.Close)
	// Deliberately omit all preview keywords from the goal: authorization must
	// grant the daemon tool independently of lexical discovery ranking.
	card := orchestrator.DelegationCard{Name: "app-worker", Description: "test worker", Profile: agents.ProfileImplementer, Goal: "check fixture", Deliverable: "report", Scope: types.GeneralScope(), Done: []string{"checked"}, Report: "summary"}
	_, rec, err := s.Orch.Spawn(s.masterAgentID, card)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"card": map[string]any{"goal": card.Goal, "deliverable": card.Deliverable, "done_criteria": card.Done}})
	policy := deliveryPolicyForTurns(nil, master.Normalized{Text: "Pásame un enlace de Cloudflare"})
	s.delegations.run("cnv_delegated_preview", t.TempDir(), args, string(rec.Run.ID), false, nil, nil, delegationOptions{ParentRunID: "parent", TurnID: "turn", Policy: policy})
	state, err := s.Orch.RunState(rec.Run.ID)
	if err != nil || state != types.RunCompleted || calls != 2 {
		t.Fatal(state, calls, err)
	}
	history := b.Master.History("cnv_delegated_preview", 0)
	if len(history) != 1 || strings.ContainsAny(history[0].Text, "?¿") || !strings.Contains(history[0].Text, "https://test-preview.trycloudflare.com") {
		t.Fatal(history)
	}
	if len(s.previews.list()) != 1 {
		t.Fatal("child used a private preview manager")
	}
	called, completed := false, false
	for _, event := range b.Bus.Replay(types.Cursor{}) {
		if event.Type != types.EventToolCalled && event.Type != types.EventToolCompleted {
			continue
		}
		var call api.ToolCallEvent
		if err := json.Unmarshal(event.Payload, &call); err != nil {
			t.Fatal(err)
		}
		if call.Tool != "public_preview" {
			continue
		}
		if call.ConversationID != "cnv_delegated_preview" || call.RunID != rec.Run.ID || call.ParentRunID != "parent" || call.TurnID != "turn" {
			t.Fatalf("authorized preview lost coding association: %+v", call)
		}
		called = called || event.Type == types.EventToolCalled
		completed = completed || event.Type == types.EventToolCompleted
	}
	if !called || !completed {
		t.Fatalf("missing authorized preview activity: called=%v completed=%v", called, completed)
	}
}
