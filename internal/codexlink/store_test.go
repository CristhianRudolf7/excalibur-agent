package codexlink

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func jwt(c map[string]any) string {
	b, _ := json.Marshal(c)
	return "header." + base64.RawURLEncoding.EncodeToString(b) + ".signature"
}
func isolatedStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("EXCALIBUR_CODEX_AUTH_FILE", "")
	t.Setenv("HOME", t.TempDir())
	return NewStore(t.TempDir())
}
func testTokens() Tokens {
	return Tokens{AccessToken: jwt(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}), RefreshToken: "test-refresh", AccountID: "test-account"}
}
func TestStoreResolutionStatusSaveDelete(t *testing.T) {
	s := isolatedStore(t)
	if s.Status().Connected {
		t.Fatal("unexpected sign-in")
	}
	tok := testTokens()
	tok.IDToken = jwt(map[string]any{"email": "tester@example.invalid", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "test-plan"}})
	cliPath := filepath.Join(os.Getenv("HOME"), ".codex", "auth.json")
	if err := writeCredentials(credentialFile{path: cliPath, raw: map[string]json.RawMessage{}, tokens: tok}); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(); !got.Connected || got.Source != "codex-cli" || got.AccountEmail != "tester@example.invalid" || got.PlanType != "test-plan" || got.ExpiresAt.IsZero() {
		t.Fatalf("bad public metadata: %+v", got)
	}
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
	if s.Status().Source != "excalibur" {
		t.Fatal("own file should win")
	}
	info, err := os.Stat(s.ownPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file permissions")
	}
	envPath := filepath.Join(t.TempDir(), "auth.json")
	if err := writeCredentials(credentialFile{path: envPath, raw: map[string]json.RawMessage{}, tokens: tok}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_CODEX_AUTH_FILE", envPath)
	if s.Status().Source != "env" {
		t.Fatal("environment should win")
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cliPath); err != nil {
		t.Fatal("deleted external sign-in")
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Fatal("deleted environment sign-in")
	}
	if err := os.WriteFile(envPath, []byte(`{"tokens":"test-secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if s.Status().Connected {
		t.Fatal("malformed override must not fall back")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, tok), tok.AccessToken) {
			t.Fatal("credential formatting exposed token")
		}
	}
}
func TestRefreshSingleFlightPreservesFile(t *testing.T) {
	s := isolatedStore(t)
	old := testTokens()
	old.AccessToken = jwt(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})
	raw := map[string]json.RawMessage{"extra": json.RawMessage(`{"keep":true}`), "tokens": json.RawMessage(`{"custom":"preserved"}`), "auth_mode": json.RawMessage(`"chatgpt"`)}
	if err := writeCredentials(credentialFile{path: s.ownPath(), raw: raw, tokens: old}); err != nil {
		t.Fatal(err)
	}
	next := testTokens()
	next.RefreshToken = "test-rotated"
	next.IDToken = "test-id"
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req map[string]string
		if json.NewDecoder(r.Body).Decode(&req) != nil || req["client_id"] != clientID || req["grant_type"] != "refresh_token" || req["refresh_token"] != old.RefreshToken || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid refresh request")
		}
		_ = json.NewEncoder(w).Encode(next)
	}))
	defer upstream.Close()
	s.tokenURL = upstream.URL
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		safego.Go("refresh-test", nil, func() {
			defer wg.Done()
			got, err := s.credentials(context.Background(), "")
			if err != nil || got.AccessToken != next.AccessToken {
				t.Error("refresh failed")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh calls: %d", calls.Load())
	}
	f, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if string(f.raw["extra"]) != `{"keep":true}` && !strings.Contains(string(f.raw["extra"]), "keep") {
		t.Fatal("lost fields")
	}
	if !strings.Contains(string(f.raw["tokens"]), "preserved") || f.tokens.RefreshToken != next.RefreshToken {
		t.Fatal("lost token fields")
	}
	if _, err := time.Parse(time.RFC3339, strings.Trim(string(f.raw["last_refresh"]), `"`)); err != nil {
		t.Fatal("invalid refresh timestamp")
	}
	info, _ := os.Stat(s.ownPath())
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions changed")
	}
}
func TestRefreshFailureRedaction(t *testing.T) {
	s := isolatedStore(t)
	tok := testTokens()
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = fmt.Fprint(w, tok.RefreshToken+tok.AccessToken)
	}))
	defer upstream.Close()
	s.tokenURL = upstream.URL
	_, err := s.credentials(context.Background(), tok.AccessToken)
	if err == nil || strings.Contains(err.Error(), tok.AccessToken) || strings.Contains(err.Error(), tok.RefreshToken) {
		t.Fatal("unsafe refresh error")
	}
	got, e := s.load()
	if e != nil || got.tokens.AccessToken != tok.AccessToken {
		t.Fatal("failed refresh overwrote file")
	}
}

func TestRefreshDoesNotOverwriteConcurrentSignIn(t *testing.T) {
	s := isolatedStore(t)
	old := testTokens()
	old.AccessToken = "test-old-access"
	if err := s.Save(old); err != nil {
		t.Fatal(err)
	}
	replacement := testTokens()
	replacement.AccessToken = "test-replacement"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A separate store represents a login route or another process replacing auth.
		if err := NewStore(s.dataDir).Save(replacement); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(Tokens{AccessToken: "test-refreshed-old-account"})
	}))
	defer upstream.Close()
	s.tokenURL = upstream.URL
	got, err := s.credentials(context.Background(), old.AccessToken)
	if err != nil || got.AccessToken != replacement.AccessToken {
		t.Fatal("refresh replaced a newer sign-in")
	}
}
func TestRefreshFailureSharedAcrossConcurrentCallers(t *testing.T) {
	s := isolatedStore(t)
	old := testTokens()
	old.AccessToken = "test-rejected"
	if err := s.Save(old); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(401) }))
	defer upstream.Close()
	s.tokenURL = upstream.URL
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		safego.Go("refresh-failure-test", nil, func() {
			defer wg.Done()
			if _, err := s.credentials(context.Background(), old.AccessToken); err == nil {
				t.Error("expected refresh failure")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("repeated concurrent failed refresh: %d", calls.Load())
	}
}
