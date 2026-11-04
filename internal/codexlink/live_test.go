//go:build live

package codexlink

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
)

// TestLiveRelay performs exactly three account calls, with no retry or fallback.
func TestLiveRelay(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" || os.Getenv("EXCALIBUR_CODEX_SMOKE_MODEL") == "" {
		t.Skip("explicit live account smoke is disabled")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store := NewStore(t.TempDir())
	if !store.Status().Connected {
		t.Fatal("machine account sign-in is unavailable")
	}
	server, err := Listen(ctx, store, RelayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	p, err := openai.New(openai.Options{APIKey: server.Key(), BaseURL: server.BaseURL(), UseResponses: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	t.Setenv("EXCALIBUR_SERVICE_TIER", "fast")
	req := providers.ChatRequest{Model: os.Getenv("EXCALIBUR_CODEX_SMOKE_MODEL"), ReasoningEffort: "low", Messages: []providers.Message{{Role: providers.RoleUser, Content: "Reply with: ok"}}}
	result, err := p.Chat(ctx, req)
	if err != nil {
		t.Fatal("non-stream smoke failed:", err)
	}
	if strings.TrimSpace(result.Message.Content) != "ok" {
		t.Fatal("non-stream smoke returned unexpected text")
	}
	t.Logf("non-stream: ok; input=%d output=%d", result.Usage.Input, result.Usage.Output)
	var text strings.Builder
	done := false
	var usage providers.Usage
	err = p.Stream(ctx, req, func(event providers.StreamEvent) error {
		text.WriteString(event.Delta)
		done = done || event.Done
		if event.Usage != nil {
			usage = *event.Usage
		}
		return nil
	})
	if err != nil {
		t.Fatal("stream smoke failed:", err)
	}
	if strings.TrimSpace(text.String()) != "ok" || !done {
		t.Fatal("stream smoke returned unexpected text or missing terminal event")
	}
	t.Logf("stream: ok; input=%d output=%d", usage.Input, usage.Output)
	req.Messages = []providers.Message{{Role: providers.RoleUser, Content: "Call relay_check with value ok. Do not answer with text."}}
	req.Tools = []providers.ToolDefinition{{Name: "relay_check", Description: "Return a short smoke-check value.", Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)}}
	req.ToolChoice = "required"
	result, err = p.Chat(ctx, req)
	if err != nil {
		t.Fatal("tool smoke failed:", err)
	}
	if len(result.Message.ToolCalls) != 1 || result.Message.ToolCalls[0].Name != "relay_check" || result.Message.ToolCalls[0].ID == "" {
		t.Fatal("tool smoke returned no complete function call")
	}
	var args struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(result.Message.ToolCalls[0].Arguments, &args) != nil || args.Value != "ok" {
		t.Fatal("tool smoke returned invalid arguments")
	}
	t.Logf("function call: name, call ID and arguments preserved; input=%d output=%d", result.Usage.Input, result.Usage.Output)
}
