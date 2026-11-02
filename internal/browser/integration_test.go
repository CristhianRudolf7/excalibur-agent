//go:build browser && linux

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

var browserCheck sync.Once
var browserCheckErr error

func requireBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("EXCALIBUR_BROWSER_TESTS") != "1" {
		t.Skip("set EXCALIBUR_BROWSER_TESTS=1")
	}
	browserCheck.Do(func() { browserCheckErr = Available() })
	if browserCheckErr != nil {
		t.Fatal(browserCheckErr)
	}
}
func invoke(t *testing.T, e *Engine, op string, args map[string]any) capabilities.TaskResult {
	t.Helper()
	r, err := e.Invoke("browser_"+op, raw(args), "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for r.State == capabilities.StateWorking {
		if time.Now().After(deadline) {
			_, _ = e.Cancel(r.TaskID)
			t.Fatal("task timeout", op)
		}
		time.Sleep(10 * time.Millisecond)
		r, err = e.Poll(r.TaskID)
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}
func success(t *testing.T, r capabilities.TaskResult) map[string]any {
	t.Helper()
	if r.State != capabilities.StateCompleted {
		t.Fatalf("operation: %+v", r)
	}
	var p map[string]any
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func fixtures() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><title>Fixture</title><label>Query<input id="query"></label><button onclick="document.querySelector('#result').textContent=document.querySelector('#query').value">Save</button><p id="result">Ready</p><input type="file" aria-label="Upload"><a href="/download" download>Download</a><button onclick="console.log('fixture console');fetch('/missing?api_key=hidden')">Log</button><a href="/next">Next</a>`)
	})
	mux.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `<title>Next</title><p>Next page</p>`) })
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="fixture.txt"`)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "download fixture")
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/captcha", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<script type="text/plain" src="/recaptcha/api.js"></script><p>Challenge</p>`)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `<input type="password">`) })
	mux.HandleFunc("/2fa", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `<input autocomplete="one-time-code">`) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		mux.ServeHTTP(w, r)
	})
}

type browserHarness struct {
	t    *testing.T
	e    *Engine
	args map[string]any
}

func harness(t *testing.T, e *Engine, site string) *browserHarness {
	t.Helper()
	p := success(t, invoke(t, e, "open_session", map[string]any{}))
	args := map[string]any{"session_id": p["session_id"], "action": "new", "url": site}
	p = success(t, invoke(t, e, "tabs", args))
	args["tab_id"] = p["tab_id"]
	delete(args, "action")
	return &browserHarness{t, e, args}
}
func (h *browserHarness) call(op string, extra map[string]any) capabilities.TaskResult {
	h.t.Helper()
	args := map[string]any{}
	for k, v := range h.args {
		args[k] = v
	}
	for k, v := range extra {
		args[k] = v
	}
	return invoke(h.t, h.e, op, args)
}
func (h *browserHarness) ready() { h.t.Helper(); success(h.t, h.call("wait_ready", nil)) }
func (h *browserHarness) snapshot() map[string]any {
	h.t.Helper()
	h.ready()
	return success(h.t, h.call("snapshot", nil))
}
func (h *browserHarness) action(name, action, value string) capabilities.TaskResult {
	h.t.Helper()
	snap := h.snapshot()
	ref := ""
	for _, el := range snap["interactive"].([]any) {
		element := el.(map[string]any)
		if element["name"] == name {
			ref = element["ref"].(string)
			break
		}
	}
	if ref == "" {
		h.t.Fatalf("missing element %q: %v", name, snap)
	}
	return h.call("act", map[string]any{"snapshot_id": snap["snapshot_id"], "ref": ref, "action": action, "value": value})
}
func TestBrowserEndToEnd(t *testing.T) {
	requireBrowser(t)
	server := httptest.NewServer(fixtures())
	defer server.Close()
	root := t.TempDir()
	uploads := filepath.Join(root, "uploads")
	if err := os.Mkdir(uploads, 0700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(uploads, "file.txt")
	outside := filepath.Join(root, "outside.txt")
	for _, p := range []string{inside, outside} {
		if err := os.WriteFile(p, []byte("upload"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	blobs := &MemoryBlobs{}
	profile := filepath.Join(root, "profile")
	e := New(Config{ProfileDir: profile, AllowedUploadDirs: []string{uploads}, Blobs: blobs, Secrets: MemorySecrets{"credential": {Value: "test-secret-sentinel", Hosts: []string{"127.0.0.1"}}}})
	defer e.Close()
	h := harness(t, e, server.URL)
	t.Run("wait modes", func(t *testing.T) {
		for _, mode := range []string{"load", "network_idle", "dom_stable", "selector"} {
			success(t, h.call("wait_ready", map[string]any{"mode": mode, "selector": "#query", "timeout_ms": 3000}))
		}
	})
	t.Run("snapshot stale refs form", func(t *testing.T) {
		snap := h.snapshot()
		if len(blobs.Get(snap["capture_ref"].(string))) == 0 || !strings.Contains(string(blobs.Get(snap["dom_ref"].(string))), "Fixture") {
			t.Fatal("missing captures")
		}
		h.snapshot()
		r := h.call("act", map[string]any{"snapshot_id": snap["snapshot_id"], "ref": "r1", "action": "click"})
		if !strings.HasPrefix(r.ErrorText, "stale_ref") {
			t.Fatal(r)
		}
		success(t, h.action("Query", "type", "form value"))
		success(t, h.action("Save", "click", ""))
		snap = h.snapshot()
		if !strings.Contains(snap["text"].(string), "form value") {
			t.Fatal(snap)
		}
	})
	t.Run("uploads", func(t *testing.T) {
		success(t, h.action("Upload", "upload", inside))
		r := h.action("Upload", "upload", outside)
		if r.State != capabilities.StateFailed || r.ErrClass != capabilities.ErrPermanent {
			t.Fatal(r)
		}
	})
	t.Run("download", func(t *testing.T) {
		snap := h.snapshot()
		ref := ""
		for _, entry := range snap["interactive"].([]any) {
			el := entry.(map[string]any)
			if el["name"] == "Download" {
				ref = el["ref"].(string)
			}
		}
		p := success(t, h.call("download", map[string]any{"snapshot_id": snap["snapshot_id"], "ref": ref}))
		if string(blobs.Get(p["blob"].(string))) != "download fixture" || p["name"] != "fixture.txt" || p["size"] != float64(16) {
			t.Fatal(p)
		}
	})
	t.Run("devtools", func(t *testing.T) {
		success(t, h.action("Log", "click", ""))
		success(t, h.call("wait_ready", map[string]any{"mode": "network_idle"}))
		console := success(t, h.call("devtools", nil))
		network := success(t, h.call("devtools", map[string]any{"type": "network"}))
		if !strings.Contains(string(raw(console)), "fixture console") || !strings.Contains(string(raw(network)), "404") || strings.Contains(string(raw(network)), "hidden") {
			t.Fatal(console, network)
		}
		success(t, h.call("devtools", map[string]any{"type": "performance"}))
	})
	t.Run("emulation", func(t *testing.T) {
		p := success(t, h.call("emulate", map[string]any{"device": "iPhone 13", "locale": "en-GB", "color_scheme": "dark"}))
		viewport := p["viewport"].(map[string]any)
		if viewport["width"] != float64(390) {
			t.Fatal(p)
		}
		page := e.sessions[h.args["session_id"].(string)].tabs[h.args["tab_id"].(string)].page
		size, err := page.Evaluate("() => innerWidth")
		if err != nil || fmt.Sprint(size) != "390" {
			t.Fatal(size, err)
		}
	})
	t.Run("routines", func(t *testing.T) {
		steps := []Step{{Action: "type", Target: Target{Role: "textbox", Name: "Query"}, Value: "routine value"}, {Action: "click", Target: Target{Role: "button", Name: "Save"}, Assert: raw(Assertion{Type: "text_contains", Value: "routine value"})}}
		success(t, h.call("routine", map[string]any{"action": "record", "routine": "save", "steps": steps}))
		success(t, h.call("routine", map[string]any{"action": "run", "routine": "save"}))
		steps[1].Assert = raw(Assertion{Type: "url_matches", Value: "/never$"})
		success(t, h.call("routine", map[string]any{"action": "record", "routine": "fail", "steps": steps}))
		r := h.call("routine", map[string]any{"action": "run", "routine": "fail"})
		if r.State != capabilities.StateFailed || !strings.Contains(r.ErrorText, "step 1") {
			t.Fatal(r)
		}
		success(t, h.call("routine", map[string]any{"action": "list"}))
		success(t, h.call("routine", map[string]any{"action": "record", "routine": "visible", "steps": []Step{{Action: "hover", Target: Target{Role: "textbox", Name: "Query"}, Assert: raw(Assertion{Type: "element_visible", Target: &Target{Role: "textbox", Name: "Query"}})}}}))
		success(t, h.call("routine", map[string]any{"action": "run", "routine": "visible"}))
	})
	t.Run("secrets", func(t *testing.T) {
		r := h.action("Query", "type", "secret://credential")
		if strings.Contains(string(raw(r)), "test-secret-sentinel") || !strings.Contains(string(r.Payload), "[secret]") {
			t.Fatal(r)
		}
		success(t, r)
		success(t, h.action("Save", "click", ""))
		snap := h.snapshot()
		if strings.Contains(string(raw(snap)), "test-secret-sentinel") || strings.Contains(string(blobs.Get(snap["dom_ref"].(string))), "test-secret-sentinel") {
			t.Fatal("secret leaked")
		}
		u, _ := url.Parse(server.URL)
		other := strings.Replace(server.URL, u.Hostname(), "localhost", 1)
		success(t, h.call("navigate", map[string]any{"url": other}))
		r = h.action("Query", "type", "secret://credential")
		if r.State != capabilities.StateFailed || r.ErrClass != capabilities.ErrPermanent {
			t.Fatal(r)
		}
		success(t, h.call("navigate", map[string]any{"url": server.URL}))
	})
	t.Run("blocks and policy", func(t *testing.T) {
		for _, c := range []struct{ path, block string }{{"/captcha", capabilities.BlockedCaptcha}, {"/login", capabilities.BlockedLogin}, {"/2fa", capabilities.Blocked2FA}} {
			r := h.call("navigate", map[string]any{"url": server.URL + c.path})
			if r.State != capabilities.StateInputRequired || !strings.Contains(r.Need, c.block) {
				tab := e.sessions[h.args["session_id"].(string)].tabs[h.args["tab_id"].(string)]
				html, _ := tab.page.Content()
				t.Fatalf("%+v HTML %s", r, html)
			}
			again, _ := e.Poll(r.TaskID)
			if again.State != r.State {
				t.Fatal(again)
			}
			updated, _ := e.Update(r.TaskID, raw(map[string]any{"solved": true}))
			if updated.State != capabilities.StateInputRequired {
				t.Fatal(updated)
			}
			tab := e.sessions[h.args["session_id"].(string)].tabs[h.args["tab_id"].(string)]
			if _, err := tab.page.Goto(server.URL); err != nil {
				t.Fatal(err)
			}
			updated, _ = e.Update(r.TaskID, raw(map[string]any{}))
			if updated.State != capabilities.StateCompleted {
				t.Fatal(updated)
			}
		}
		r := h.call("navigate", map[string]any{"url": "file:///etc/passwd"})
		if r.ErrClass != capabilities.ErrPermanent {
			t.Fatal(r)
		}
	})
	t.Run("suspend resume RSS sandbox persistence", func(t *testing.T) {
		h.ready()
		status := success(t, h.call("status", nil))
		if status["rss_bytes"].(float64) <= 0 {
			t.Fatal(status)
		}
		_, cmds := processStats(e.cfg.ProfileDir)
		found := false
		for _, cmd := range cmds {
			if strings.Contains(cmd, "--user-data-dir=") {
				found = true
				if strings.Contains(cmd, "--no-sandbox") {
					t.Fatal("sandbox disabled")
				}
			}
		}
		if !found {
			t.Fatal("missing Chrome process")
		}
		success(t, h.call("suspend_session", nil))
		if e.browser != nil {
			t.Fatal("not suspended")
		}
		success(t, h.call("tabs", nil))
		h.ready()
		tab := e.sessions[h.args["session_id"].(string)].tabs[h.args["tab_id"].(string)]
		if _, err := tab.page.Evaluate("() => { document.cookie = 'persistent=yes;max-age=3600;path=/'; localStorage.setItem('persistent', 'yes'); }"); err != nil {
			t.Fatal(err)
		}
		if err := e.Close(); err != nil {
			t.Fatal(err)
		}
		reopened := New(Config{ProfileDir: profile})
		defer reopened.Close()
		h2 := harness(t, reopened, server.URL)
		tab = reopened.sessions[h2.args["session_id"].(string)].tabs[h2.args["tab_id"].(string)]
		cookie, err := tab.page.Evaluate("() => document.cookie + localStorage.getItem('persistent')")
		if err != nil || !strings.Contains(fmt.Sprint(cookie), "persistent=yes") {
			t.Fatal(cookie, err)
		}
	})
}
func TestBrowserCancellationAndIdle(t *testing.T) {
	requireBrowser(t)
	server := httptest.NewServer(fixtures())
	defer server.Close()
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile"), IdleTimeout: 200 * time.Millisecond, Clock: func() time.Time { return time.Unix(0, now.Load()) }})
	defer e.Close()
	h := harness(t, e, server.URL)
	snap := h.snapshot()
	var ref string
	for _, v := range snap["interactive"].([]any) {
		el := v.(map[string]any)
		if el["name"] == "Query" {
			ref = el["ref"].(string)
		}
	}
	tab := e.sessions[h.args["session_id"].(string)].tabs[h.args["tab_id"].(string)]
	if _, err := tab.page.Evaluate("() => document.querySelector('#query').disabled=true"); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{}
	for k, v := range h.args {
		args[k] = v
	}
	args["snapshot_id"] = snap["snapshot_id"]
	args["ref"] = ref
	args["action"] = "type"
	args["value"] = "cancel"
	r, err := e.Invoke("browser_act", raw(args), "cancel-action")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	_, _ = e.Cancel(r.TaskID)
	deadline := time.Now().Add(3 * time.Second)
	for {
		polled, _ := e.Poll(r.TaskID)
		if polled.State == capabilities.StateCancelled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(polled)
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.ready()
	now.Add(int64(time.Second))
	idleDeadline := time.Now().Add(30 * time.Second)
	for {
		status := success(t, h.call("status", nil))
		if status["suspended"] == true && status["idle_ms"].(float64) >= 200 {
			break
		}
		if time.Now().After(idleDeadline) {
			t.Fatal(status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.ready()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Install(ctx, InstallOptions{}); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestBrowserNavigationActionsAndBounds(t *testing.T) {
	requireBrowser(t)
	mux := http.NewServeMux()
	mux.Handle("/", fixtures())
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	server := httptest.NewServer(mux)
	defer server.Close()
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile"), MaxTabs: 2})
	defer e.Close()
	h := harness(t, e, server.URL)
	r := h.call("snapshot", nil)
	if r.ErrorText != capabilities.NotReadyMessage {
		t.Fatal(r)
	}
	success(t, h.call("navigate", map[string]any{"url": server.URL + "/next"}))
	for _, action := range []string{"back", "forward", "reload"} {
		success(t, h.call("navigate", map[string]any{"action": action}))
	}
	success(t, h.call("navigate", map[string]any{"url": server.URL}))
	r = h.call("wait_ready", map[string]any{"mode": "selector", "selector": "#never", "timeout_ms": 50})
	if r.ErrClass != capabilities.ErrTransient {
		t.Fatal(r)
	}
	// Add local controls solely for exercising the remaining fixed action kinds.
	page := e.sessions[h.args["session_id"].(string)].tabs[h.args["tab_id"].(string)].page
	_, err := page.Evaluate(`() => { const div=document.createElement('div'); div.innerHTML='<label>Options<select><option value="one">One</option><option value="two">Two</option></select></label><button aria-label="Source" draggable="true">Source</button><button aria-label="Target">Target</button>'; document.body.append(div); }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, action, value string }{{"Options", "select", "two"}, {"Query", "hover", ""}, {"Query", "scroll", ""}, {"Query", "press", "End"}} {
		success(t, h.action(c.name, c.action, c.value))
	}
	snap := h.snapshot()
	refs := map[string]string{}
	for _, entry := range snap["interactive"].([]any) {
		el := entry.(map[string]any)
		refs[el["name"].(string)] = el["ref"].(string)
	}
	success(t, h.call("act", map[string]any{"snapshot_id": snap["snapshot_id"], "ref": refs["Source"], "target_ref": refs["Target"], "action": "drag"}))
	success(t, h.call("routine", map[string]any{"action": "record", "routine": "drag", "steps": []Step{{Action: "drag", Target: Target{Role: "button", Name: "Source"}, Destination: &Target{Role: "button", Name: "Target"}}}}))
	success(t, h.call("routine", map[string]any{"action": "run", "routine": "drag"}))
	h.ready()
	_, err = page.Evaluate(`() => { const button=document.createElement('button');button.textContent='duplicate accessible name';document.body.append(button,button.cloneNode(true)); const text=document.createElement('p');text.textContent='long '.repeat(10000);document.body.append(text); }`)
	if err != nil {
		t.Fatal(err)
	}
	snap = h.snapshot()
	if len(raw(snap)) > inlineBudget {
		t.Fatal("unbounded snapshot")
	}
	var duplicateRefs []string
	for _, entry := range snap["interactive"].([]any) {
		el := entry.(map[string]any)
		if el["name"] == "duplicate accessible name" {
			duplicateRefs = append(duplicateRefs, el["ref"].(string))
		}
	}
	if len(duplicateRefs) != 2 || duplicateRefs[0] == duplicateRefs[1] {
		t.Fatal("duplicate accessible names lost", duplicateRefs)
	}
	// Routine refs become accessible targets on disk, never persisted stale refs.
	success(t, h.call("routine", map[string]any{"action": "record", "routine": "by-ref", "steps": []Step{{Action: "hover", Ref: refs["Query"]}}}))
	data, err := os.ReadFile(filepath.Join(e.cfg.RoutinesDir, "routines.json"))
	if err != nil || strings.Contains(string(data), `"ref"`) {
		t.Fatal("persisted snapshot ref", err)
	}
	// A navigation cancellation must stop the actual in-flight request.
	args := map[string]any{}
	for k, v := range h.args {
		args[k] = v
	}
	args["url"] = server.URL + "/slow"
	r, err = e.Invoke("browser_navigate", raw(args), "slow")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cancelled, err := e.Cancel(r.TaskID)
	if err != nil || cancelled.State != capabilities.StateCancelled {
		t.Fatal(cancelled, err)
	}
	success(t, h.call("navigate", map[string]any{"url": server.URL}))
	r = h.call("navigate", map[string]any{"url": server.URL + "/slow", "timeout_ms": 100})
	if r.ErrClass != capabilities.ErrTransient {
		t.Fatal(r)
	}
	success(t, h.call("navigate", map[string]any{"url": server.URL}))
	success(t, h.call("tabs", map[string]any{"action": "new"}))
	r = h.call("tabs", map[string]any{"action": "new"})
	if r.ErrClass != capabilities.ErrTransient {
		t.Fatal(r)
	}
	success(t, h.call("tabs", map[string]any{"action": "switch"}))
	success(t, h.call("tabs", map[string]any{"action": "close"}))
	success(t, h.call("close_session", nil))
	success(t, h.call("close_session", nil))
}
