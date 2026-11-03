package daemon

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// credentialBrowserTool keeps secure form guidance on the assistant surface.
// Shared capability definitions and the frozen eval tools remain unchanged.
type credentialBrowserTool struct {
	tools.Tool
	registry *CredentialRegistry
}

func (t *credentialBrowserTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	if inv.Tool == "browser_act" && t.registry != nil && t.registry.vault != nil {
		var a struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(inv.Arguments, &a) == nil && strings.HasPrefix(a.Value, "secret://credentials-") {
			// The browser capability has no run context. Check run ownership before
			// dispatch; its resolver still enforces the page's actual exact host.
			ref := strings.TrimPrefix(a.Value, "secret://")
			if at := strings.LastIndexByte(ref, '.'); at >= 0 {
				ref = ref[:at] + "-" + ref[at+1:]
			}
			md, err := t.registry.vault.Describe(ctx, ref)
			origin, _ := ctx.Value(credentialOriginKey{}).(credentialOrigin)
			if err != nil || md.Meta["credential_run"] != "" && md.Meta["credential_run"] != origin.run {
				return tools.Result{Status: tools.StatusDenied, Output: "browser: credential is unavailable for this run"}, nil
			}
		}
	}
	result, err := t.Tool.Invoke(ctx, inv)
	if err == nil && strings.HasPrefix(result.Output, "input_required:") && (strings.Contains(result.Output, capabilities.BlockedLogin) || strings.Contains(result.Output, capabilities.Blocked2FA)) {
		need := capabilities.BlockedLogin
		fields := `["username","password"]`
		if strings.Contains(result.Output, capabilities.Blocked2FA) {
			need = capabilities.Blocked2FA
			fields = `["otp"]`
		}
		guidance := "Call request_credentials only if you do not already have the required secret:// refs, with site set to the exact page host (hostname only, without port), a short purpose, and fields " + fields + ". Wait for the secure web form; never request values in chat."
		if inv.Tool == "browser_wait_ready" {
			// The real browser marks login/OTP forms ready after the requested
			// load check. Authentication is still pending, but observation and
			// secure form actions can proceed without another navigation.
			raw, _ := json.Marshal(map[string]any{"ready": true, "authentication": need, "next": "Call browser_snapshot now, then browser_act type with the credential refs. " + guidance + " After each action, call browser_wait_ready and take a fresh snapshot."})
			result.Output = string(raw)
		} else if inv.Tool == "browser_act" {
			// A detected login form after an action does not undo that action.
			// The real browser returns this state only after the action succeeds.
			var action struct {
				Action string `json:"action"`
				Ref    string `json:"ref"`
			}
			_ = json.Unmarshal(inv.Arguments, &action)
			raw, _ := json.Marshal(map[string]any{"action": action.Action, "ref": action.Ref, "resnapshot": true, "authentication": need, "next": "The action succeeded. Call browser_wait_ready and take a fresh browser_snapshot, then fill the remaining fields or click the submit button. Do not repeat a completed action just because the form still requires authentication. " + guidance})
			result.Output = string(raw)
		} else {
			result.Output = "input_required: " + need + ". The login form is loaded; keep this session and tab. " + guidance + " Then call browser_wait_ready, browser_snapshot, and browser_act type using the returned secret:// refs, with a fresh snapshot before each action. Do not navigate again to fill this form."
		}
	}
	return result, err
}
