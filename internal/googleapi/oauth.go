package googleapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

var requiredScopes = []string{"openid", "email", "https://mail.google.com/", "https://www.googleapis.com/auth/drive", "https://www.googleapis.com/auth/documents"}

// ConnectionStatus adds safe operational details without changing TokenProvider.
type ConnectionStatus struct {
	Status
	Configured    bool     `json:"configured"`
	Reason        string   `json:"reason,omitempty"`
	Warning       string   `json:"warning,omitempty"`
	MissingScopes []string `json:"missing_scopes,omitempty"`
}

// OAuthOptions injects storage, transport and endpoints for isolated daemon tests.
type OAuthOptions struct {
	ConfigDir   string
	ClientPath  string
	ImportPath  string
	RedirectURI string
	HTTPClient  *http.Client
	Vault       *vault.Vault
	UserInfoURL string
	RevokeURL   string
	RefreshTTL  *time.Duration
	Now         func() time.Time
}

type clientConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
	RedirectURIs []string `json:"redirect_uris"`
}
type tokenRecord struct {
	AccessToken   string    `json:"access_token,omitempty"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	AccessExpiry  time.Time `json:"access_expiry,omitempty"`
	RefreshIssued time.Time `json:"refresh_issued,omitempty"`
	RefreshExpiry time.Time `json:"refresh_expiry,omitempty"`
	Email         string    `json:"email,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
	Reason        string    `json:"reason,omitempty"`
}
type authorization struct {
	verifier string
	expires  time.Time
}

// OAuth is a single-account provider. The mutex also serializes exchanges,
// refreshes and disconnects so a late refresh cannot resurrect a disconnected account.
type OAuth struct {
	mu         sync.Mutex
	opts       OAuthOptions
	client     clientConfig
	configured bool
	token      tokenRecord
	pending    map[string]authorization
	warning    string
	loaded     bool
	ttl        time.Duration
}

var errStorage = errors.New("googleapi: token storage unavailable")
var errExchange = errors.New("googleapi: Google token exchange failed")
var errInvalidGrant = errors.New("googleapi: authorization expired or revoked; reconnect Google")

const vaultName = "google-oauth-account"

// NewOAuth reads configuration and imports local credentials without making network calls.
func NewOAuth(ctx context.Context, opts OAuthOptions) (*OAuth, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errStorage
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = filepath.Join(home, ".config", "excalibur")
	}
	if opts.ClientPath == "" {
		opts.ClientPath = os.Getenv("EXCALIBUR_GOOGLE_OAUTH_CLIENT")
	}
	if opts.ClientPath == "" {
		opts.ClientPath = filepath.Join(home, ".config", "excalibur", "credentials", "google-oauth.json")
	}
	if opts.ImportPath == "" {
		opts.ImportPath = filepath.Join(home, ".config", "excalibur", "credentials", "google-oauth-token.json")
	}
	if opts.RedirectURI == "" {
		opts.RedirectURI = os.Getenv("EXCALIBUR_GOOGLE_REDIRECT_URI")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.UserInfoURL == "" {
		opts.UserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
	}
	if opts.RevokeURL == "" {
		opts.RevokeURL = "https://oauth2.googleapis.com/revoke"
	}
	ttl := 7 * 24 * time.Hour
	if raw := os.Getenv("EXCALIBUR_GOOGLE_REFRESH_TTL"); raw != "" {
		ttl, err = time.ParseDuration(raw)
		if err != nil || ttl < 0 {
			return nil, errors.New("googleapi: invalid refresh TTL")
		}
	}
	if opts.RefreshTTL != nil {
		ttl = *opts.RefreshTTL
		if ttl < 0 {
			return nil, errors.New("googleapi: invalid refresh TTL")
		}
	}
	p := &OAuth{opts: opts, pending: map[string]authorization{}, ttl: ttl}
	raw, err := os.ReadFile(opts.ClientPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("googleapi: OAuth client file unavailable")
	}
	if err == nil {
		defer vault.ZeroBytes(raw)
		var doc struct {
			Web clientConfig `json:"web"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			return nil, errors.New("googleapi: invalid OAuth client configuration")
		}
		p.client = doc.Web
		if p.opts.RedirectURI == "" && len(p.client.RedirectURIs) > 0 {
			p.opts.RedirectURI = p.client.RedirectURIs[0]
		}
		if p.client.ClientID == "" || p.client.ClientSecret == "" || !validEndpoint(p.client.AuthURI) || !validEndpoint(p.client.TokenURI) || !validEndpoint(p.opts.RedirectURI) {
			return nil, errors.New("googleapi: incomplete OAuth web client configuration")
		}
		p.configured = true
	}
	if err := p.load(ctx); err != nil {
		return nil, err
	}
	return p, nil
}
func validEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))
}
func (p *OAuth) Status() Status { p.mu.Lock(); defer p.mu.Unlock(); return p.status().Status }
func (p *OAuth) ConnectionStatus(ctx context.Context) ConnectionStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		if err := p.load(ctx); err != nil {
			p.warning = "storage_unavailable"
		}
	}
	if p.warning == "file_storage" && p.opts.Vault != nil && !p.opts.Vault.IsLocked() {
		if p.persist(ctx) != nil {
			p.warning = "storage_unavailable"
		}
	}
	return p.status()
}
func (p *OAuth) status() ConnectionStatus {
	t := p.token
	connected := p.configured && t.Reason == "" && (t.RefreshToken != "" || t.AccessToken != "" && p.opts.Now().Before(t.AccessExpiry))
	reason := t.Reason
	if !p.configured {
		reason = "not_configured"
	} else if !p.loaded {
		reason = "vault_locked"
	} else if !connected && reason == "" {
		reason = "disconnected"
	}
	if !t.RefreshExpiry.IsZero() && !p.opts.Now().Before(t.RefreshExpiry) {
		connected = false
		reason = "refresh_expired"
	}
	missing := []string{}
	if connected {
		for _, s := range requiredScopes {
			if !hasScope(t.Scopes, s) {
				missing = append(missing, s)
			}
		}
	}
	warning := p.warning
	if p.opts.Vault != nil && p.opts.Vault.IsLocked() && connected && warning == "" {
		warning = "vault_locked"
	}
	return ConnectionStatus{Status: Status{Connected: connected, Email: t.Email, Scopes: slices.Clone(t.Scopes), ExpiresAt: t.RefreshExpiry}, Configured: p.configured, Reason: reason, Warning: warning, MissingScopes: missing}
}
func hasScope(scopes []string, s string) bool {
	if slices.Contains(scopes, s) {
		return true
	}
	return s == "email" && slices.Contains(scopes, "https://www.googleapis.com/auth/userinfo.email")
}
func randomValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("googleapi: random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (p *OAuth) Start(ctx context.Context) (string, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if !p.configured {
		return "", "", errors.New("googleapi: OAuth client is not configured")
	}
	state, err := randomValue()
	if err != nil {
		return "", "", err
	}
	verifier, err := randomValue()
	if err != nil {
		return "", "", err
	}
	for key, a := range p.pending {
		if !p.opts.Now().Before(a.expires) {
			delete(p.pending, key)
		}
	}
	// Bound abandoned authorizations without retaining any browser tokens.
	if len(p.pending) >= 32 {
		return "", "", errors.New("googleapi: too many pending connections")
	}
	p.pending[state] = authorization{verifier: verifier, expires: p.opts.Now().Add(10 * time.Minute)}
	hash := sha256.Sum256([]byte(verifier))
	u, _ := url.Parse(p.client.AuthURI)
	q := u.Query()
	for k, v := range map[string]string{"client_id": p.client.ClientID, "redirect_uri": p.opts.RedirectURI, "response_type": "code", "scope": strings.Join(requiredScopes, " "), "state": state, "access_type": "offline", "prompt": "consent", "include_granted_scopes": "true", "code_challenge_method": "S256", "code_challenge": base64.RawURLEncoding.EncodeToString(hash[:])} {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), state, nil
}
func (p *OAuth) Complete(ctx context.Context, code, state string) (ConnectionStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.pending[state]
	delete(p.pending, state)
	if !ok || code == "" || !p.opts.Now().Before(a.expires) {
		return p.status(), errors.New("googleapi: invalid or expired OAuth state; reconnect Google")
	}
	response, err := p.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {a.verifier}, "redirect_uri": {p.opts.RedirectURI}})
	if err != nil {
		return p.status(), err
	}
	if response.RefreshToken == "" {
		return p.status(), errors.New("googleapi: offline access was not granted; reconnect Google")
	}
	email, err := p.email(ctx, response.AccessToken)
	if err != nil {
		return p.status(), err
	}
	previous := p.token
	p.token = tokenRecord{Email: email, Scopes: strings.Fields(response.Scope)}
	p.apply(response)
	if err := p.persist(ctx); err != nil {
		p.token = previous
		return p.status(), err
	}
	p.loaded = true
	return p.status(), nil
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
}

func (p *OAuth) exchange(ctx context.Context, values url.Values) (tokenResponse, error) {
	values.Set("client_id", p.client.ClientID)
	values.Set("client_secret", p.client.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.client.TokenURI, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, errExchange
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := p.opts.HTTPClient.Do(req)
	if err != nil {
		return tokenResponse{}, errExchange
	}
	defer res.Body.Close()
	var t tokenResponse
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t) != nil {
		return t, errExchange
	}
	if t.Error == "invalid_grant" {
		return t, errInvalidGrant
	}
	if res.StatusCode != 200 || t.Error != "" || t.AccessToken == "" || t.ExpiresIn <= 0 {
		return tokenResponse{}, errExchange
	}
	return t, nil
}
func (p *OAuth) apply(t tokenResponse) {
	now := p.opts.Now()
	p.token.AccessToken = t.AccessToken
	p.token.AccessExpiry = now.Add(time.Duration(t.ExpiresIn) * time.Second)
	if t.RefreshToken != "" && t.RefreshToken != p.token.RefreshToken {
		p.token.RefreshToken = t.RefreshToken
		p.token.RefreshIssued = now
		p.token.RefreshExpiry = time.Time{}
		if p.ttl > 0 {
			p.token.RefreshExpiry = now.Add(p.ttl)
		}
	}
	if t.RefreshExpiresIn > 0 {
		p.token.RefreshExpiry = now.Add(time.Duration(t.RefreshExpiresIn) * time.Second)
	}
	if t.Scope != "" {
		p.token.Scopes = strings.Fields(t.Scope)
	}
}
func (p *OAuth) email(ctx context.Context, access string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.opts.UserInfoURL, nil)
	if err != nil {
		return "", errors.New("googleapi: account identity unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+access)
	res, err := p.opts.HTTPClient.Do(req)
	if err != nil {
		return "", errors.New("googleapi: account identity unavailable")
	}
	defer res.Body.Close()
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&claims) != nil || claims.Email == "" || !claims.Verified {
		return "", errors.New("googleapi: verified account email was not granted")
	}
	return claims.Email, nil
}
func (p *OAuth) AccessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !p.loaded {
		if err := p.load(ctx); err != nil {
			return "", err
		}
	}
	if !p.status().Connected {
		return "", ErrNotConnected
	}
	if p.opts.Now().Add(time.Minute).Before(p.token.AccessExpiry) {
		return p.token.AccessToken, nil
	}
	if p.token.RefreshToken == "" {
		return "", ErrNotConnected
	}
	t, err := p.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {p.token.RefreshToken}})
	if errors.Is(err, errInvalidGrant) {
		p.token = tokenRecord{Reason: "invalid_grant"}
		if p.persist(ctx) != nil {
			p.warning = "storage_unavailable"
		}
		return "", ErrNotConnected
	}
	if err != nil {
		return "", err
	}
	p.apply(t)
	if err := p.persist(ctx); err != nil {
		return "", err
	}
	return p.token.AccessToken, nil
}
func (p *OAuth) Disconnect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		if err := p.load(ctx); err != nil {
			return err
		}
		if !p.loaded {
			return errors.New("googleapi: unlock the vault to disconnect Google")
		}
	}
	token := p.token.RefreshToken
	if token == "" {
		token = p.token.AccessToken
	}
	// Forget locally even if Google's revocation service is unavailable.
	p.token = tokenRecord{Reason: "disconnected"}
	clear(p.pending)
	if err := p.persist(ctx); err != nil {
		return err
	}
	if token != "" {
		revokeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(revokeCtx, http.MethodPost, p.opts.RevokeURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if res, err := p.opts.HTTPClient.Do(req); err == nil {
				_ = res.Body.Close()
			}
		}
	}
	return nil
}
