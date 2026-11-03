package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestChatBrowserPublishesUsableArguments(t *testing.T) {
	step := 0
	var session string
	b, _ := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		step++
		if step == 1 {
			for _, def := range req.Tools {
				if !strings.HasPrefix(def.Name, "browser_") {
					continue
				}
				var schema struct {
					Properties map[string]any `json:"properties"`
				}
				if json.Unmarshal(def.Schema, &schema) != nil || len(schema.Properties) == 0 {
					t.Errorf("%s has no concrete arguments", def.Name)
				}
				if def.Name == "browser_navigate" && (schema.Properties["session_id"] == nil || schema.Properties["url"] == nil) {
					t.Error("navigation arguments missing")
				}
			}
			return browserChatCall("browser_open_session", map[string]any{}), nil
		}
		var out map[string]any
		for _, m := range req.Messages {
			if m.ToolResult != nil {
				_ = json.Unmarshal([]byte(m.ToolResult.Output), &out)
			}
		}
		switch step {
		case 2:
			session, _ = out["session_id"].(string)
			if session == "" {
				t.Error("missing session")
			}
			return browserChatCall("browser_tabs", map[string]any{"session_id": session, "action": "new"}), nil
		case 3:
			return browserChatCall("browser_navigate", map[string]any{"session_id": session, "tab_id": out["tab_id"], "url": "https://example.com"}), nil
		default:
			if out["url"] != "https://example.com" {
				t.Error("navigation did not succeed")
			}
			return textReply("Page loaded."), nil
		}
	})
	if _, _, err := b.ConversationSend(context.Background(), "web", "browse-message", "browse-conversation", "Open browser and sign in"); err != nil {
		t.Fatal(err)
	}
	awaitAssistant(t, b, "browse-conversation", 1)
}

func browserChatCall(name string, args any) providers.ChatResponse {
	raw, _ := json.Marshal(args)
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "call-" + name, Name: name, Arguments: raw}}}}
}

func TestChatBrowserSessionLifecycle(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			s, err := Assemble(AssembleOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cap, _ := s.Caps.Lookup("browser")
			fake := cap.(*capabilities.BrowserFake)
			r := &assistantToolRunner{daemonToolRunner: daemonToolRunner{ex: s.Exec, reg: s.Tools}, runID: "lifecycle", allowed: map[string]bool{"browser_open_session": true, "browser_navigate": true, "browser_close_session": true}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			call := func(name string, args any, n int) tools.Result {
				t.Helper()
				raw, _ := json.Marshal(args)
				out, err := r.Execute(ctx, name, raw, tools.ExecuteOptions{IdempotencyKey: fmt.Sprint(n)})
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			var session, tab string
			for n := 0; n < 40; n++ {
				out := call("browser_open_session", map[string]any{}, n)
				var opened map[string]string
				if out.Status != tools.StatusOK || json.Unmarshal([]byte(out.Output), &opened) != nil {
					t.Fatal("open failed")
				}
				if n == 0 {
					session, tab = opened["session_id"], opened["tab_id"]
				}
				if session == "" || tab == "" || session != opened["session_id"] || tab != opened["tab_id"] {
					t.Fatal("session was not reused with its initial tab")
				}
			}
			if fake.Calls("browser_open_session") != 1 || fake.Calls("browser_tabs") != 1 {
				t.Fatal("repeated calls leaked sessions or tabs")
			}
			for _, tc := range []struct {
				args   map[string]any
				reason string
			}{
				{map[string]any{"url": "https://example.com"}, "session_id is required"},
				{map[string]any{"session_id": "another-turn", "url": "https://example.com"}, "unknown session for this turn"},
				{map[string]any{"session_id": session, "tab_id": "absent", "url": "https://example.com"}, "tab does not exist in this session"},
			} {
				out := call("browser_navigate", tc.args, 100+len(tc.reason))
				if out.Status != tools.StatusInputError || !strings.Contains(out.Output, tc.reason) {
					t.Fatalf("missing concrete reason: %s", out.Output)
				}
			}
			out := call("browser_navigate", map[string]any{"session_id": session, "url": "https://example.com"}, 200)
			if out.Status != tools.StatusOK {
				t.Fatalf("initial tab unusable: %s", out.Output)
			}
			if cancelled {
				cancel()
			}
			r.closeBrowser(ctx)
			if fake.Calls("browser_close_session") != 1 {
				t.Fatal("turn did not close its session")
			}
			r.closeBrowser(ctx)
			if fake.Calls("browser_close_session") != 1 {
				t.Fatal("cleanup repeated")
			}
		})
	}
}

type failedChatTab struct{ tools.Tool }

func (t failedChatTab) Invoke(context.Context, tools.Invocation) (tools.Result, error) {
	return tools.Result{Status: tools.StatusToolError, Output: "browser: tab creation failed"}, nil
}

func TestChatBrowserClosesSessionWhenInitialTabFails(t *testing.T) {
	s, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tab, _ := s.Tools.Get("browser_tabs")
	s.Tools.Unregister("browser_tabs")
	if err := s.Tools.Register(failedChatTab{tab}); err != nil {
		t.Fatal(err)
	}
	cap, _ := s.Caps.Lookup("browser")
	fake := cap.(*capabilities.BrowserFake)
	r := &assistantToolRunner{daemonToolRunner: daemonToolRunner{ex: s.Exec, reg: s.Tools}, runID: "failed-tab", allowed: map[string]bool{"browser_open_session": true}}
	out, err := r.Execute(context.Background(), "browser_open_session", json.RawMessage(`{}`), tools.ExecuteOptions{IdempotencyKey: "open"})
	if err != nil || out.Status != tools.StatusToolError || r.browser.session != "" || fake.Calls("browser_close_session") != 1 {
		t.Fatal("failed initial tab retained its session")
	}
}
