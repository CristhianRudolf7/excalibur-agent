package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/coder/websocket"
	"log/slog"
)

func savedLoginRPC(t *testing.T, r *CredentialRegistry, p api.CredentialsSaveLoginParams) (any, error) {
	t.Helper()
	b := NewDaemonBackend(nil, nil, r.bus)
	b.credentials = r
	raw, _ := json.Marshal(p)
	out, err := b.Passthrough(context.Background(), api.MethodCredentialsSaveLogin, raw)
	if !bytes.Equal(raw, make([]byte, len(raw))) {
		t.Fatal("RPC retained owned values")
	}
	return out, err
}
func savedLoginParams() api.CredentialsSaveLoginParams {
	return api.CredentialsSaveLoginParams{Site: "Accounts.Example.test", Label: "  Intranet  ", StartURL: "https://start.example.test/", Values: map[string]string{"username": "credential-user-sentinel", "password": "credential-password-sentinel"}}
}
func TestSavedLoginRPCReuseReplaceAndPolicy(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)
	r, v, _ := credentialTestRegistry(t)
	ctx := context.WithValue(context.Background(), credentialOriginKey{}, credentialOrigin{conversation: "saved", run: "saved-run"})
	var previous map[string]string
	for i := 0; i < 2; i++ {
		out, err := savedLoginRPC(t, r, savedLoginParams())
		if err != nil || out != (api.CredentialsOKResult{OK: true}) {
			t.Fatal("save RPC failed", err)
		}
		result, err := r.request(ctx, credentialArgs{Site: "accounts.example.test"})
		if err != nil || result.Status != "stored" || len(result.Refs) != 2 {
			t.Fatal("saved login not reused", err)
		}
		if len(r.Pending("").Requests) != 0 || r.bus.Len() != 0 {
			t.Fatal("saved login opened a form")
		}
		for field, ref := range result.Refs {
			md, err := v.Describe(ctx, credentialEntry(ref))
			if err != nil || md.Meta["credential_label"] != "Intranet" || md.Meta["credential_start_url"] != "https://start.example.test/" || md.ExpiresAt != nil || md.Meta["credential_remembered"] != "true" {
				t.Fatal("incorrect saved metadata", err)
			}
			resolved, err := v.CheckFormPolicy(ctx, md.Name, "browser_act", "saved-run", "", "accounts.example.test")
			if err != nil {
				t.Fatal("exact host rejected", err)
			}
			if string(resolved.Value) != savedLoginParams().Values[field] {
				t.Fatal("wrong resolved value")
			}
			vault.ZeroBytes(resolved.Value)
			for _, host := range []string{"example.test", "sub.accounts.example.test", "accounts.example.test.evil.test"} {
				if _, err := v.CheckFormPolicy(ctx, md.Name, "browser_act", "saved-run", "", host); !errors.Is(err, vault.ErrDenied) {
					t.Fatal("another host accepted")
				}
			}
		}
		for _, ref := range previous {
			if _, err := v.Describe(ctx, credentialEntry(ref)); !errors.Is(err, vault.ErrNotFound) {
				t.Fatal("old bundle retained")
			}
		}
		previous = result.Refs
		entries, _ := v.ListMetadata(ctx)
		if len(entries) != 2 {
			t.Fatal("replacement mixed bundles")
		}
		encoded, _ := json.Marshal([]any{out, result, entries, r.bus.Replay(types.Cursor{}), logs.String()})
		if strings.Contains(string(encoded), "credential-user-sentinel") || strings.Contains(string(encoded), "credential-password-sentinel") {
			t.Fatal("value leaked into metadata, events or output")
		}
	}
}
func TestSavedLoginValidationAndZeroing(t *testing.T) {
	r, v, _ := credentialTestRegistry(t)
	cases := []struct {
		name   string
		change func(*api.CredentialsSaveLoginParams)
	}{
		{"url site", func(p *api.CredentialsSaveLoginParams) { p.Site = "https://example.test/" }},
		{"port", func(p *api.CredentialsSaveLoginParams) { p.Site = "example.test:443" }},
		{"wildcard", func(p *api.CredentialsSaveLoginParams) { p.Site = "*.example.test" }},
		{"empty host", func(p *api.CredentialsSaveLoginParams) { p.Site = "" }},
		{"label", func(p *api.CredentialsSaveLoginParams) { p.Label = strings.Repeat("界", 81) }},
		{"userinfo", func(p *api.CredentialsSaveLoginParams) { p.StartURL = "https://user@example.test/" }},
		{"scheme", func(p *api.CredentialsSaveLoginParams) { p.StartURL = "file:///tmp/login" }},
		{"relative", func(p *api.CredentialsSaveLoginParams) { p.StartURL = "/login" }},
		{"url length", func(p *api.CredentialsSaveLoginParams) {
			p.StartURL = "https://example.test/" + strings.Repeat("x", 512)
		}},
		{"missing password", func(p *api.CredentialsSaveLoginParams) { delete(p.Values, "password") }},
		{"missing identity", func(p *api.CredentialsSaveLoginParams) { delete(p.Values, "username") }},
		{"otp", func(p *api.CredentialsSaveLoginParams) { p.Values["otp"] = "123456" }},
		{"empty", func(p *api.CredentialsSaveLoginParams) { p.Values["password"] = "" }},
		{"too long", func(p *api.CredentialsSaveLoginParams) { p.Values["password"] = strings.Repeat("x", 16385) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := savedLoginParams()
			tc.change(&p)
			values := map[string][]byte{}
			for f, v := range p.Values {
				values[f] = []byte(v)
			}
			owned := values["password"]
			err := r.SaveLogin(context.Background(), p.Site, p.Label, p.StartURL, values)
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Code != api.CodeInvalidParams {
				t.Fatal("invalid saved login accepted")
			}
			if len(values) != 0 || strings.Trim(string(owned), "\x00") != "" {
				t.Fatal("rejected values retained")
			}
		})
	}
	entries, _ := v.ListMetadata(context.Background())
	if len(entries) != 0 {
		t.Fatal("validation created entries")
	}
	// An email can replace username, and the limit is measured in runes.
	p := savedLoginParams()
	p.Label = strings.Repeat("界", 80)
	p.Values["email"] = p.Values["username"]
	delete(p.Values, "username")
	if _, err := savedLoginRPC(t, r, p); err != nil {
		t.Fatal("valid email login rejected", err)
	}
	v.LockVault()
	values := credentialTestValues()
	owned := values["password"]
	err := r.SaveLogin(context.Background(), p.Site, p.Label, p.StartURL, values)
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Message != "vault_locked" || len(values) != 0 || strings.Trim(string(owned), "\x00") != "" {
		t.Fatal("locked save mapping or clearing failed")
	}
}
func TestSavedLoginPartialBundleRollback(t *testing.T) {
	r, v, _ := credentialTestRegistry(t)
	ctx := context.Background()
	const base = "credentials-rollback"
	_, err := v.Create(ctx, vault.CreateParams{Name: base + "-username", Type: vault.TypeWebsitePassword, Value: []byte("existing")})
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	values := credentialTestValues()
	_, err = r.storeBundleLocked(ctx, base, "example.test", "", values, true, nil, time.Time{})
	clearCredentialValues(values)
	r.mu.Unlock()
	if err == nil {
		t.Fatal("partial failure not returned")
	}
	if _, err := v.Describe(ctx, base+"-password"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatal("partial bundle not rolled back")
	}
	if _, err := v.Describe(ctx, base+"-username"); err != nil {
		t.Fatal("existing entry removed")
	}
}
func TestSavedLoginLockedPendingFormSubmission(t *testing.T) {
	r, v, _ := credentialTestRegistry(t)
	if _, err := savedLoginRPC(t, r, savedLoginParams()); err != nil {
		t.Fatal(err)
	}
	v.LockVault()
	event, done := startCredentialRequest(t, r, context.Background(), credentialArgs{Site: "accounts.example.test"})
	if err := v.UnlockVault(context.Background(), []byte(testPassphrase)); err != nil {
		t.Fatal(err)
	}
	if err := r.Submit(context.Background(), event.RequestID, credentialTestValues(), false); err != nil {
		t.Fatal(err)
	}
	if (<-done).Status != "provided" {
		t.Fatal("pending form did not resume")
	}
}

// Keep the borrowed params until response encoding to verify gateway clearing.
type savedLoginBufferBackend struct{ gateway.Backend }
type savedLoginBufferProbe struct{ params json.RawMessage }

func (*savedLoginBufferBackend) Passthrough(_ context.Context, _ string, p json.RawMessage) (any, error) {
	return savedLoginBufferProbe{p}, nil
}
func (p savedLoginBufferProbe) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]bool{"cleared": bytes.Equal(p.params, make([]byte, len(p.params)))})
}
func TestSavedLoginGatewayRedaction(t *testing.T) {
	gate := gateway.New(gateway.DefaultConfig(), &savedLoginBufferBackend{gateway.NewMemoryBackend(nil)}, nil, nil)
	server := httptest.NewServer(gate)
	defer server.Close()
	frame := func() []byte {
		req, _ := api.NewRequest("save", api.MethodCredentialsSaveLogin, savedLoginParams())
		raw, _ := json.Marshal(req)
		return raw
	}
	check := func(raw []byte) {
		t.Helper()
		var response api.Response
		if json.Unmarshal(raw, &response) != nil || response.Error != nil || !strings.Contains(string(response.Result), `"cleared":true`) {
			t.Fatal("transport retained credential params")
		}
	}
	for _, batch := range []bool{false, true} {
		raw := frame()
		if batch {
			raw = append(append([]byte{'['}, raw...), ']')
		}
		res, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if batch {
			var out []json.RawMessage
			err = json.NewDecoder(res.Body).Decode(&out)
			if err != nil || len(out) != 1 {
				t.Fatal("invalid batch response")
			}
			check(out[0])
		} else {
			var out json.RawMessage
			if json.NewDecoder(res.Body).Decode(&out) != nil {
				t.Fatal("invalid response")
			}
			check(out)
		}
		res.Body.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := conn.Write(ctx, websocket.MessageText, frame()); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check(raw)
}

func TestSavedLoginHintMetadataSanitizationAndLock(t *testing.T) {
	r, v, _ := credentialTestRegistry(t)
	p := savedLoginParams()
	p.Label = "\"Intranet\"\n\t'Office'\u202e"
	if _, err := savedLoginRPC(t, r, p); err != nil {
		t.Fatal(err)
	}
	logins := r.savedLogins(context.Background())
	if len(logins) != 1 {
		t.Fatal("saved metadata missing")
	}
	hint := savedLoginHint(logins)
	for _, part := range []string{`"IntranetOffice"`, "https://start.example.test/", "accounts.example.test", "actually showing the login form", "no form appears"} {
		if !strings.Contains(hint, part) {
			t.Fatal("hint omitted safe guidance")
		}
	}
	for _, private := range []string{"credential-user-sentinel", "credential-password-sentinel", "\u202e", "\t", "'Office'"} {
		if strings.Contains(hint, private) {
			t.Fatal("hint retained unsafe content")
		}
	}
	many := make([]savedLogin, 25)
	for i := range many {
		many[i] = logins[0]
	}
	if strings.Count(savedLoginHint(many), " login host accounts.example.test.") != 20 {
		t.Fatal("hint is unbounded")
	}
	v.LockVault()
	if got := r.savedLogins(context.Background()); len(got) != 0 || savedLoginHint(got) != "" {
		t.Fatal("locked vault disclosed login hints")
	}
}
func TestSavedLoginBrowserActivation(t *testing.T) {
	r, v, _ := credentialTestRegistry(t)
	p := savedLoginParams()
	p.Label = "Intranet Acme the los"
	p.Site = "campuslogin.acme.example"
	if _, err := savedLoginRPC(t, r, p); err != nil {
		t.Fatal(err)
	}
	_, s := assistantTestWiring(t, false, nil)
	s.Credentials = r
	positive := []string{"https://a.invalid/", "HTTP://a.invalid", "www.a.invalid", "example.org", "127.0.0.1", "intranet", "internet", "Open the INTERNET", "campus virtual", "portal", "website", "sitio web", "página web", "log in", "login", "sign in", "iniciar sesión", "inicia sesión", "navega", "browse", "Check ACME", "read campuslogin"}
	for _, request := range positive {
		if !s.savedBrowserIntent(request) {
			t.Errorf("missing intent for %q", request)
		}
		activation := s.chatToolActivation(request, "test", nil)
		for _, name := range []string{"request_credentials", "browser_open_session", "browser_act"} {
			if !slices.Contains(activation.Names, name) {
				t.Errorf("%q missing %s", request, name)
			}
		}
	}
	for _, request := range []string{"hello", "the los com edu", "acmeish", "supercampuslogin", "websites", "portalized", "internetwork", "change main.go", "3.14159", "compare numbers"} {
		if s.savedBrowserIntent(request) {
			t.Errorf("unexpected intent for %q", request)
		}
	}
	v.LockVault()
	for _, request := range []string{"ACME", "campuslogin"} {
		if s.savedBrowserIntent(request) {
			t.Fatal("locked login token activated browser")
		}
	}
	for _, request := range []string{"intranet", "internet", "example.org", "browse"} {
		if !s.savedBrowserIntent(request) {
			t.Fatal("locked vault blocked generic intent")
		}
	}
}
func TestSavedLoginBrowserAssistantLimits(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_STEPS", "")
	for _, name := range []string{"browser_navigate", "request_credentials"} {
		steps, timeout := browserAssistantLimits([]tools.Definition{{Name: name}}, 12, 2*time.Minute)
		if steps != 48 || timeout != 12*time.Minute {
			t.Fatal("browser defaults not applied")
		}
	}
	steps, timeout := browserAssistantLimits([]tools.Definition{{Name: "clock"}}, 12, 2*time.Minute)
	if steps != 12 || timeout != 2*time.Minute {
		t.Fatal("non-browser limits changed")
	}
	t.Setenv("EXCALIBUR_MASTER_STEPS", "31")
	steps, timeout = browserAssistantLimits([]tools.Definition{{Name: "browser_act"}}, 31, 15*time.Minute)
	if steps != 31 || timeout != 15*time.Minute {
		t.Fatal("explicit steps or longer deadline lost")
	}
	t.Setenv("EXCALIBUR_MASTER_STEPS", "invalid")
	steps, _ = browserAssistantLimits([]tools.Definition{{Name: "browser_act"}}, 12, time.Minute)
	if steps != 48 {
		t.Fatal("invalid env suppressed browser default")
	}
}

func TestSavedLoginHintOnlyInBrowserTurn(t *testing.T) {
	r, _, _ := credentialTestRegistry(t)
	if _, err := savedLoginRPC(t, r, savedLoginParams()); err != nil {
		t.Fatal(err)
	}
	b, s := assistantTestWiring(t, false, nil)
	s.Credentials = r
	var sawHint bool
	a := &assistantTurns{backend: b, services: s, modelID: "test", endpoint: timingEndpoint{chat: func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		sawHint = false
		for _, message := range req.Messages {
			sawHint = sawHint || strings.Contains(message.Content, "Saved logins")
			if strings.Contains(message.Content, "credential-user-sentinel") || strings.Contains(message.Content, "credential-password-sentinel") {
				t.Fatal("model hint contains a value")
			}
		}
		if sawHint {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) < 11*time.Minute {
				t.Fatal("browser default deadline not applied")
			}
		}
		return textReply("Ready"), nil
	}}}
	t.Setenv("EXCALIBUR_MASTER_TIMEOUT", "")
	t.Setenv("EXCALIBUR_MASTER_STEPS", "")
	if _, _, err := a.CompleteTurn(context.Background(), "saved-browser", master.Normalized{Text: "Use the website"}); err != nil || !sawHint {
		t.Fatal("browser turn missing saved hint", err)
	}
	if _, _, err := a.CompleteTurn(context.Background(), "saved-plain", master.Normalized{Text: "Hello"}); err != nil || sawHint {
		t.Fatal("plain turn received saved hint", err)
	}
}
