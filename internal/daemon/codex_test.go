package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
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
	"github.com/CristhianRudolf7/excalibur-agent/internal/codexlink"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

type accountStoreFake struct {
	mu                 sync.Mutex
	tokens             codexlink.Tokens
	status             codexlink.Status
	fallback           codexlink.Status
	saveErr, deleteErr error
	saves              int
}

func (s *accountStoreFake) Status() codexlink.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
func (s *accountStoreFake) Save(tokens codexlink.Tokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.tokens = tokens
	s.saves++
	s.status = codexlink.Status{Connected: true, Source: "excalibur", AccountEmail: "person@example.test", PlanType: "plus", ExpiresAt: time.Now().Add(time.Hour)}
	return nil
}
func (s *accountStoreFake) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.tokens = codexlink.Tokens{}
	s.status = s.fallback
	return nil
}
func testIdentity() string {
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"person@example.test","https://api.openai.com/auth":{"chatgpt_account_id":"test-account","chatgpt_plan_type":"plus"}}`)) + ".test"
}
func accountFixture(t *testing.T) (*CodexConnection, *accountStoreFake, *atomic.Int32) {
	t.Helper()
	store := &accountStoreFake{}
	var calls atomic.Int32
	c := NewCodexConnection(t.TempDir())
	c.store = store
	c.listen = func() (net.Listener, error) { return nil, errors.New("port occupied") }
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("incorrect token exchange request")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("client_id") != codexClientID || r.Form.Get("redirect_uri") != c.redirectURI || r.Form.Get("code") != "test-code" {
			t.Error("incorrect token exchange fields")
		}
		verifier := r.Form.Get("code_verifier")
		if len(verifier) != 43 {
			t.Error("missing PKCE verifier")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": testIdentity(), "access_token": "test-access", "refresh_token": "test-refresh"})
	}))
	c.issuer = issuer.URL
	t.Cleanup(issuer.Close)
	t.Cleanup(c.Close)
	return c, store, &calls
}
func startAccount(t *testing.T, c *CodexConnection) *url.URL {
	t.Helper()
	out, err := c.Op(context.Background(), "codex.login.start", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var start struct {
		URL string `json:"auth_url"`
	}
	_ = json.Unmarshal(b, &start)
	u, err := url.Parse(start.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func TestCodexPKCEAndProtocol(t *testing.T) {
	// RFC 7636 Appendix B, independent of our random verifier implementation.
	if pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatal("incorrect S256 challenge")
	}
	c, _, _ := accountFixture(t)
	u := startAccount(t, c)
	q := u.Query()
	for key, want := range map[string]string{"client_id": codexClientID, "response_type": "code", "redirect_uri": "http://localhost:1455/auth/callback", "scope": "openid profile email offline_access", "code_challenge_method": "S256", "id_token_add_organizations": "true", "codex_cli_simplified_flow": "true", "originator": "codex_cli_rs"} {
		if q.Get(key) != want {
			t.Errorf("wrong %s", key)
		}
	}
	if q.Get("code_challenge") != pkceChallenge(c.pending.verifier) || len(q.Get("state")) != 43 {
		t.Fatal("invalid PKCE/state")
	}
	oldVerifier := c.pending.verifier
	oldState := q.Get("state")
	next := startAccount(t, c)
	if next.Query().Get("state") == oldState || c.pending.verifier == oldVerifier {
		t.Fatal("login values reused")
	}
	if strings.Contains(u.String(), oldVerifier) {
		t.Fatal("verifier exposed")
	}
	if _, err := c.complete(context.Background(), "test-code", oldState); err == nil {
		t.Fatal("superseded state accepted")
	}
}
func TestCodexCompleteValidatesAndSavesOnce(t *testing.T) {
	c, store, calls := accountFixture(t)
	u := startAccount(t, c)
	state := u.Query().Get("state")
	for _, bad := range []string{"", "wrong", strings.Repeat("x", 257)} {
		if _, err := c.complete(context.Background(), "test-code", bad); err == nil {
			t.Fatal("invalid state accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid state reached token endpoint")
	}
	out, err := c.complete(context.Background(), "test-code", state)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Connected || out.Source != "excalibur" || out.AccountEmail != "person@example.test" || out.PlanType != "plus" || out.ExpiresAt == nil {
		t.Fatal("missing public metadata")
	}
	if store.tokens.AccountID != "test-account" || store.tokens.AccessToken != "test-access" || store.tokens.RefreshToken != "test-refresh" || store.saves != 1 {
		t.Fatal("tokens not saved")
	}
	encoded, _ := json.Marshal(out)
	for _, private := range []string{"test-access", "test-refresh", testIdentity()} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("credential exposed")
		}
	}
	if _, err := c.complete(context.Background(), "test-code", state); err == nil || calls.Load() != 1 {
		t.Fatal("callback replay accepted")
	}
}
func TestCodexPastedRedirectAndExpiry(t *testing.T) {
	c, _, calls := accountFixture(t)
	state := startAccount(t, c).Query().Get("state")
	for _, input := range []string{"https://example.test/auth/callback?code=test-code&state=" + state, c.redirectURI + "?code=test-code&state=" + state + "&state=duplicate", c.redirectURI + "?error=denied&state=" + state, c.redirectURI + "?code=test-code&state=" + state + "#fragment"} {
		if _, err := c.complete(context.Background(), input, ""); err == nil {
			t.Fatal("invalid pasted redirect accepted")
		}
	}
	if _, err := c.complete(context.Background(), c.redirectURI+"?code=test-code&state="+state, "wrong"); err == nil {
		t.Fatal("conflicting state accepted")
	}
	if _, err := c.complete(context.Background(), c.redirectURI+"?code=test-code&state="+state, ""); err != nil {
		t.Fatal(err)
	}
	state = startAccount(t, c).Query().Get("state")
	c.mu.Lock()
	c.pending.expires = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if _, err := c.complete(context.Background(), "test-code", state); err == nil || calls.Load() != 1 {
		t.Fatal("expired login accepted")
	}
}

func TestCodexConcurrentCompletionsSaveOnce(t *testing.T) {
	c, store, calls := accountFixture(t)
	state := startAccount(t, c).Query().Get("state")
	ready := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		safego.Go("account-completion-test", nil, func() {
			<-ready
			_, err := c.complete(context.Background(), "test-code", state)
			results <- err
		})
	}
	close(ready)
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 || calls.Load() != 1 || store.saves != 1 {
		t.Fatal("concurrent callbacks did not consume the authorization exactly once")
	}
}
func TestCodexLoopbackCallbackAndCleanup(t *testing.T) {
	c, _, calls := accountFixture(t)
	var addr string
	c.listen = func() (net.Listener, error) {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err == nil {
			addr = ln.Addr().String()
			c.redirectURI = "http://" + addr + "/auth/callback"
		}
		return ln, err
	}
	state := startAccount(t, c).Query().Get("state")
	base := "http://" + addr
	for _, path := range []string{"/missing", "/auth/callback?code=test-code&state=wrong", "/auth/callback?error=denied&state=" + state} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode < 400 {
			t.Fatal("bad callback accepted")
		}
	}
	res, err := http.Get(base + "/auth/callback?code=test-code&state=" + state)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "Cuenta conectada") || res.Header.Get("Cache-Control") != "no-store" || calls.Load() != 1 {
		t.Fatal("loopback login failed")
	}
	c.Close()
	if _, err := c.start(); err == nil {
		t.Fatal("closed connection started")
	}
}
func TestCodexFailuresAreRedactedAndConsumed(t *testing.T) {
	for _, kind := range []string{"denied", "invalid-json", "missing-token", "invalid-identity", "save", "transport", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			c, store, _ := accountFixture(t)
			failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "denied":
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, "private-issuer-response")
				case "invalid-json":
					_, _ = io.WriteString(w, "private-issuer-response")
				case "redirect":
					http.Redirect(w, r, "http://127.0.0.1:1/private-issuer-response", http.StatusFound)
				default:
					id := testIdentity()
					if kind == "invalid-identity" {
						id = "private-issuer-response"
					}
					refresh := "test-refresh"
					if kind == "missing-token" {
						refresh = ""
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"id_token": id, "access_token": "test-access", "refresh_token": refresh})
				}
			}))
			t.Cleanup(failing.Close)
			c.issuer = failing.URL
			if kind == "save" {
				store.saveErr = errors.New("private-issuer-response")
			}
			if kind == "transport" {
				failing.Close()
			}
			state := startAccount(t, c).Query().Get("state")
			if _, err := c.complete(context.Background(), "test-code", state); err == nil || strings.Contains(err.Error(), "private-issuer-response") {
				t.Fatal("unsafe authorization failure")
			}
			if _, err := c.complete(context.Background(), "test-code", state); err == nil {
				t.Fatal("failed exchange replayed")
			}
		})
	}
}
func TestCodexRPCThroughGatewayAndLogout(t *testing.T) {
	c, store, calls := accountFixture(t)
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Codex: c})
	server := httptest.NewServer(gateway.New(gateway.DefaultConfig(), b, nil, nil))
	defer server.Close()
	rpc := func(method string, params any) api.Response {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "account-test", "method": method, "params": params})
		res, err := http.Post(server.URL+"/rpc", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out api.Response
		if json.NewDecoder(res.Body).Decode(&out) != nil {
			t.Fatal("invalid RPC response")
		}
		return out
	}
	if out := rpc("codex.status", map[string]any{}); out.Error != nil {
		t.Fatal(out.Error)
	}
	start := rpc("codex.login.start", map[string]any{})
	if start.Error != nil {
		t.Fatal(start.Error)
	}
	var auth struct {
		URL string `json:"auth_url"`
	}
	_ = json.Unmarshal(start.Result, &auth)
	u, _ := url.Parse(auth.URL)
	state := u.Query().Get("state")
	out := rpc("codex.login.complete", map[string]string{"code": "test-code", "state": state})
	if out.Error != nil {
		t.Fatal(out.Error)
	}
	out = rpc("codex.logout", map[string]any{})
	if out.Error != nil || store.Status().Connected || store.tokens.AccessToken != "" {
		t.Fatal("logout did not delete own connection")
	}
	for _, params := range []any{nil, []string{}, map[string]string{"unknown": "private"}} {
		if out := rpc("codex.status", params); out.Error == nil || out.Error.Code != api.CodeInvalidParams {
			t.Fatal("invalid parameters accepted")
		}
	}
	if out := rpc("codex.login.complete", map[string]string{"code": "test-code"}); out.Error == nil {
		t.Fatal("missing state accepted")
	}
	if out := rpc("codex.unknown", map[string]any{}); out.Error == nil || out.Error.Code != api.CodeMethodNotFound {
		t.Fatal("unknown method accepted")
	}
	state = startAccount(t, c).Query().Get("state")
	if _, err := c.Op(context.Background(), "codex.logout", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.complete(context.Background(), "test-code", state); err == nil || calls.Load() != 1 {
		t.Fatal("logout left pending login valid")
	}
	// An external CLI/env connection remains visible after deleting only our file.
	store.fallback = codexlink.Status{Connected: true, Source: "codex-cli"}
	fallback, err := c.Op(context.Background(), "codex.logout", nil)
	if err != nil || fallback.(CodexStatus).Source != "codex-cli" || !fallback.(CodexStatus).Connected {
		t.Fatal("logout hid the external account source")
	}
	store.deleteErr = errors.New("private-path")
	if _, err := c.Op(context.Background(), "codex.logout", nil); err == nil || strings.Contains(err.Error(), "private-path") {
		t.Fatal("unsafe delete failure")
	}
}
func TestCodexOwnStoreSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("EXCALIBUR_CODEX_AUTH_FILE", "")
	cliPath := filepath.Join(dir, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(cliPath), 0700); err != nil {
		t.Fatal(err)
	}
	const cliSentinel = `{"auth_mode":"chatgpt","tokens":{"access_token":"test-cli-sentinel"}}`
	if err := os.WriteFile(cliPath, []byte(cliSentinel), 0600); err != nil {
		t.Fatal(err)
	}
	c, _, _ := accountFixture(t)
	c.store = codexlink.NewStore(dir)
	state := startAccount(t, c).Query().Get("state")
	if _, err := c.complete(context.Background(), "test-code", state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "codexlink", "auth.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal("own account file missing")
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("account file permissions are not private")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.Mode != "chatgpt" || doc.Tokens.Account != "test-account" {
		t.Fatal("incorrect account file format")
	}
	if _, err := c.Op(context.Background(), "codex.logout", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("logout did not remove own file")
	}
	cliBytes, err := os.ReadFile(cliPath)
	if err != nil || string(cliBytes) != cliSentinel {
		t.Fatal("login/logout modified external CLI credentials")
	}
}
func TestCodexDaemonLifecycle(t *testing.T) {
	b := NewDaemonBackend(nil, nil, nil)
	d := New(Config{DataDir: t.TempDir()}, Deps{Backend: b})
	if _, err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.codex == nil {
		t.Fatal("daemon did not wire account service")
	}
	if b.Services != nil {
		t.Fatal("account wiring changed the embedded services fallback")
	}
	c := b.codex
	c.listen = func() (net.Listener, error) { return nil, errors.New("no callback") }
	startAccount(t, c)
	d.Stop(context.Background())
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed || c.pending != nil {
		t.Fatal("daemon left pending login alive")
	}
}
