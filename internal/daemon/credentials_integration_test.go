package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Exercise the public RPC/event transport and real chat engine together. Only
// model inference is simulated; credentials use the real encrypted vault.
func TestCredentialDaemonRememberedFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var mu sync.Mutex
	var observed []providers.ChatRequest
	b, services := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		mu.Lock()
		observed = append(observed, req)
		step := len(observed)
		mu.Unlock()
		if step%2 == 1 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "login", Name: "request_credentials", Arguments: json.RawMessage(`{"site":"accounts.example.test","purpose":"Sign in","remember_default":true}`)}}}}, nil
		}
		return textReply("Secure sign in is ready."), nil
	})
	v, _ := openTestVault(t)
	services.Credentials.vault = v
	b.Services.Vault = v
	t.Cleanup(services.Credentials.Close)
	gate := gateway.New(gateway.DefaultConfig(), b, gateway.NewMemoryBlobs(), b.Bus)
	addr, err := gate.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gate.Stop(context.Background()) })
	client := gateway.Dial(addr, "credential-integration")
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		raw, err := client.Call(ctx, method, method, params)
		if err != nil {
			t.Fatalf("%s failed: %v", method, err)
		}
		return raw
	}
	events := make(chan types.Event, 4)
	subscribed := make(chan struct{})
	safego.Go("credential-integration-events", nil, func() {
		defer close(subscribed)
		_ = client.Subscribe(ctx, types.Cursor{}, []string{string(api.EventCredentialsRequested), string(api.EventCredentialsResolved)}, func(event types.Event) {
			select {
			case events <- event:
			case <-ctx.Done():
			}
		})
	})
	t.Cleanup(func() {
		cancel()
		select {
		case <-subscribed:
		case <-time.After(time.Second):
			t.Error("event subscriber did not stop")
		}
	})
	const conversation = "credential-integration-conversation"
	call(api.MethodVaultLock, struct{}{})
	call(api.MethodConversationSend, api.ConversationSendParams{ConversationID: conversation, ClientMsgID: "first-login", Text: "Sign in to accounts.example.test"})
	var requested api.CredentialsRequestedEvent
	select {
	case event := <-events:
		if event.Type != api.EventCredentialsRequested || json.Unmarshal(event.Payload, &requested) != nil || requested.Site != "accounts.example.test" || requested.ConversationID != conversation || requested.RunID == "" {
			t.Fatal("requested event omitted origin metadata")
		}
	case <-ctx.Done():
		t.Fatal("agent did not publish a credential request")
	}
	pending := func() api.CredentialsPendingResult {
		t.Helper()
		var out api.CredentialsPendingResult
		if err := json.Unmarshal(call(api.MethodCredentialsPending, api.CredentialsPendingParams{ConversationID: ""}), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	params := api.CredentialsSubmitParams{RequestID: requested.RequestID, Values: map[string]string{"username": "integration-user-sentinel", "password": "integration-password-sentinel"}, Remember: true}
	_, err = client.Call(ctx, "locked-submit", api.MethodCredentialsSubmit, params)
	var rpcErr *api.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != api.CodeAppError {
		t.Fatal("locked submission did not return a numeric JSON-RPC application error")
	}
	data, _ := json.Marshal(rpcErr.Data)
	var details struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(data, &details) != nil || details.Code != "vault_locked" {
		t.Fatal("locked submission omitted data.code vault_locked")
	}
	if p := pending(); len(p.Requests) != 1 || p.Requests[0].ID != requested.RequestID || p.Requests[0].Status != "pending" {
		t.Fatal("locked submission consumed pending request")
	}
	call(api.MethodVaultUnlock, map[string]string{"passphrase": testPassphrase})
	var submitted api.CredentialsOKResult
	if json.Unmarshal(call(api.MethodCredentialsSubmit, params), &submitted) != nil || !submitted.OK {
		t.Fatal("secure submission failed")
	}
	select {
	case event := <-events:
		var resolved api.CredentialsResolvedEvent
		if event.Type != api.EventCredentialsResolved || json.Unmarshal(event.Payload, &resolved) != nil || resolved.RequestID != requested.RequestID || resolved.Status != "provided" {
			t.Fatal("resolution event did not match submitted request")
		}
	case <-ctx.Done():
		t.Fatal("missing resolution event")
	}
	awaitAssistant(t, b, conversation, 1)
	call(api.MethodConversationSend, api.ConversationSendParams{ConversationID: conversation, ClientMsgID: "second-login", Text: "Sign in to accounts.example.test again"})
	awaitAssistant(t, b, conversation, 2)
	if len(pending().Requests) != 0 {
		t.Fatal("remembered request asked for credentials again")
	}
	mu.Lock()
	modelRequests := append([]providers.ChatRequest{}, observed...)
	mu.Unlock()
	if len(modelRequests) != 4 {
		t.Fatal("agent did not complete both credential turns")
	}
	var refs map[string]string
	for i, expected := range map[int]string{1: "provided", 3: "stored"} {
		var result credentialResult
		found := false
		for _, message := range modelRequests[i].Messages {
			if message.ToolResult != nil && message.ToolResult.Name == "request_credentials" {
				if json.Unmarshal([]byte(message.ToolResult.Output), &result) == nil && result.Status == expected {
					found = true
				}
			}
		}
		if !found || len(result.Refs) != 2 || !strings.HasPrefix(result.Refs["password"], "secret://") {
			t.Fatalf("model did not receive %s credential references", expected)
		}
		if refs == nil {
			refs = result.Refs
		} else if refs["password"] != result.Refs["password"] || refs["username"] != result.Refs["username"] {
			t.Fatal("remembered tool returned different refs")
		}
	}
	// Inspect public history, every journal entry for both runs, model context,
	// and every event. Plaintext values may exist only inside the secure call.
	all := []any{modelRequests, call(api.MethodConversationHistory, api.ConversationHistoryParams{ConversationID: conversation})}
	requestedCount := 0
	for _, event := range b.Bus.Replay(types.Cursor{}) {
		all = append(all, event)
		if event.Type == api.EventCredentialsRequested {
			requestedCount++
		}
		if event.RunID != nil {
			entries, err := services.Jrnl.List(ctx, string(*event.RunID))
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, entries)
		}
	}
	if requestedCount != 1 {
		t.Fatal("remembered reuse emitted a new request")
	}
	encoded, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range params.Values {
		if strings.Contains(string(encoded), value) {
			t.Fatal("plaintext reached history, journal, model context or events")
		}
	}
	entries, err := v.ListMetadata(ctx)
	if err != nil || len(entries) != 2 {
		t.Fatal("remembered credential entries missing after run end")
	}
	for _, entry := range entries {
		if entry.ExpiresAt != nil || len(entry.Domains) != 1 || entry.Domains[0] != requested.Site || len(entry.AllowedTools) != 1 || entry.AllowedTools[0] != "browser_act" {
			t.Fatal("remembered credential policy changed")
		}
	}
}
