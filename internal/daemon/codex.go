package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/codexlink"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// Browser login protocol pinned to the CLI rust-v0.101.0 login server.
const (
	codexIssuer      = "https://auth.openai.com"
	codexClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexRedirectURI = "http://localhost:1455/auth/callback"
	codexScope       = "openid profile email offline_access"
	codexLoginTTL    = 10 * time.Minute
)

type codexTokenStore interface {
	Status() codexlink.Status
	Save(codexlink.Tokens) error
	Delete() error
}

// CodexStatus is the public account metadata. Credentials never cross the RPC boundary.
type CodexStatus struct {
	Connected    bool       `json:"connected"`
	Source       string     `json:"source"`
	AccountEmail string     `json:"account_email"`
	PlanType     string     `json:"plan_type"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// CodexConnection owns one bounded browser sign-in and its temporary callback server.
// All completion/logout operations serialize so a canceled exchange cannot save later.
type CodexConnection struct {
	mu          sync.Mutex
	store       codexTokenStore
	issuer      string
	redirectURI string
	client      *http.Client
	listen      func() (net.Listener, error)
	pending     *codexPending
	closed      bool
}
type codexPending struct {
	state, verifier string
	expires         time.Time
	server          *http.Server
	timer           *time.Timer
}

func NewCodexConnection(dataDir string) *CodexConnection {
	return &CodexConnection{
		store: codexlink.NewStore(dataDir), issuer: codexIssuer, redirectURI: codexRedirectURI,
		client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		listen: func() (net.Listener, error) { return net.Listen("tcp4", "127.0.0.1:1455") },
	}
}
func (c *CodexConnection) status() CodexStatus {
	s := c.store.Status()
	out := CodexStatus{Connected: s.Connected, Source: s.Source, AccountEmail: s.AccountEmail, PlanType: s.PlanType}
	if !s.ExpiresAt.IsZero() {
		out.ExpiresAt = &s.ExpiresAt
	}
	return out
}
func codexError(code int, message string) error { return &api.Error{Code: code, Message: message} }
func codexInvalid(message string) error         { return codexError(api.CodeInvalidParams, message) }
func randomOAuthValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *CodexConnection) start() (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, codexInvalid("Account connection is closed")
	}
	state, err := randomOAuthValue()
	if err != nil {
		return nil, codexInvalid("Could not begin account connection")
	}
	verifier, err := randomOAuthValue()
	if err != nil {
		return nil, codexInvalid("Could not begin account connection")
	}
	c.clearPending()
	p := &codexPending{state: state, verifier: verifier, expires: time.Now().Add(codexLoginTTL)}
	c.pending = p
	// An occupied callback port must not interfere with another application.
	// Users can still paste the final redirect URL from another device.
	if ln, err := c.listen(); err == nil {
		p.server = &http.Server{Handler: http.HandlerFunc(c.callback), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 5 * time.Second}
		safego.Go("account-login-callback", nil, func() { _ = p.server.Serve(ln) })
	}
	p.timer = time.AfterFunc(codexLoginTTL, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.pending == p {
			c.clearPending()
		}
	})
	q := url.Values{"response_type": {"code"}, "client_id": {codexClientID}, "redirect_uri": {c.redirectURI}, "scope": {codexScope}, "code_challenge": {pkceChallenge(verifier)}, "code_challenge_method": {"S256"}, "id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}, "originator": {"codex_cli_rs"}, "state": {state}}
	return struct {
		AuthURL string `json:"auth_url"`
	}{c.issuer + "/oauth/authorize?" + q.Encode()}, nil
}
func (c *CodexConnection) clearPending() {
	if p := c.pending; p != nil {
		if p.timer != nil {
			p.timer.Stop()
		}
		if p.server != nil {
			_ = p.server.Close()
		}
		c.pending = nil
	}
}

// Close releases pending login state and the temporary listener.
func (c *CodexConnection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.clearPending()
}

func parseCodexCompletion(code, state, redirectURI string) (string, string, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if len(code) > 8192 || len(state) > 256 {
		return "", "", codexInvalid("Invalid account callback")
	}
	if strings.Contains(code, "://") {
		u, err := url.Parse(code)
		if err != nil {
			return "", "", codexInvalid("Invalid account callback")
		}
		expected, _ := url.Parse(redirectURI)
		if u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.User != nil || u.Fragment != "" {
			return "", "", codexInvalid("Invalid account callback URL")
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["code"]) != 1 || len(q["state"]) != 1 || q.Has("error") {
			return "", "", codexInvalid("Account authorization was not completed")
		}
		if state != "" && subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
			return "", "", codexInvalid("Account callback state mismatch")
		}
		code, state = q.Get("code"), q.Get("state")
	}
	if code == "" || state == "" || len(state) > 256 {
		return "", "", codexInvalid("Account callback needs code and state")
	}
	return code, state, nil
}

func (c *CodexConnection) complete(ctx context.Context, code, state string) (CodexStatus, error) {
	code, state, err := parseCodexCompletion(code, state, c.redirectURI)
	if err != nil {
		return CodexStatus{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.pending
	if c.closed || p == nil || time.Now().After(p.expires) {
		c.clearPending()
		return CodexStatus{}, codexInvalid("Account sign-in expired; connect again")
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(p.state)) != 1 {
		return CodexStatus{}, codexInvalid("Account callback state mismatch")
	}
	// Consume once before exchanging: replay and simultaneous callbacks cannot save twice.
	// Keep the server open until its current callback response has been written.
	c.pending = nil
	if p.timer != nil {
		p.timer.Stop()
	}
	if p.server != nil {
		defer func() {
			safego.Go("account-callback-shutdown", nil, func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = p.server.Shutdown(shutdownCtx)
			})
		}()
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {codexClientID}, "code": {code}, "redirect_uri": {c.redirectURI}, "code_verifier": {p.verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return CodexStatus{}, codexInvalid("Could not exchange account authorization")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.client.Do(req)
	if err != nil {
		return CodexStatus{}, codexInvalid("Could not exchange account authorization; connect again")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return CodexStatus{}, codexInvalid("Account authorization was rejected; connect again")
	}
	var tokens struct {
		ID      string `json:"id_token"`
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tokens) != nil || tokens.ID == "" || tokens.Access == "" || tokens.Refresh == "" {
		return CodexStatus{}, codexInvalid("Invalid account authorization response; connect again")
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	parts := strings.Split(tokens.ID, ".")
	if len(parts) != 3 {
		return CodexStatus{}, codexInvalid("Invalid account identity response")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return CodexStatus{}, codexInvalid("Invalid account identity response")
	}
	if c.store.Save(codexlink.Tokens{IDToken: tokens.ID, AccessToken: tokens.Access, RefreshToken: tokens.Refresh, AccountID: claims.Auth.AccountID}) != nil {
		return CodexStatus{}, codexError(api.CodeInternal, "Could not save account connection")
	}
	return c.status(), nil
}

func (c *CodexConnection) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet || r.URL.Path != "/auth/callback" {
		http.NotFound(w, r)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q["code"]) != 1 || len(q["state"]) != 1 || q.Has("error") {
		http.Error(w, "Sign-in was not completed. Return to Settings. / No se completó la conexión. Vuelve a Ajustes.", http.StatusBadRequest)
		return
	}
	if _, err = c.complete(r.Context(), q.Get("code"), q.Get("state")); err != nil {
		http.Error(w, "Sign-in failed. Return to Settings and connect again. / Falló la conexión. Vuelve a Ajustes e inténtalo de nuevo.", http.StatusBadRequest)
		return
	}
	_, _ = io.WriteString(w, "Account connected. Return to Settings. / Cuenta conectada. Vuelve a Ajustes.")
}

func codexDecode(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return codexInvalid("Invalid account parameters")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil || !errors.Is(dec.Decode(new(any)), io.EOF) {
		return codexInvalid("Invalid account parameters")
	}
	return nil
}
func (c *CodexConnection) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method != "codex.status" && method != "codex.login.start" && method != "codex.login.complete" && method != "codex.logout" {
		return nil, codexError(api.CodeMethodNotFound, "Unknown account method")
	}
	if c == nil {
		return nil, codexError(api.CodeInternal, "Account connection unavailable")
	}
	if method == "codex.login.complete" {
		var p struct {
			Code  string `json:"code"`
			State string `json:"state"`
		}
		if err := codexDecode(raw, &p); err != nil {
			return nil, err
		}
		return c.complete(ctx, p.Code, p.State)
	}
	if err := codexDecode(raw, &struct{}{}); err != nil {
		return nil, err
	}
	if method == "codex.login.start" {
		return c.start()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if method == "codex.logout" {
		c.clearPending()
		if c.store.Delete() != nil {
			return nil, codexError(api.CodeInternal, "Could not remove account connection")
		}
	}
	return c.status(), nil
}
