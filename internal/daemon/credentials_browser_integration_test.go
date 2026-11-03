//go:build browser && linux

package daemon

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestCredentialBrowserSecureLogin(t *testing.T) {
	if os.Getenv("EXCALIBUR_BROWSER_TESTS") != "1" {
		t.Skip("set EXCALIBUR_BROWSER_TESTS=1")
	}
	if err := browser.Available(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var authenticated atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/session":
			_ = r.ParseForm()
			if r.FormValue("username") == "browser-user-sentinel" && r.FormValue("password") == "browser-password-sentinel" {
				authenticated.Store(true)
				http.Redirect(w, r, "/home", http.StatusSeeOther)
				return
			}
			http.Error(w, "invalid login", http.StatusUnauthorized)
		case "/home":
			fmt.Fprint(w, `<h1>Signed in</h1><p>browser-user-sentinel browser-password-sentinel</p>`)
		default:
			fmt.Fprint(w, `<form method="post" action="/session"><label>Username<input name="username" autocomplete="off"></label><label>Password<input name="password" type="password" autocomplete="off"></label><button>Sign in</button></form>`)
		}
	}))
	defer site.Close()
	parsed, _ := url.Parse(site.URL)
	v, pf := openTestVault(t)
	dir := filepath.Dir(pf)
	bus := telemetry.NewBus(nil)
	cfg := config.Default()
	svc, err := Assemble(AssembleOptions{Vault: v, Bus: bus, BrowserWeb: &wireengine.BrowserWebOptions{Config: cfg, DataDir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.BrowserWeb.Close()
	defer svc.Credentials.Close()
	ctx = context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{conversation: "browser-conversation", run: "browser-run"})
	outputs := []string{}
	seq := 0
	call := func(name string, args any) string {
		t.Helper()
		seq++
		raw, _ := json.Marshal(args)
		out, err := svc.Exec.Execute(ctx, name, raw, tools.ExecuteOptions{IdempotencyKey: fmt.Sprintf("credential-browser-%d", seq), Timeout: 30 * time.Second})
		if err != nil || out.Status != tools.StatusOK {
			t.Fatalf("%s failed (%s): %v", name, out.Status, err)
		}
		outputs = append(outputs, out.Output)
		return out.Output
	}
	object := func(raw string) map[string]any {
		t.Helper()
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) != nil {
			t.Fatalf("expected browser JSON: %.200s", raw)
		}
		return out
	}
	session := object(call("browser_open_session", map[string]any{}))
	tab := object(call("browser_tabs", map[string]any{"session_id": session["session_id"], "action": "new"}))
	args := map[string]any{"session_id": session["session_id"], "tab_id": tab["tab_id"], "action": "go", "url": site.URL}
	need := call("browser_navigate", args)
	if !strings.Contains(need, "request_credentials") {
		t.Fatal("login detection omitted credential guidance")
	}
	delete(args, "action")
	delete(args, "url")
	requested := make(chan api.CredentialsRequestedEvent, 1)
	origin := ctx.Value(credentialOriginKey{}).(credentialOrigin)
	origin.requested = func(e api.CredentialsRequestedEvent) { requested <- e }
	requestCtx := context.WithValue(ctx, credentialOriginKey{}, origin)
	done := make(chan tools.Result, 1)
	safego.Go("credential-browser-request", nil, func() {
		raw, _ := json.Marshal(credentialArgs{Site: parsed.Hostname(), Purpose: "Local browser login"})
		out, _ := svc.Exec.Execute(requestCtx, "request_credentials", raw, tools.ExecuteOptions{})
		done <- out
	})
	var event api.CredentialsRequestedEvent
	select {
	case event = <-requested:
	case <-ctx.Done():
		t.Fatal("credential request missing")
	}
	backend := NewDaemonBackend(nil, nil, bus)
	backend.credentials = svc.Credentials
	gate := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), bus)
	addr, err := gate.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Stop(context.Background())
	_, err = gateway.Dial(addr, "browser-test").Call(ctx, "submit", api.MethodCredentialsSubmit, api.CredentialsSubmitParams{RequestID: event.RequestID, Values: map[string]string{"username": "browser-user-sentinel", "password": "browser-password-sentinel"}})
	if err != nil {
		t.Fatal(err)
	}
	var result tools.Result
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatal("credential tool did not resume")
	}
	if result.Status != tools.StatusOK {
		t.Fatal("credential request failed")
	}
	outputs = append(outputs, result.Output)
	var provided credentialResult
	if json.Unmarshal([]byte(result.Output), &provided) != nil || provided.Status != "provided" {
		t.Fatal("credential refs missing")
	}
	snapshot := func() map[string]any { call("browser_wait_ready", args); return object(call("browser_snapshot", args)) }
	act := func(label, action, value string) {
		t.Helper()
		snap := snapshot()
		ref := ""
		for _, item := range snap["interactive"].([]any) {
			el := item.(map[string]any)
			if el["name"] == label {
				ref = el["ref"].(string)
			}
		}
		if ref == "" {
			t.Fatalf("missing form ref %s", label)
		}
		a := map[string]any{}
		for k, v := range args {
			a[k] = v
		}
		a["snapshot_id"] = snap["snapshot_id"]
		a["ref"] = ref
		a["action"] = action
		a["value"] = value
		call("browser_act", a)
	}
	act("Username", "type", provided.Refs["username"])
	act("Password", "type", provided.Refs["password"])
	act("Sign in", "click", "")
	final := snapshot()
	if !authenticated.Load() || !strings.Contains(final["text"].(string), "Signed in") {
		t.Fatal("refs did not authenticate")
	}
	if final["capture_masked"] != true {
		t.Fatal("secret page screenshot unmasked")
	}
	events, _ := json.Marshal(bus.Replay(types.Cursor{}))
	outputs = append(outputs, string(events))
	for _, secret := range []string{"browser-user-sentinel", "browser-password-sentinel"} {
		if strings.Contains(strings.Join(outputs, "\n"), secret) {
			t.Fatal("browser/tool/event output leaked credential")
		}
		// Browser-owned DOM captures and screenshots are durable model artifacts.
		err = filepath.WalkDir(filepath.Join(dir, "blobs"), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil && strings.Contains(string(data), secret) {
				t.Error("browser blob leaked credential")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	svc.Credentials.EndRun("browser-run")
	for _, ref := range provided.Refs {
		if _, err := v.Describe(ctx, credentialEntry(ref)); err != vault.ErrNotFound {
			t.Fatal("run credential retained")
		}
	}
}
