package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/codexlink"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestDaemonCodexProviderThroughGateway(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER", "codex")
	t.Setenv("OPENAI_API_KEY", "unrelated-test-key")
	t.Setenv("EXCALIBUR_API_KEY", "unrelated-operator-key")
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "unrelated-fallback-key")
	t.Setenv("EXCALIBUR_SERVICE_TIER", "fast")
	t.Setenv("EXCALIBUR_CODEX_AUTH_FILE", "")
	t.Setenv("HOME", t.TempDir())
	data := t.TempDir()
	store := codexlink.NewStore(data)
	if err := store.Save(codexlink.Tokens{AccessToken: "test-account-token", AccountID: "test-account"}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-account-token" {
			t.Error("deployment key replaced sign-in")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["model"] != "test-model" || body["instructions"] != "obey" || body["service_tier"] != "priority" || body["stream"] != true {
			t.Error("gateway request translation failed")
		}
		response := `{"id":"test-response","model":"test-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]},{"type":"function_call","call_id":"test-call","name":"inspect","arguments":"{}"}],"usage":{"input_tokens":4,"output_tokens":2}}`
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
	}))
	defer upstream.Close()
	settings, server, err := daemon.WireCodex(context.Background(), data, models.ModelSettings{Provider: "codex", Model: "test-model", ReasoningEffort: "low"}, config.ModelsConfig{}, codexlink.RelayOptions{UpstreamURL: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	provider, err := buildDaemonProvider(context.Background(), settings, server, func(context.Context, string) (string, error) {
		t.Error("account selection consulted vault")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	gateway := cli.BuildGateway(settings, config.Default(), provider)
	defer gateway.Close()
	response, _, err := gateway.Chat(context.Background(), types.RoleInterlocutor, providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleSystem, Content: "obey"}, {Role: providers.RoleUser, Content: "run"}}, Tools: []providers.ToolDefinition{{Name: "inspect", Schema: json.RawMessage(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "ok" || len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].ID != "test-call" || response.Usage.Input != 4 {
		t.Fatal("gateway lost function call or usage")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := buildDaemonProvider(canceled, settings, server, nil); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatal("cancellation ignored")
	}
}
