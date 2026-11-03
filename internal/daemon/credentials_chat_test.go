package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestCredentialChatJournalHistoryAndModelPrivacy(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var observed []providers.ChatRequest
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		mu.Lock()
		observed = append(observed, req)
		step := len(observed)
		mu.Unlock()
		if step == 1 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "secure-login", Name: "request_credentials", Arguments: json.RawMessage(`{"site":"example.com","purpose":"Sign in"}`)}}}}, nil
		}
		return textReply("The secure form was completed."), nil
	})
	v, _ := openTestVault(t)
	s.Credentials.vault = v
	t.Cleanup(s.Credentials.Close)
	if _, _, err := b.ConversationSend(ctx, "web", "secure-message", "secure-conversation", "Sign in to example.com"); err != nil {
		t.Fatal(err)
	}
	var request api.CredentialRequest
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		requests := s.Credentials.Pending("secure-conversation").Requests
		if len(requests) == 1 {
			request = requests[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if request.ID == "" {
		t.Fatal("assistant did not request credentials")
	}
	// Simulate a reloaded client: the separate RPC must recover pending metadata.
	pendingRaw, _ := json.Marshal(api.CredentialsPendingParams{ConversationID: "secure-conversation"})
	pending, err := b.Passthrough(ctx, api.MethodCredentialsPending, pendingRaw)
	if err != nil || len(pending.(api.CredentialsPendingResult).Requests) != 1 {
		t.Fatal("pending metadata unavailable after reload")
	}
	raw, _ := json.Marshal(api.CredentialsSubmitParams{RequestID: request.ID, Values: map[string]string{"username": "chat-user-private-sentinel", "password": "chat-password-private-sentinel"}})
	if _, err := b.Passthrough(ctx, api.MethodCredentialsSubmit, raw); err != nil {
		t.Fatal(err)
	}
	if strings.Trim(string(raw), "\x00") != "" {
		t.Fatal("submit transport buffer not zeroed")
	}
	history := awaitAssistant(t, b, "secure-conversation", 1)
	mu.Lock()
	modelRequests := append([]providers.ChatRequest{}, observed...)
	mu.Unlock()
	if len(modelRequests) != 2 {
		t.Fatal("waiting tool did not resume model")
	}
	var all []any
	all = append(all, history, modelRequests, b.Bus.Replay(types.Cursor{}))
	runID := ""
	for _, event := range b.Bus.Replay(types.Cursor{}) {
		if event.Type == api.EventCredentialsRequested {
			var e api.CredentialsRequestedEvent
			_ = json.Unmarshal(event.Payload, &e)
			runID = e.RunID
		}
	}
	if runID == "" {
		t.Fatal("credential event omitted origin run")
	}
	entries, err := s.Jrnl.List(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	all = append(all, entries)
	encoded, _ := json.Marshal(all)
	for _, secret := range []string{"chat-user-private-sentinel", "chat-password-private-sentinel"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("credential reached journal, history, model context or events")
		}
	}
	// The model and journal should still receive usable references.
	if !strings.Contains(string(encoded), "secret://credentials-") {
		t.Fatal("credential refs missing from tool observation")
	}
	md, err := v.ListMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range md {
		if entry.Meta["credential_run"] != "" {
			t.Fatal("completed assistant turn retained ephemeral credentials")
		}
	}
}

func TestCredentialChatRespectsTurnDeadline(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_TIMEOUT", "100ms")
	b, s := assistantTestWiring(t, false, func(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "deadline-login", Name: "request_credentials", Arguments: json.RawMessage(`{"site":"example.com"}`)}}}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	started := time.Now()
	_, _, _ = a.CompleteTurn(ctx, "deadline-conversation", master.Normalized{Text: "Sign in to example.com"})
	if time.Since(started) >= time.Second {
		t.Fatal("credential wait ignored operator turn deadline")
	}
	if len(s.Credentials.Pending("deadline-conversation").Requests) != 0 {
		t.Fatal("deadline left request pending")
	}
}
