package googleapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

type fixture struct {
	p        *OAuth
	now      time.Time
	refresh  atomic.Int32
	revoked  atomic.Bool
	invalid  atomic.Bool
	scope    string
	verifier string
}

func setup(t *testing.T, v *vault.Vault) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), scope: strings.Join(requiredScopes, " ")}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost {
				t.Error("exchange must POST")
			}
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "fake-client-secret" {
				t.Error("missing client authentication")
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				f.refresh.Add(1)
				if f.invalid.Load() {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"private-token-detail"}`))
					return
				}
			} else {
				f.verifier = r.Form.Get("code_verifier")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fake-access", "refresh_token": "fake-refresh", "expires_in": 3600, "scope": f.scope})
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer fake-access" {
				t.Error("missing bearer")
			}
			_, _ = w.Write([]byte(`{"email":"person@example.test","email_verified":true}`))
		case "/revoke":
			_ = r.ParseForm()
			f.revoked.Store(r.Form.Get("token") == "fake-refresh")
			w.WriteHeader(503)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(s.Close)
	dir := t.TempDir()
	client := filepath.Join(dir, "client.json")
	raw, _ := json.Marshal(map[string]any{"web": clientConfig{ClientID: "fake-client", ClientSecret: "fake-client-secret", AuthURI: s.URL + "/auth", TokenURI: s.URL + "/token", RedirectURIs: []string{s.URL + "/callback"}}})
	if err := os.WriteFile(client, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	f.p, err = NewOAuth(context.Background(), OAuthOptions{ConfigDir: dir, ClientPath: client, ImportPath: filepath.Join(dir, "import.json"), Vault: v, HTTPClient: s.Client(), UserInfoURL: s.URL + "/userinfo", RevokeURL: s.URL + "/revoke", Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) connect(t *testing.T) ConnectionStatus {
	t.Helper()
	_, state, err := f.p.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.p.Complete(context.Background(), "fake-code", state)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestOAuthStartCompletePKCEAndSingleUse(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	auth, state, err := f.p.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(auth)
	q := u.Query()
	for k, v := range map[string]string{"response_type": "code", "access_type": "offline", "prompt": "consent", "include_granted_scopes": "true", "state": state, "code_challenge_method": "S256", "scope": strings.Join(requiredScopes, " ")} {
		if q.Get(k) != v {
			t.Errorf("authorization parameter %s", k)
		}
	}
	s, err := f.p.Complete(ctx, "fake-code", state)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(f.verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatal("PKCE mismatch")
	}
	if !s.Connected || s.Email != "person@example.test" || !s.ExpiresAt.Equal(f.now.Add(7*24*time.Hour)) || s.Warning != "file_storage" {
		t.Fatalf("unexpected safe status: %+v", s)
	}
	if _, err := f.p.Complete(ctx, "fake-code", state); err == nil {
		t.Fatal("replayed state accepted")
	}
	if _, err := f.p.Complete(ctx, "fake-code", "wrong-state"); err == nil {
		t.Fatal("unknown state accepted")
	}
	_, state, _ = f.p.Start(ctx)
	f.now = f.now.Add(10 * time.Minute)
	if _, err := f.p.Complete(ctx, "fake-code", state); err == nil {
		t.Fatal("expired state accepted")
	}
	info, err := os.Stat(f.p.path())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("fallback must be private")
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "fake-") {
		t.Fatal("secret in status")
	}
}
func TestRefreshSingleFlightInvalidGrantAndDisconnect(t *testing.T) {
	f := setup(t, nil)
	f.connect(t)
	f.now = f.now.Add(59*time.Minute + 30*time.Second)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		safego.Go("google-refresh-test", nil, func() {
			defer wg.Done()
			token, err := f.p.AccessToken(context.Background())
			if err != nil || token != "fake-access" {
				t.Error("refresh failed")
			}
		})
	}
	wg.Wait()
	if f.refresh.Load() != 1 {
		t.Fatalf("refresh requests: %d", f.refresh.Load())
	}
	f.now = f.now.Add(time.Hour)
	f.invalid.Store(true)
	if _, err := f.p.AccessToken(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("expected disconnected: %v", err)
	}
	if s := f.p.ConnectionStatus(context.Background()); s.Connected || s.Reason != "invalid_grant" {
		t.Fatal("invalid grant did not disconnect")
	}
	p, err := NewOAuth(context.Background(), f.p.opts)
	if err != nil || p.Status().Connected {
		t.Fatal("invalid grant resurrected")
	}
	f.invalid.Store(false)
	f.connect(t)
	if err := f.p.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !f.revoked.Load() || f.p.Status().Connected {
		t.Fatal("disconnect did not revoke and clear")
	}
	p, err = NewOAuth(context.Background(), f.p.opts)
	if err != nil || p.Status().Connected {
		t.Fatal("disconnect resurrected")
	}
}
func TestImportOnceMissingScopesAndExpiry(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"import@example.test","iat":1793707200}`))
	raw, _ := json.Marshal(map[string]any{"access_token": "fake-access", "refresh_token": "fake-refresh", "expires_in": 3600, "scope": strings.Join(requiredScopes, " "), "id_token": "x." + claims + ".x"})
	if err := os.WriteFile(f.p.opts.ImportPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := NewOAuth(ctx, f.p.opts)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Status().Connected || p.Status().Email != "import@example.test" {
		t.Fatal("import missing")
	}
	if err := p.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = NewOAuth(ctx, f.p.opts)
	if err != nil || p.Status().Connected {
		t.Fatal("import must run only once")
	}
	f.scope = "openid email https://www.googleapis.com/auth/drive"
	s := f.connect(t)
	if len(s.MissingScopes) != 2 {
		t.Fatal("missing permission list")
	}
	f.now = f.now.Add(7 * 24 * time.Hour)
	if f.p.Status().Connected || f.p.ConnectionStatus(ctx).Reason != "refresh_expired" {
		t.Fatal("expiry not surfaced")
	}
}
func testVault(t *testing.T) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	pf := filepath.Join(dir, "passphrase")
	if err := os.WriteFile(pf, []byte("fake-test-passphrase"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(context.Background(), filepath.Join(dir, "vault.db"), vault.Options{PassphraseFile: pf})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v
}
func TestVaultFallbackMigrationAndLockedDisconnect(t *testing.T) {
	ctx := context.Background()
	v := testVault(t)
	f := setup(t, v)
	f.connect(t)
	if _, err := os.Stat(f.p.path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("vault connection left plaintext token")
	}
	p, err := NewOAuth(ctx, f.p.opts)
	if err != nil || !p.Status().Connected {
		t.Fatal("vault reload failed")
	}
	v.LockVault()
	f.now = f.now.Add(time.Hour)
	if _, err := f.p.AccessToken(ctx); err != nil {
		t.Fatal(err)
	}
	if f.p.ConnectionStatus(ctx).Warning != "file_storage" {
		t.Fatal("locked refresh must warn")
	}
	if err := v.Unlock(ctx, []byte("fake-test-passphrase")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.AccessToken(ctx); err != nil {
		t.Fatal(err)
	}
	if f.p.ConnectionStatus(ctx).Warning != "" {
		t.Fatal("unlocked access must migrate")
	}
	if _, err := os.Stat(f.p.path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("migration left plaintext")
	}
	v.LockVault()
	p, err = NewOAuth(ctx, f.p.opts)
	if err != nil {
		t.Fatal(err)
	}
	if p.ConnectionStatus(ctx).Reason != "vault_locked" {
		t.Fatal("locked stored token must wait")
	}
	if err := f.p.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(ctx, []byte("fake-test-passphrase")); err != nil {
		t.Fatal(err)
	}
	p, err = NewOAuth(ctx, f.p.opts)
	if err != nil || p.Status().Connected {
		t.Fatal("stale vault token resurrected")
	}
	if _, err := v.Describe(ctx, vaultName); !errors.Is(err, vault.ErrNotFound) {
		t.Fatal("disconnect did not delete stale vault record")
	}
}
func TestNotConfiguredAndUnknownTTL(t *testing.T) {
	f := setup(t, nil)
	opts := f.p.opts
	opts.ClientPath = filepath.Join(t.TempDir(), "absent")
	p, err := NewOAuth(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if p.ConnectionStatus(context.Background()).Configured || p.Status().Connected {
		t.Fatal("absent client configured")
	}
	if _, _, err := p.Start(context.Background()); err == nil {
		t.Fatal("started without client")
	}
	zero := time.Duration(0)
	opts = f.p.opts
	opts.RefreshTTL = &zero
	p, err = NewOAuth(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	f.p = p
	if !f.connect(t).ExpiresAt.IsZero() {
		t.Fatal("TTL zero must mean unknown")
	}
}

func TestEnvironmentOverridesAndConfigurationFailures(t *testing.T) {
	f := setup(t, nil)
	t.Setenv("EXCALIBUR_GOOGLE_OAUTH_CLIENT", f.p.opts.ClientPath)
	t.Setenv("EXCALIBUR_GOOGLE_REDIRECT_URI", "https://web.example.test/oauth/google/callback")
	t.Setenv("EXCALIBUR_GOOGLE_REFRESH_TTL", "0")
	opts := f.p.opts
	opts.ClientPath = ""
	opts.RedirectURI = ""
	p, err := NewOAuth(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := p.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(auth)
	if u.Query().Get("redirect_uri") != "https://web.example.test/oauth/google/callback" {
		t.Fatal("redirect override ignored")
	}
	f.p = p
	if !f.connect(t).ExpiresAt.IsZero() {
		t.Fatal("environment TTL zero must mean unknown")
	}
	t.Setenv("EXCALIBUR_GOOGLE_REFRESH_TTL", "bad-duration")
	if _, err := NewOAuth(context.Background(), opts); err == nil {
		t.Fatal("invalid TTL accepted")
	}
	t.Setenv("EXCALIBUR_GOOGLE_REFRESH_TTL", "-1h")
	if _, err := NewOAuth(context.Background(), opts); err == nil {
		t.Fatal("negative TTL accepted")
	}
	t.Setenv("EXCALIBUR_GOOGLE_REFRESH_TTL", "168h")
	opts.ClientPath = filepath.Join(t.TempDir(), "malformed-client.json")
	if err := os.WriteFile(opts.ClientPath, []byte(`{"web":{"client_secret":"fake-private-detail"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOAuth(context.Background(), opts); err == nil || strings.Contains(err.Error(), "fake-private-detail") {
		t.Fatal("invalid client must fail without exposing its contents")
	}
}

func TestRefreshPreservesIssueDateAndOfflineGrant(t *testing.T) {
	f := setup(t, nil)
	s := f.connect(t)
	issued := f.p.token.RefreshIssued
	f.now = f.now.Add(time.Hour)
	// Google's refresh response may omit the existing refresh token and scope.
	f.p.apply(tokenResponse{AccessToken: "fake-next-access", ExpiresIn: 3600})
	if f.p.token.RefreshToken != "fake-refresh" || !f.p.token.RefreshIssued.Equal(issued) || !f.p.token.RefreshExpiry.Equal(s.ExpiresAt) || len(f.p.token.Scopes) != len(requiredScopes) {
		t.Fatal("refresh must preserve offline grant and original lifetime")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.p.AccessToken(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled access must stop")
	}
}
