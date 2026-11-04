package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestCredentialPromptPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, site, workspace string
		delivery, denied, own bool
	}{
		{"delivery loopback", "127.0.0.1", "", true, true, true},
		{"delivery ipv6", "::1", "", true, true, true},
		{"delivery localhost", "app.localhost", "", true, true, true},
		{"delivery private", "192.168.1.8", "", true, true, false},
		{"delivery external", "accounts.example.com", "", true, true, false},
		{"workspace loopback", "127.0.0.1", "/workspace/app", false, true, true},
		{"workspace private", "10.0.0.8", "/workspace/app", false, false, false},
		{"interactive loopback", "127.0.0.1", "", false, false, false},
		{"interactive external", "accounts.example.com", "", false, false, false},
		{"workspace external", "accounts.example.com", "/workspace/app", false, false, false},
		{"external localhost suffix", "localhost.example.com", "", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := telemetry.NewBus(nil)
			r := newCredentialRegistry(bus)
			defer r.Close()
			store := journal.NewMemStore()
			published := 0
			ctx := withDeliveryPolicy(context.Background(), deliveryPolicy{Autonomous: tc.delivery})
			ctx = context.WithValue(ctx, workspaceAppKey{}, tc.workspace)
			ctx = context.WithValue(ctx, promptJournalKey{}, journal.Store(store))
			ctx = context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{conversation: "conversation", run: "run", requested: func(e api.CredentialsRequestedEvent) {
				published++
				if err := r.Cancel(e.RequestID); err != nil {
					t.Error(err)
				}
			}})
			// Cancellation bounds a regression without mistaking it for a denial.
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			started := time.Now()
			raw, _ := json.Marshal(credentialArgs{Site: tc.site})
			out, err := (&credentialsTool{registry: r}).Invoke(ctx, tools.Invocation{Tool: "request_credentials", Arguments: raw})
			var result credentialResult
			if err != nil || json.Unmarshal([]byte(out.Output), &result) != nil || out.Status != tools.StatusOK {
				t.Fatal(out, err)
			}
			if tc.denied {
				if result.Status != "denied" || result.Refs != nil || time.Since(started) >= time.Second || published != 0 {
					t.Fatalf("prompt did not immediately deny: %+v, callbacks=%d", result, published)
				}
				if strings.Contains(result.Guidance, "your own app") != tc.own || result.Guidance == "" {
					t.Fatal(result.Guidance)
				}
				if len(bus.Replay(types.Cursor{})) != 0 || len(r.Pending("").Requests) != 0 {
					t.Fatal("denied prompt was published or retained")
				}
			} else if result.Status != "cancelled" || published != 1 || len(bus.Replay(types.Cursor{})) != 2 {
				t.Fatalf("interactive secure form changed: %+v, callbacks=%d", result, published)
			}
			entries, err := store.List(ctx, "run")
			if err != nil || len(entries) != boolCount(tc.denied) {
				t.Fatalf("denial journal: %+v %v", entries, err)
			}
			if tc.denied && (entries[0].Type != "user_prompt_denied" || !strings.Contains(string(entries[0].Payload), "request_credentials")) {
				t.Fatal(entries)
			}
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

type promptFixtureTool struct {
	codingFixtureTool
	name, group string
}

func (p promptFixtureTool) Definition() tools.Definition {
	d := p.codingFixtureTool.Definition()
	d.Name, d.Group = p.name, p.group
	return d
}

func TestDeliveryPromptToolsAndResults(t *testing.T) {
	for _, delivery := range []bool{false, true} {
		for _, tc := range []struct {
			name, group, output string
			status              tools.ResultStatus
			preempt             bool
		}{
			{"ask_user", "conversation", "question-published", tools.StatusOK, true},
			{"request_confirmation", "", "confirmation-published", tools.StatusOK, true},
			{"fixture", "question", "question-published", tools.StatusOK, true},
			{"fixture", "confirmation", "confirmation-published", tools.StatusOK, true},
			{"fixture", "", `{"confirmation_required":true}`, tools.StatusDenied, false},
			{"fixture", "", `{"requires_confirmation":true}`, tools.StatusOK, false},
			{"fixture", "", `{"status":"needs_user_input"}`, tools.StatusOK, false},
			{"fixture", "", "input_required: approval", tools.StatusOK, false},
			{"fixture", "", "Confirmation required. Ask for explicit approval in chat.", tools.StatusDenied, false},
			{"browser_navigate", "", "input_required: captcha", tools.StatusOK, false},
		} {
			t.Run(tc.name+"/"+tc.group+"/"+tc.output+"/"+stringBool(delivery), func(t *testing.T) {
				called := 0
				reg := tools.NewMapRegistry()
				tool := promptFixtureTool{name: tc.name, group: tc.group, codingFixtureTool: codingFixtureTool{invoke: func() (tools.Result, error) {
					called++
					return tools.Result{Status: tc.status, Output: tc.output}, nil
				}}}
				if err := reg.Register(tool); err != nil {
					t.Fatal(err)
				}
				store := journal.NewMemStore()
				ctx := withDeliveryPolicy(context.Background(), deliveryPolicy{Autonomous: delivery})
				ctx = context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{run: "child"})
				ctx = context.WithValue(ctx, promptJournalKey{}, journal.Store(store))
				runner := &daemonToolRunner{reg: reg, ex: tools.NewExecutor(reg)}
				out, err := runner.Execute(ctx, tc.name, json.RawMessage(`{}`), tools.ExecuteOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if delivery {
					if out.Status != tools.StatusDenied || out.Output != autonomousPromptGuidance || called != boolCount(!tc.preempt) {
						t.Fatalf("prompt guard: %+v called=%d", out, called)
					}
				} else if called != 1 || out.Output != tc.output || out.Status != tc.status {
					t.Fatalf("interactive tool changed: %+v called=%d", out, called)
				}
				entries, _ := store.List(ctx, "child")
				if len(entries) != boolCount(delivery) {
					t.Fatalf("denial not journaled: %+v", entries)
				}
			})
		}
	}
}

func stringBool(value bool) string {
	if value {
		return "delivery"
	}
	return "interactive"
}

func TestDeliveryPromptGuardPreservesActionableLoginAndOrdinaryData(t *testing.T) {
	for _, output := range []string{
		"input_required: needs_login. Use workspace demo credentials directly.",
		"input_required: needs_2fa. Use existing refs.",
		`{"ready":true,"authentication":"needs_login"}`,
		`{"confirmation_required":false}`,
		"source code containing ask the user",
	} {
		if userPromptResult("browser_navigate", tools.Result{Status: tools.StatusOK, Output: output}) {
			t.Fatal("actionable login or ordinary data denied", output)
		}
	}
}

func TestDeliveryHiddenQuestionAndAgendaConfirmationAreDenied(t *testing.T) {
	store := journal.NewMemStore()
	ctx := withDeliveryPolicy(context.Background(), deliveryPolicy{Autonomous: true})
	ctx = context.WithValue(ctx, promptJournalKey{}, journal.Store(store))
	ctx = context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{run: "child"})
	// Even a hallucinated call to the hidden ask_user tool receives guidance.
	runner := &assistantToolRunner{allowed: map[string]bool{}}
	result, err := runner.Execute(ctx, "ask_user", json.RawMessage(`{}`), tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusDenied || result.Output != autonomousPromptGuidance {
		t.Fatal(result, err)
	}
	_, questions := testOrchestrator()
	confirmer := NewQuestionConfirmer(questions)
	confirmed, err := confirmer.WaitConfirm(ctx, "delivery", "publish", "sensitive prompt sentinel")
	if err != nil || confirmed {
		t.Fatal(confirmed, err)
	}
	if _, err := questions.GetQuestion("confirm-delivery-publish"); err == nil {
		t.Fatal("delivery published an agenda confirmation")
	}
	entries, _ := store.List(ctx, "child")
	if len(entries) != 2 || strings.Contains(string(entries[1].Payload), "sentinel") {
		t.Fatal("denials missing or prompt content leaked", entries)
	}
}
