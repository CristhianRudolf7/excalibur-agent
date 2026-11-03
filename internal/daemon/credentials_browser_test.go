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
