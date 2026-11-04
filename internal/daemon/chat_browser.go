package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Chat publishes the browser's concrete arguments without changing the shared
// capability manifest or the frozen embedded/eval catalog.
func chatBrowserDefinition(def tools.Definition) tools.Definition {
	properties := map[string]any{}
	var required []string
	str := func(name, description string) {
		properties[name] = map[string]any{"type": "string", "description": description}
	}
	enum := func(name string, values ...string) {
		properties[name] = map[string]any{"type": "string", "enum": values}
	}
	if def.Name != "browser_open_session" {
		str("session_id", "The session_id returned by browser_open_session.")
		required = append(required, "session_id")
	}
	switch def.Name {
	case "browser_open_session":
		str("profile", "Persistent profile name; omit for default.")
		enum("mode", "headless", "headed")
		properties["mode"].(map[string]any)["description"] = "Omit to use the configured default (headless). Use headed only when a graphical display is available."
		def.Description += ". Returns session_id and an initial tab_id. Repeated calls reuse the turn's session; use browser_tabs for additional tabs."
	case "browser_close_session":
	case "browser_tabs":
		enum("action", "list", "new", "switch", "close")
		str("tab_id", "Tab to switch or close.")
		str("url", "Optional http(s) URL for a new tab.")
		def.Description += ". Use action=new to create a tab; action=list only lists existing tabs."
	case "browser_navigate", "browser_wait_ready", "browser_snapshot", "browser_act":
		str("tab_id", "The tab_id returned by browser_tabs; omit to use the active tab.")
		properties["timeout_ms"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 60000}
		switch def.Name {
		case "browser_navigate":
			enum("action", "go", "back", "forward", "reload")
			str("url", "Full http(s) URL; required for action=go (the default).")
		case "browser_wait_ready":
			enum("mode", "load", "network_idle", "selector", "dom_stable")
			str("selector", "CSS selector required for mode=selector.")
			properties["stable_ms"] = map[string]any{"type": "integer", "minimum": 1}
			def.Description += ". A ready login or OTP form returns ready=true with authentication still pending; proceed to browser_snapshot and fill it using credential refs."
		case "browser_snapshot":
			def.Description += ". Call browser_wait_ready first. Use the returned snapshot_id and interactive ref in browser_act."
		case "browser_act":
			enum("action", "click", "type", "select", "hover", "scroll", "press", "upload", "drag")
			str("snapshot_id", "snapshot_id from the most recent browser_snapshot.")
			str("ref", "Interactive element ref from that snapshot.")
			str("value", "Action value; use workspace demo credentials directly for your own app. For third-party credentials use secret:// refs from request_credentials.")
			str("target_ref", "Destination ref for drag.")
			properties["x"] = map[string]any{"type": "number"}
			properties["y"] = map[string]any{"type": "number"}
			required = append(required, "action", "snapshot_id", "ref")
			def.Description += ". After every action, call browser_wait_ready and take a fresh browser_snapshot before the next action."
		}
	default:
		return def
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	def.InputSchema, _ = json.Marshal(schema)
	return def
}

// A conversational turn owns one logical session. Chrome's persistent profile
// retains logins after that session's tabs are closed at the end of the turn.
type chatBrowser struct {
	mu      sync.Mutex
	session string
	opened  tools.Result
}

func (r *assistantToolRunner) executeBrowser(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	b := &r.browser
	b.mu.Lock()
	defer b.mu.Unlock()
	if name == "browser_status" {
		return r.daemonToolRunner.Execute(ctx, name, args, opts)
	}
	if name == "browser_open_session" {
		if b.session != "" {
			return b.opened, nil
		}
		out, err := r.daemonToolRunner.Execute(ctx, name, args, opts)
		if err != nil || out.Status != tools.StatusOK {
			return out, err
		}
		var opened map[string]any
		if json.Unmarshal([]byte(out.Output), &opened) != nil {
			return tools.Result{Status: tools.StatusToolError, Output: "browser: session response is invalid"}, nil
		}
		b.session, _ = opened["session_id"].(string)
		if b.session == "" {
			return tools.Result{Status: tools.StatusToolError, Output: "browser: session response has no session_id"}, nil
		}
		tabArgs, _ := json.Marshal(map[string]string{"session_id": b.session, "action": "new"})
		tab, err := r.daemonToolRunner.Execute(ctx, "browser_tabs", tabArgs, tools.ExecuteOptions{IdempotencyKey: r.runID + ":" + b.session + ":initial-tab"})
		if err != nil || tab.Status != tools.StatusOK {
			r.closeBrowserLocked(ctx)
			return tab, err
		}
		var details struct {
			TabID string `json:"tab_id"`
		}
		if json.Unmarshal([]byte(tab.Output), &details) != nil || details.TabID == "" {
			r.closeBrowserLocked(ctx)
			return tools.Result{Status: tools.StatusToolError, Output: "browser: initial tab response has no tab_id"}, nil
		}
		opened["tab_id"] = details.TabID
		raw, _ := json.Marshal(opened)
		out.Output = string(raw)
		b.opened = out
		return out, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil || fields == nil {
		return tools.Result{Status: tools.StatusInputError, Output: "browser: arguments must be a JSON object"}, nil
	}
	var session string
	_ = json.Unmarshal(fields["session_id"], &session)
	if session == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "browser: session_id is required; use the session_id returned by browser_open_session"}, nil
	}
	if session != b.session {
		return tools.Result{Status: tools.StatusInputError, Output: "browser: unknown session for this turn; use the session_id returned by browser_open_session"}, nil
	}
	out, err := r.daemonToolRunner.Execute(ctx, name, args, opts)
	if err == nil && name == "browser_close_session" && out.Status == tools.StatusOK {
		b.session, b.opened = "", tools.Result{}
	}
	if out.Status == tools.StatusInputError && strings.Contains(out.Output, "unknown tab") {
		out.Output += "; the tab does not exist in this session. Call browser_tabs with this session_id and action=list or action=new; keep the same session"
	}
	return out, err
}

func (r *assistantToolRunner) closeBrowser(ctx context.Context) {
	r.browser.mu.Lock()
	defer r.browser.mu.Unlock()
	r.closeBrowserLocked(ctx)
}

func (r *assistantToolRunner) closeBrowserLocked(ctx context.Context) {
	if r.browser.session == "" {
		return
	}
	// Cancellation of model work must not skip resource cleanup.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	args, _ := json.Marshal(map[string]string{"session_id": r.browser.session})
	_, _ = r.daemonToolRunner.Execute(cleanup, "browser_close_session", args, tools.ExecuteOptions{IdempotencyKey: r.runID + ":" + r.browser.session + ":close", Timeout: 10 * time.Second})
	r.browser.session, r.browser.opened = "", tools.Result{}
}
