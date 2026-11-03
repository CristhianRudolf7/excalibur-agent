//go:build browser && linux

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// Only inference is simulated: RPC, chat activation, the engine, browser tools,
// Chrome, form submission, remembered credentials and the vault are real.
func TestCredentialDaemonChatBrowserLogin(t *testing.T) {
	if os.Getenv("EXCALIBUR_BROWSER_TESTS") != "1" {
		t.Skip("set EXCALIBUR_BROWSER_TESTS=1")
	}
	if err := browser.Available(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	const username = "chat-browser-user-sentinel"
	const password = "chat-browser-password-sentinel"
	var logins atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/logout":
			http.SetCookie(w, &http.Cookie{Name: "login", Value: "", Path: "/", MaxAge: -1})
			http.Redirect(w, r, "/", http.StatusSeeOther)
		case "/session":
			_ = r.ParseForm()
			if r.FormValue("username") != username || r.FormValue("password") != password {
				http.Error(w, "invalid login", http.StatusUnauthorized)
				return
			}
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "login", Value: "valid", Path: "/", HttpOnly: true})
			http.Redirect(w, r, "/panel", http.StatusSeeOther)
		case "/panel":
			if c, err := r.Cookie("login"); err != nil || c.Value != "valid" {
				http.Error(w, "not signed in", http.StatusUnauthorized)
				return
			}
			// Echo values to check redaction before model context and durable blobs.
			fmt.Fprint(w, "<h1>DASH-4417</h1><p>"+username+" "+password+"</p>")
		default:
			fmt.Fprint(w, `<form method="post" action="/session"><label>Username<input name="username" autocomplete="off"></label><label>Password<input name="password" type="password" autocomplete="off"></label><button>Sign in</button></form>`)
		}
	}))
	defer site.Close()
	host, _ := url.Parse(site.URL)
	v, passfile := openTestVault(t)
	dir := filepath.Dir(passfile)
	logfile, err := os.Create(filepath.Join(dir, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logfile.Close()
	previousLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logfile, nil)))
	defer slog.SetDefault(previousLog)
	db, err := store.Open(ctx, filepath.Join(dir, "daemon.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := telemetry.NewBus(nil)
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), &OrchControl{Store: orchestrator.NewMemoryStore()}, bus)
	b.AttachServices(BackendServices{Vault: v})
	endpoint := models.NewGateway(models.GatewayConfig{MaxAttempts: 1, DisableBudgetRetries: true, Roles: map[types.ModelRole]models.RoleEntry{types.RoleInterlocutor: {Primary: models.ModelRef{Provider: "chat-browser-test", Model: "test"}}}})
	defer endpoint.Close()
	var mu sync.Mutex
	var observed []providers.ChatRequest
	var session, tab string
	var refs map[string]string
	step, remembered := 0, false
	endpoint.Register(&turnProvider{FakeProvider: fake.New("chat-browser-test"), chat: func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, req)
		phase := step % 15
		step++
		var last map[string]any
		for _, m := range req.Messages {
			if m.ToolResult != nil {
				last = nil
				_ = json.Unmarshal([]byte(m.ToolResult.Output), &last)
			}
		}
		args := map[string]any{"session_id": session, "tab_id": tab}
		call := func(name string) (providers.ChatResponse, error) { return browserChatCall(name, args), nil }
		switch phase {
		case 0:
			return browserChatCall("browser_open_session", map[string]any{}), nil
		case 1:
			session, _ = last["session_id"].(string)
			tab, _ = last["tab_id"].(string)
			if session == "" || tab == "" {
				return providers.ChatResponse{}, fmt.Errorf("chat open_session omitted usable session or tab")
			}
			return browserChatCall("browser_navigate", map[string]any{"session_id": session, "tab_id": tab, "url": site.URL + "/logout"}), nil
		case 2:
			return browserChatCall("request_credentials", credentialArgs{Site: host.Hostname(), Purpose: "Local login", RememberDefault: true}), nil
		case 3:
			var result credentialResult
			raw, _ := json.Marshal(last)
			if json.Unmarshal(raw, &result) != nil || len(result.Refs) != 2 {
				return providers.ChatResponse{}, fmt.Errorf("chat credential references missing")
			}
			refs = result.Refs
			if step > 15 {
				remembered = result.Status == "stored"
			}
			return call("browser_wait_ready")
		case 6, 9, 12:
			if last["resnapshot"] != true || last["action"] != map[int]string{6: "type", 9: "type", 12: "click"}[phase] {
				return providers.ChatResponse{}, fmt.Errorf("chat did not acknowledge the completed action at phase %d", phase)
			}
			return call("browser_wait_ready")
		case 4, 7, 10, 13:
			if last["ready"] != true {
				return providers.ChatResponse{}, fmt.Errorf("chat readiness did not permit a snapshot at phase %d", phase)
			}
			return call("browser_snapshot")
		case 5, 8, 11:
			label, field := "Sign in", ""
			args["action"] = "click"
			if phase == 5 {
				label, field = "Username", "username"
			} else if phase == 8 {
				label, field = "Password", "password"
			}
			if field != "" {
				args["action"], args["value"] = "type", refs[field]
			}
			args["snapshot_id"] = last["snapshot_id"]
			items, _ := last["interactive"].([]any)
			for _, item := range items {
				el, _ := item.(map[string]any)
				if el["name"] == label {
					args["ref"] = el["ref"]
				}
			}
			if args["ref"] == nil {
				return providers.ChatResponse{}, fmt.Errorf("chat snapshot omitted %s control at phase %d", label, phase)
			}
			return call("browser_act")
		default:
			text, _ := last["text"].(string)
			if !strings.Contains(text, "DASH-4417") || last["capture_masked"] != true {
				return providers.ChatResponse{}, fmt.Errorf("chat did not observe masked authenticated panel")
			}
			return textReply("The panel code is DASH-4417."), nil
		}
	}})
	svc, err := Assemble(AssembleOptions{Vault: v, Bus: bus, Models: endpoint, Jrnl: &journal.SQLStore{W: db, R: db}, BrowserWeb: &wireengine.BrowserWebOptions{Config: config.Default(), DataDir: dir, Blobs: wireengine.StoreBrowserBlobs{Store: db.Blob()}}})
	if err != nil {
		t.Fatal(err)
	}
	WireAssistantTurns(b, svc, endpoint, "test")
	gate := gateway.New(gateway.DefaultConfig(), b, gateway.NewMemoryBlobs(), bus)
	d := New(DefaultConfig(), Deps{Gate: gate, Backend: b, Master: b.Master, Services: svc, Orch: svc.Orch, Bus: bus})
	addr, err := d.Start(ctx)
	if err != nil {
		_ = svc.BrowserWeb.Close()
		t.Fatal(err)
	}
	defer d.Stop(context.Background())
	client := gateway.Dial(addr, "chat-browser-integration")
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		raw, err := client.Call(ctx, method, method, params)
		if err != nil {
			t.Fatalf("%s failed: %v", method, err)
		}
		return raw
	}
	const conversation = "chat-browser-conversation"
	for turn := 1; turn <= 2; turn++ {
		call(api.MethodConversationSend, api.ConversationSendParams{ConversationID: conversation, ClientMsgID: fmt.Sprint(turn), Text: "Sign in with the browser to " + site.URL + " and read the panel code"})
		submitted := false
		for {
			var pending api.CredentialsPendingResult
			if json.Unmarshal(call(api.MethodCredentialsPending, api.CredentialsPendingParams{ConversationID: conversation}), &pending) != nil {
				t.Fatal("invalid pending metadata")
			}
			if len(pending.Requests) > 0 {
				if turn != 1 || submitted || len(pending.Requests) != 1 || pending.Requests[0].Site != host.Hostname() {
					t.Fatal("unexpected credential card")
				}
				call(api.MethodCredentialsSubmit, api.CredentialsSubmitParams{RequestID: pending.Requests[0].ID, Values: map[string]string{"username": username, "password": password}, Remember: true})
				submitted = true
			}
			if !b.Master.Turns().Active(conversation) && len(b.Master.History(conversation, 0)) >= turn*2 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("chat login timed out")
			case <-time.After(10 * time.Millisecond):
			}
		}
		if turn == 1 && !submitted {
			t.Fatal("first login did not create a card")
		}
		history := call(api.MethodConversationHistory, api.ConversationHistoryParams{ConversationID: conversation, Limit: 50})
		if !strings.Contains(string(history), "DASH-4417") || logins.Load() != int32(turn) {
			t.Fatal("chat did not complete the authenticated login")
		}
		for _, secret := range []string{username, password} {
			if strings.Contains(string(history), secret) {
				t.Fatal("conversation history leaked credentials")
			}
		}
		out, err := svc.Exec.Execute(ctx, "browser_status", json.RawMessage(`{}`), tools.ExecuteOptions{})
		if err != nil || out.Status != tools.StatusOK || !strings.Contains(out.Output, `"sessions":[]`) {
			t.Fatal("chat retained browser sessions after completion")
		}
	}
	mu.Lock()
	encoded, _ := json.Marshal(observed)
	reused := remembered
	mu.Unlock()
	if !reused {
		t.Fatal("second login did not reuse remembered credential references")
	}
	events, _ := json.Marshal(bus.Replay(types.Cursor{}))
	requests := 0
	for _, event := range bus.Replay(types.Cursor{}) {
		if event.Type == api.EventCredentialsRequested {
			requests++
		}
	}
	if requests != 1 {
		t.Fatal("remembered login created another credential event")
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	logs, _ := os.ReadFile(logfile.Name())
	for _, secret := range []string{username, password} {
		for _, data := range [][]byte{encoded, events, logs} {
			if strings.Contains(string(data), secret) {
				t.Fatal("model context, events or daemon logs leaked credentials")
			}
		}
	}
	// Check the durable journal and every stored browser/tool artifact too.
	if err := db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, "SELECT payload FROM journal")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload string
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			if strings.Contains(payload, username) || strings.Contains(payload, password) {
				return fmt.Errorf("journal leaked credentials")
			}
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	var digests []string
	if err := db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, "SELECT digest FROM blob_refs")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var digest string
			if err := rows.Scan(&digest); err != nil {
				return err
			}
			digests = append(digests, digest)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if len(digests) == 0 {
		t.Fatal("browser captures were not stored")
	}
	for _, digest := range digests {
		// Open through the store so the check covers decompressed bytes.
		r, err := db.Blob().Open(ctx, digest)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), username) || strings.Contains(string(data), password) {
			t.Fatal("durable blob leaked credentials")
		}
	}
}
