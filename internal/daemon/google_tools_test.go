package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/googleapi"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type testGoogleAccount struct{}

func (testGoogleAccount) Status() googleapi.Status {
	return googleapi.Status{Connected: true, Email: "fake@example.test"}
}
func (testGoogleAccount) AccessToken(context.Context) (string, error) { return "fake-token", nil }

func TestGoogleToolsStableDeferredAndDefaultOnly(t *testing.T) {
	b, s := assistantTestWiring(t, false, nil)
	before := s.Catalog.PrefixHash()
	for _, name := range []string{"gmail", "drive", "docs"} {
		tl, err := s.Tools.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		if !s.Catalog.IsDeferred(name) || !slices.Equal(tl.Definition().Profiles, []string{"default"}) {
			t.Fatalf("%s escaped daemon/default deferred catalog", name)
		}
		out, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"read","id":"fake"}`)})
		if err == nil || !strings.Contains(out.Output, "Connect Google") {
			t.Fatal(out, err)
		}
	}
	if err := wireGoogleTools(b, s); err != nil {
		t.Fatal("repeat wiring", err)
	}
	if before != s.Catalog.PrefixHash() {
		t.Fatal("Google registration moved cached core prefix")
	}
	// Changing the account behind the stable provider proxy requires no rewire.
	s.Google = testGoogleAccount{}
	p := googleAccount{s}
	token, err := p.AccessToken(context.Background())
	if err != nil || token != "fake-token" || !p.Status().Connected {
		t.Fatal("daemon provider not followed")
	}
}
func TestGoogleToolsVisibleAndExecutableInChat(t *testing.T) {
	calls := 0
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		for _, name := range []string{"gmail", "drive", "docs"} {
			if !slices.ContainsFunc(req.Tools, func(d providers.ToolDefinition) bool { return d.Name == name }) {
				t.Errorf("chat omitted %s", name)
			}
		}
		if calls == 1 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "google-read", Name: "gmail", Arguments: json.RawMessage(`{"action":"read","id":"m1"}`)}}}}, nil
		}
		found := false
		for _, message := range req.Messages {
			if message.ToolResult != nil && message.ToolResult.CallID == "google-read" {
				found = strings.Contains(message.ToolResult.Output, "Connect Google")
			}
		}
		if !found {
			t.Error("not-connected tool result missing from model history")
		}
		return textReply("Open Settings to connect Google."), nil
	})
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	if _, _, err := a.CompleteTurn(context.Background(), "cnv_google", master.Normalized{Text: "Read my Gmail"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("model calls = %d", calls)
	}
}
