//go:build browser && linux

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestDaemonPersistentBrowserEndToEnd(t *testing.T) {
	if os.Getenv("EXCALIBUR_BROWSER_TESTS") != "1" {
		t.Skip("set EXCALIBUR_BROWSER_TESTS=1")
	}
	if err := browser.Available(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var cookieSeen atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("persist"); err == nil && c.Value == "saved" {
			cookieSeen.Store(true)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<title>Daemon fixture</title><label>Message<input id="message"></label><button onclick="document.cookie='persist=saved; max-age=3600; path=/'; document.getElementById('result').textContent=document.getElementById('message').value">Save</button><p id="result">Ready</p>`)
	}))
	defer site.Close()
	dir := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(dir, "daemon.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := func() (*Daemon, *Services) {
		svc, err := Assemble(AssembleOptions{BrowserWeb: &wireengine.BrowserWebOptions{Config: config.Default(), DataDir: dir, Blobs: wireengine.StoreBrowserBlobs{Store: db.Blob()}}})
		if err != nil {
			t.Fatal(err)
		}
		g := gateway.New(gateway.DefaultConfig(), nil, gateway.NewMemoryBlobs(), nil)
		d := New(DefaultConfig(), Deps{Gate: g, Services: svc, Orch: svc.Orch})
		if _, err = d.Start(ctx); err != nil {
			_ = svc.BrowserWeb.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.Stop(context.Background()) })
		return d, svc
	}
	call := func(svc *Services, name string, args map[string]any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(args)
		res, err := svc.Exec.Execute(ctx, name, raw, tools.ExecuteOptions{Timeout: 45 * time.Second, IdempotencyKey: fmt.Sprintf("test-%d", time.Now().UnixNano())})
		if err != nil || res.Status != tools.StatusOK {
			t.Fatalf("%s: %+v %v", name, res, err)
		}
		var out map[string]any
		if err = json.Unmarshal([]byte(res.Output), &out); err != nil {
			t.Fatalf("%s: %s", name, res.Output)
		}
		return out
	}
	session := func(svc *Services) map[string]any {
		open := call(svc, "browser_open_session", map[string]any{})
		args := map[string]any{"session_id": open["session_id"], "action": "new"}
		tab := call(svc, "browser_tabs", args)
		args["tab_id"] = tab["tab_id"]
		args["url"] = site.URL
		args["action"] = "go"
		call(svc, "browser_navigate", args)
		delete(args, "action")
		call(svc, "browser_wait_ready", args)
		return args
	}
	d, svc := start()
	args := session(svc)
	snapshot := func() map[string]any {
		call(svc, "browser_wait_ready", args)
		return call(svc, "browser_snapshot", args)
	}
	act := func(name, action, value string) {
		snap := snapshot()
		ref := ""
		for _, item := range snap["interactive"].([]any) {
			e := item.(map[string]any)
			if e["name"] == name {
				ref = e["ref"].(string)
			}
		}
		if ref == "" {
			t.Fatalf("missing ref %s", name)
		}
		a := map[string]any{}
		for k, v := range args {
			a[k] = v
		}
		a["snapshot_id"] = snap["snapshot_id"]
		a["ref"] = ref
		a["action"] = action
		a["value"] = value
		call(svc, "browser_act", a)
	}
	act("Message", "type", "form submitted")
	act("Save", "click", "")
	snap := snapshot()
	raw, _ := json.Marshal(snap)
	if !strings.Contains(string(raw), "form submitted") {
		t.Fatal("form effect not observed")
	}
	if snap["capture_ref"] == "" || snap["dom_ref"] == "" {
		t.Fatal("missing durable captures")
	}
	for _, field := range []string{"capture_ref", "dom_ref"} {
		ref, ok := snap[field].(string)
		if !ok {
			t.Fatalf("missing %s", field)
		}
		if _, err := db.Blob().Stat(ctx, ref); err != nil {
			t.Fatalf("capture not in daemon blob store: %v", err)
		}
	}
	// A public page reader must refuse this loopback URL; the browser tools are
	// intentionally permitted to test local projects. Never weaken SSRF for tests.
	raw, _ = json.Marshal(map[string]string{"url": site.URL})
	res, err := svc.Exec.Execute(ctx, "web_read", raw, tools.ExecuteOptions{})
	if err != nil || res.Status != tools.StatusToolError || !strings.Contains(res.Output, "SSRF") {
		t.Fatalf("local web_read guard: %+v %v", res, err)
	}
	if err = d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	cookieSeen.Store(false)
	_, svc = start()
	session(svc)
	if !cookieSeen.Load() {
		t.Fatal("cookie did not survive daemon restart")
	}
}
