package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestDeliveryScreenshotChildCannotPublishCredentialPrompt(t *testing.T) {
	for _, site := range []string{"127.0.0.1", "accounts.example.com"} {
		t.Run(site, func(t *testing.T) {
			b, s := assistantTestWiring(t, false, nil)
			result := make(chan string, 1)
			fake := timingEndpoint{chat: func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
				child := false
				for _, message := range req.Messages {
					child = child || strings.Contains(message.Content, "Goal: Capture login screenshot")
				}
				if !child {
					for _, message := range req.Messages {
						if message.ToolResult != nil && message.ToolResult.Name == "subagent" {
							<-ctx.Done()
							return providers.ChatResponse{}, ctx.Err()
						}
					}
					args := json.RawMessage(`{"action":"spawn","card":{"name":"screenshot","description":"browser screenshot child","profile":"implementer","scope":"general","goal":"Capture login screenshot using browser login credentials","deliverable":"screenshot","done_criteria":["screenshot captured"],"report_contract":"summary"}}`)
					return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "spawn-screenshot", Name: "subagent", Arguments: args}}}}, nil
				}
				for _, message := range req.Messages {
					if message.ToolResult != nil && message.ToolResult.Name == "request_credentials" {
						select {
						case result <- message.ToolResult.Output:
						default:
						}
						<-ctx.Done()
						return providers.ChatResponse{}, ctx.Err()
					}
				}
				askDenied := false
				for _, message := range req.Messages {
					if message.ToolResult != nil && message.ToolResult.Name == "ask_user" {
						askDenied = strings.Contains(message.ToolResult.Output, "automatically denied")
					}
				}
				if !askDenied {
					return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "hidden-question", Name: "ask_user", Arguments: json.RawMessage(`{"question":"demo prompt sentinel"}`)}}}}, nil
				}
				args, _ := json.Marshal(credentialArgs{Site: site, Purpose: "Screenshot app login"})
				return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "login", Name: "request_credentials", Arguments: args}}}}, nil
			}}
			s.delegations.owner.endpoint = fake
			card := orchestrator.DelegationCard{Name: "app-worker", Description: "build app", Profile: agents.ProfileImplementer, Goal: "Implement the app", Deliverable: "full app", Scope: types.GeneralScope(), Done: []string{"verified"}, Report: "summary"}
			_, rec, err := s.Orch.Spawn(s.masterAgentID, card)
			if err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(map[string]any{"card": map[string]any{"goal": card.Goal, "deliverable": card.Deliverable, "done_criteria": card.Done}})
			// Both registry and channel callback must remain silent. A child would
			// otherwise send a secure-form link into this channel's conversation.
			s.delegations.spawn("cnv_delivery_login", t.TempDir(), args, string(rec.Run.ID), false, nil, delegationOptions{Channel: "telegram", Policy: deliveryPolicy{Autonomous: true}})
			select {
			case output := <-result:
				var denied credentialResult
				if json.Unmarshal([]byte(output), &denied) != nil || denied.Status != "denied" || denied.Guidance == "" {
					t.Fatal("screenshot child did not receive immediate guidance", output)
				}
				if (site == "127.0.0.1") != strings.Contains(denied.Guidance, "your own app") {
					t.Fatal(denied.Guidance)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("screenshot child blocked waiting for credentials")
			}
			s.delegations.stop()
			if len(s.Credentials.Pending("").Requests) != 0 {
				t.Fatal("delivery retained credential prompt")
			}
			for _, event := range b.Bus.Replay(types.Cursor{}) {
				if event.Type == api.EventCredentialsRequested {
					t.Fatal("delivery published credential form")
				}
			}
			for _, message := range b.Master.History("cnv_delivery_login", 0) {
				if strings.Contains(message.Text, "secure credential card") {
					t.Fatal("delivery sent credential question to channel")
				}
			}
			denials, results := 0, 0
			for _, run := range s.Store.ListRuns() {
				entries, err := s.Jrnl.List(context.Background(), string(run.Run.ID))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.Type == "user_prompt_denied" {
						denials++
					}
					if entry.Type == journal.TypeToolResult && strings.Contains(string(entry.Payload), "request_credentials") {
						results++
					}
				}
			}
			if denials != 2 || results != 1 {
				t.Fatalf("missing denial/tool result journal: denials=%d results=%d", denials, results)
			}
		})
	}
}

type failingPromptJournal struct{ journal.Store }

func (failingPromptJournal) Append(context.Context, journal.Entry) (journal.Entry, error) {
	return journal.Entry{}, errors.New("journal unavailable")
}

func TestDeliveryPromptDenialJournalFailureStaysClosed(t *testing.T) {
	ctx := withDeliveryPolicy(context.Background(), deliveryPolicy{Autonomous: true})
	ctx = context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{run: "child"})
	ctx = context.WithValue(ctx, promptJournalKey{}, journal.Store(failingPromptJournal{}))
	result, err := automaticPromptDenial(ctx, "ask_user")
	if err == nil || result.Status == "ok" {
		t.Fatal("failed journal authorized prompt", result, err)
	}
	registry := newCredentialRegistry(nil)
	defer registry.Close()
	if _, err := registry.request(ctx, credentialArgs{Site: "127.0.0.1"}); err == nil || len(registry.Pending("").Requests) != 0 {
		t.Fatal("credential prompt survived denial journal failure", err)
	}
}
