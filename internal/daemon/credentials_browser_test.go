package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type credentialBrowserStub struct {
	tools.Tool
	output string
	called bool
}

func (t *credentialBrowserStub) Invoke(context.Context, tools.Invocation) (tools.Result, error) {
	t.called = true
	return tools.Result{Status: tools.StatusOK, Output: t.output}, nil
}

func TestCredentialBrowserGuidance(t *testing.T) {
	for _, block := range []string{capabilities.BlockedLogin, capabilities.Blocked2FA, capabilities.BlockedCaptcha, capabilities.BlockedBySite} {
		stub := &credentialBrowserStub{output: "input_required: " + block}
		wrapper := &credentialBrowserTool{Tool: stub}
		out, err := wrapper.Invoke(context.Background(), tools.Invocation{Tool: "browser_navigate"})
		if err != nil {
			t.Fatal(err)
		}
		credentialBlock := block == capabilities.BlockedLogin || block == capabilities.Blocked2FA
		if strings.Contains(out.Output, "request_credentials") != credentialBlock {
			t.Fatalf("guidance: %s", out.Output)
		}
		if credentialBlock {
			for _, want := range []string{"exact page host", "secret://", "browser_snapshot", "never request values in chat"} {
				if !strings.Contains(out.Output, want) {
					t.Errorf("missing %s", want)
				}
			}
		}
	}
}

func TestCredentialBrowserRunScope(t *testing.T) {
	r, _, _ := credentialTestRegistry(t)
	event, result := startCredentialRequest(t, r, context.Background(), credentialArgs{Site: "example.com"})
	if err := r.Submit(context.Background(), event.RequestID, credentialTestValues(), false); err != nil {
		t.Fatal(err)
	}
	out := <-result
	for _, run := range []string{"", "other", "run"} {
		stub := &credentialBrowserStub{output: `{"ok":true}`}
		wrapper := &credentialBrowserTool{Tool: stub, registry: r}
		ctx := context.WithValue(context.Background(), credentialOriginKey{}, credentialOrigin{run: run})
		args, _ := json.Marshal(map[string]string{"value": out.Refs["password"]})
		got, err := wrapper.Invoke(ctx, tools.Invocation{Tool: "browser_act", Arguments: args})
		if err != nil || stub.called != (run == "run") || (got.Status == tools.StatusDenied) != (run != "run") {
			t.Fatalf("scope %q: %+v %v", run, got, err)
		}
	}
}

func TestCredentialBrowserReadyLoginForm(t *testing.T) {
	for _, block := range []string{capabilities.BlockedLogin, capabilities.Blocked2FA, capabilities.BlockedCaptcha, capabilities.BlockedBySite} {
		stub := &credentialBrowserStub{output: "input_required: " + block}
		wrapper := &credentialBrowserTool{Tool: stub}
		out, err := wrapper.Invoke(context.Background(), tools.Invocation{Tool: "browser_wait_ready"})
		if err != nil || out.Status != tools.StatusOK {
			t.Fatal("readiness failed")
		}
		if block == capabilities.BlockedLogin || block == capabilities.Blocked2FA {
			var ready struct {
				Ready          bool   `json:"ready"`
				Authentication string `json:"authentication"`
				Next           string `json:"next"`
			}
			if json.Unmarshal([]byte(out.Output), &ready) != nil || !ready.Ready || ready.Authentication != block {
				t.Fatal("loaded credential form did not permit observation")
			}
			for _, want := range []string{"browser_snapshot now", "only if you do not already have", "without port", "never request values in chat"} {
				if !strings.Contains(ready.Next, want) {
					t.Errorf("readiness guidance omitted %s", want)
				}
			}
		} else if out.Output != stub.output {
			t.Fatal("CAPTCHA or site block was marked ready")
		}
	}
}

func TestCredentialBrowserAcknowledgesCompletedFormAction(t *testing.T) {
	for _, block := range []string{capabilities.BlockedLogin, capabilities.Blocked2FA, capabilities.BlockedCaptcha, capabilities.BlockedBySite} {
		stub := &credentialBrowserStub{output: "input_required: " + block}
		wrapper := &credentialBrowserTool{Tool: stub}
		out, err := wrapper.Invoke(context.Background(), tools.Invocation{Tool: "browser_act", Arguments: json.RawMessage(`{"action":"type","ref":"r2","value":"secret://credentials-test.username"}`)})
		if err != nil || out.Status != tools.StatusOK {
			t.Fatal("form action failed")
		}
		if block == capabilities.BlockedLogin || block == capabilities.Blocked2FA {
			var completed struct {
				Action         string `json:"action"`
				Ref            string `json:"ref"`
				Resnapshot     bool   `json:"resnapshot"`
				Authentication string `json:"authentication"`
				Next           string `json:"next"`
			}
			if json.Unmarshal([]byte(out.Output), &completed) != nil || completed.Action != "type" || completed.Ref != "r2" || !completed.Resnapshot || completed.Authentication != block || !strings.Contains(completed.Next, "remaining fields or click the submit button") {
				t.Fatal("successful form action was reported as an unresolved action")
			}
		} else if out.Output != stub.output {
			t.Fatal("CAPTCHA or site block was reported as a successful form action")
		}
	}
}

func TestCredentialChatActivation(t *testing.T) {
	s, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range []string{"login", "sign in", "iniciar sesión", "contraseña", "password", "credentials", "browser"} {
		activated := s.chatToolActivation(intent, "test", nil)
		present := map[string]bool{}
		for _, name := range activated.Names {
			present[name] = true
		}
		for _, name := range []string{"request_credentials", "browser_open_session", "browser_navigate", "browser_wait_ready", "browser_snapshot", "browser_act"} {
			if !present[name] {
				t.Errorf("%q lacks %s", intent, name)
			}
		}
	}
}

func TestCredentialBrowserOwnAppGuidance(t *testing.T) {
	for _, tc := range []struct {
		name, page, requested, workspace string
		delivery, own                    bool
	}{
		{"delivery localhost", "http://127.0.0.1:8123/login", "http://127.0.0.1:8123/login", "", true, true},
		{"unowned private site", "http://10.2.3.4:8123/login", "", "/workspace/app", false, false},
		{"workspace ipv6", "http://[::1]:8123/login", "", "/workspace/app", false, true},
		{"interactive localhost", "http://localhost:8123/login", "", "", false, false},
		{"interactive external", "https://accounts.example.com/login", "", "", false, false},
		{"delivery external", "https://accounts.example.com/login", "", "", true, false},
		{"redirect external", "https://accounts.example.com/login", "http://127.0.0.1:8123/login", "", true, false},
		{"redirect own app", "http://127.0.0.1:8123/login", "https://example.com", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"browser_navigate", "browser_wait_ready", "browser_act"} {
				for _, block := range []string{capabilities.BlockedLogin, capabilities.Blocked2FA} {
					ctx := withDeliveryPolicy(context.Background(), deliveryPolicy{Autonomous: tc.delivery})
					ctx = context.WithValue(ctx, workspaceAppKey{}, tc.workspace)
					listing, _ := json.Marshal(map[string]any{"tabs": []map[string]string{{"tab_id": "tab", "url": tc.page}}})
					tabs := &credentialBrowserStub{output: string(listing)}
					stub := &credentialBrowserStub{output: "input_required: " + block}
					wrapper := &credentialBrowserTool{Tool: stub, tabs: tabs}
					args, _ := json.Marshal(map[string]string{"session_id": "session", "tab_id": "tab", "url": tc.requested, "action": "type"})
					out, err := wrapper.Invoke(ctx, tools.Invocation{Tool: name, Arguments: args})
					if err != nil || !tabs.called || strings.Contains(out.Output, "your own app") != tc.own {
						t.Fatal(name, out, err)
					}
					if tc.own {
						for _, want := range []string{"README/seed", ".env.example", "directly", "browser_act"} {
							if !strings.Contains(out.Output, want) {
								t.Error("own app guidance missing", want)
							}
						}
						if strings.Contains(out.Output, "request_credentials") || strings.Contains(out.Output, "returned secret://") {
							t.Fatal("own app hint requests user secrets", out.Output)
						}
					} else if tc.delivery {
						if !strings.Contains(out.Output, "forms are disabled") || strings.Contains(out.Output, "Wait for the secure web form") {
							t.Fatal("delivery hint invites a prompt", out.Output)
						}
					} else if !strings.Contains(out.Output, "Wait for the secure web form") {
						t.Fatal("interactive hint changed", out.Output)
					}
				}
			}
		})
	}
}

func TestCredentialBrowserDoesNotGuessAmbiguousOrigin(t *testing.T) {
	tabs := &credentialBrowserStub{output: `{"tabs":[{"tab_id":"a","url":"http://localhost:8123/login"},{"tab_id":"b","url":"https://example.com/login"}]}`}
	wrapper := &credentialBrowserTool{Tool: &credentialBrowserStub{output: "input_required: " + capabilities.BlockedLogin}, tabs: tabs}
	ctx := withDeliveryPolicy(context.Background(), deliveryPolicy{Autonomous: true})
	out, err := wrapper.Invoke(ctx, tools.Invocation{Tool: "browser_navigate", Arguments: json.RawMessage(`{"session_id":"session","url":"http://localhost:8123/login"}`)})
	if err != nil || strings.Contains(out.Output, "your own app") || !strings.Contains(out.Output, "forms are disabled") {
		t.Fatal(out, err)
	}
}
