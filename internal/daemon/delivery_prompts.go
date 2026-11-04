package daemon

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const workspaceLoginGuidance = "This is your own app: log in with the credentials you created (see the workspace README/seed data or .env.example). Non-secret demo credentials are not user secrets; type them directly with browser_act after browser_wait_ready and a fresh browser_snapshot. Do not request credentials from the user."
const autonomousPromptGuidance = "This delivery run must ask zero questions. The user prompt was automatically denied. Use existing authorized resources, choose a reasonable default and record the assumption, or skip the blocked operation and continue with alternatives. Do not ask the user or retry with fabricated confirmation."

type promptJournalKey struct{}
type workspaceAppKey struct{}

func autonomousDelivery(ctx context.Context) bool {
	policy, _ := ctx.Value(deliveryPolicyKey{}).(deliveryPolicy)
	return policy.Autonomous
}

// Local addresses are classified literally, without resolving third-party DNS.
func localAppHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified())
}

func ownWorkspaceApp(ctx context.Context, host string) bool {
	return ownWorkspaceOrigin(ctx, host, "")
}

// Denials contain only policy metadata, never prompt arguments or credentials.
// The engine also records its normal tool intent/result around these decisions.
func recordPromptDenial(ctx context.Context, tool, guidance string) error {
	store, _ := ctx.Value(promptJournalKey{}).(journal.Store)
	origin, _ := ctx.Value(credentialOriginKey{}).(credentialOrigin)
	if store == nil || origin.run == "" {
		return nil
	}
	_, err := store.Append(ctx, journal.Entry{RunID: origin.run, Type: "user_prompt_denied", Payload: journal.MarshalPayload(map[string]string{"tool": tool, "decision": "auto_denied", "guidance": guidance})})
	return err
}

func userPromptTool(def tools.Definition) bool {
	switch def.Name {
	case "ask_user", "request_user_input", "request_confirmation", "confirm":
		return true
	}
	return def.Group == "question" || def.Group == "confirmation"
}

func automaticPromptDenial(ctx context.Context, name string) (tools.Result, error) {
	if err := recordPromptDenial(ctx, name, autonomousPromptGuidance); err != nil {
		return tools.Result{Status: tools.StatusToolError, Output: "Could not journal automatic user prompt denial; the operation was not authorized."}, err
	}
	return tools.Result{Status: tools.StatusDenied, Output: autonomousPromptGuidance}, nil
}

// Structured confirmation results and capability input requests must not send
// an autonomous worker back to the user. Login/OTP browser forms remain locally
// actionable; their daemon wrapper already supplies context-aware guidance.
func userPromptResult(name string, result tools.Result) bool {
	output := strings.TrimSpace(result.Output)
	if strings.HasPrefix(output, "input_required:") {
		return !(strings.HasPrefix(name, "browser_") && (strings.Contains(output, capabilities.BlockedLogin) || strings.Contains(output, capabilities.Blocked2FA)))
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(output), &fields) == nil {
		for _, key := range []string{"confirmation_required", "requires_confirmation", "question_required", "credentials_required", "user_input_required"} {
			var value bool
			if json.Unmarshal(fields[key], &value) == nil && value {
				return true
			}
		}
		for _, key := range []string{"state", "status"} {
			var value string
			_ = json.Unmarshal(fields[key], &value)
			switch value {
			case "input_required", "confirmation_required", "needs_confirmation", "needs_user_input":
				return true
			}
		}
	}
	// Some adapters return a denied text result instead of a structured flag.
	if result.Status == tools.StatusDenied || result.Status == tools.StatusInputError {
		lower := strings.ToLower(output)
		return strings.HasPrefix(lower, "confirmation required") || strings.Contains(lower, "ask the user") || strings.Contains(lower, "ask for explicit approval") || strings.Contains(lower, "user confirmation")
	}
	return false
}

// A hallucinated hidden prompt still needs a definition for engine batch
// planning. This definition permits only local denial, never interactive dispatch.
func deniedPromptDefinition(name string) tools.Definition {
	return tools.Definition{Name: name, Description: "Automatically deny user prompts during zero-question delivery.", InputSchema: json.RawMessage(`{"type":"object"}`), Effect: types.EffectCommunication, Idempotency: types.IdemPure, Cancel: types.CancelImmediate, Timeout: time.Second, OutputBudget: tools.DefaultOutputBudget, Profiles: []string{"default"}}
}
