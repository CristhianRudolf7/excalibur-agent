// Package codexlink connects local Responses callers to an account sign-in.
package codexlink

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const tokenEndpoint = "https://auth.openai.com/oauth/token"
const clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

// Tokens uses the sign-in file's wire field names.
type Tokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

// String and GoString prevent accidental credential formatting.
func (Tokens) String() string   { return "[redacted credentials]" }
func (Tokens) GoString() string { return "[redacted credentials]" }

// Status contains public metadata only. Claims are display hints, not verification.
type Status struct {
	Connected    bool      `json:"connected"`
	Source       string    `json:"source"`
	AccountEmail string    `json:"account_email"`
	PlanType     string    `json:"plan_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Store serializes reads, writes and refreshes for the daemon's shared sign-in.
type Store struct {
	mu          sync.Mutex
	dataDir     string
	client      *http.Client
	tokenURL    string
	failedToken string
	failedAt    time.Time
}

func NewStore(dataDir string) *Store {
	return &Store{dataDir: dataDir, client: accountClient(nil), tokenURL: tokenEndpoint}
}

func accountClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &copy
}

func (s *Store) ownPath() string { return filepath.Join(s.dataDir, "codexlink", "auth.json") }

type credentialFile struct {
	path, source string
	raw          map[string]json.RawMessage
	tokens       Tokens
}

func (s *Store) load() (credentialFile, error) {
	paths := []struct{ path, source string }{}
	if p := os.Getenv("EXCALIBUR_CODEX_AUTH_FILE"); p != "" {
		paths = append(paths, struct{ path, source string }{p, "env"})
	}
	paths = append(paths, struct{ path, source string }{s.ownPath(), "excalibur"})
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, struct{ path, source string }{filepath.Join(home, ".codex", "auth.json"), "codex-cli"})
	}
	for _, p := range paths {
		b, err := os.ReadFile(p.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return credentialFile{}, errors.New("sign-in file cannot be read")
		}
		f := credentialFile{path: p.path, source: p.source}
		if json.Unmarshal(b, &f.raw) != nil || json.Unmarshal(f.raw["tokens"], &f.tokens) != nil {
			return credentialFile{}, errors.New("sign-in file is invalid")
		}
		if f.tokens.AccountID == "" {
			f.tokens.AccountID = claimString(f.tokens.AccessToken, "https://api.openai.com/auth", "chatgpt_account_id")
		}
		if f.tokens.AccessToken == "" || f.tokens.AccountID == "" {
			return credentialFile{}, errors.New("account sign-in is required")
		}
		return f, nil
	}
	return credentialFile{}, errors.New("account sign-in is required")
}

func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return Status{}
	}
	email := claimString(f.tokens.IDToken, "email")
	if email == "" {
		email = claimString(f.tokens.AccessToken, "https://api.openai.com/profile", "email")
	}
	plan := claimString(f.tokens.IDToken, "https://api.openai.com/auth", "chatgpt_plan_type")
	if plan == "" {
		plan = claimString(f.tokens.AccessToken, "https://api.openai.com/auth", "chatgpt_plan_type")
	}
	return Status{Connected: true, Source: f.source, AccountEmail: email, PlanType: plan, ExpiresAt: expires(f.tokens.AccessToken)}
}

func (s *Store) Save(t Tokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.AccessToken == "" || t.AccountID == "" {
		return errors.New("account sign-in is incomplete")
	}
	raw := map[string]json.RawMessage{"auth_mode": json.RawMessage(`"chatgpt"`), "OPENAI_API_KEY": json.RawMessage(`null`)}
	return writeCredentials(credentialFile{path: s.ownPath(), raw: raw, tokens: t})
}

func (s *Store) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.ownPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("sign-in file cannot be removed")
	}
	return nil
}

func writeCredentials(f credentialFile) error {
	// Preserve unknown top-level and token fields when refreshing an existing file.
	tokenFields := map[string]json.RawMessage{}
	_ = json.Unmarshal(f.raw["tokens"], &tokenFields)
	encoded, _ := json.Marshal(f.tokens)
	var updated map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &updated)
	for k, v := range updated {
		tokenFields[k] = v
	}
	f.raw["tokens"], _ = json.Marshal(tokenFields)
	f.raw["last_refresh"], _ = json.Marshal(time.Now().UTC().Format(time.RFC3339))
	b, err := json.MarshalIndent(f.raw, "", "  ")
	if err != nil {
		return errors.New("sign-in file cannot be encoded")
	}
	if os.MkdirAll(filepath.Dir(f.path), 0700) != nil {
		return errors.New("sign-in directory cannot be created")
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".auth-*")
	if err != nil {
		return errors.New("sign-in file cannot be saved")
	}
	defer os.Remove(tmp.Name())
	ok := tmp.Chmod(0600) == nil
	if ok {
		_, err = tmp.Write(append(b, '\n'))
		ok = err == nil
	}
	if ok {
		ok = tmp.Sync() == nil
	}
	if err = tmp.Close(); err != nil {
		ok = false
	}
	if !ok || os.Rename(tmp.Name(), f.path) != nil {
		return errors.New("sign-in file cannot be saved")
	}
	return nil
}

func claims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var c map[string]any
	_ = json.Unmarshal(b, &c)
	return c
}
func claimString(token string, path ...string) string {
	var value any = claims(token)
	for _, p := range path {
		m, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = m[p]
	}
	str, _ := value.(string)
	return str
}
func expires(token string) time.Time {
	exp, _ := claims(token)["exp"].(float64)
	if exp == 0 {
		return time.Time{}
	}
	return time.Unix(int64(exp), 0)
}

// credentials refreshes expired tokens, or the exact token rejected by upstream.
// Reloading under the lock makes concurrent successful refreshes single-flight.
func (s *Store) credentials(ctx context.Context, rejected string) (Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Tokens{}, err
	}
	f, err := s.load()
	if err != nil {
		return Tokens{}, err
	}
	expiry := expires(f.tokens.AccessToken)
	refresh := !expiry.IsZero() && time.Until(expiry) < 30*time.Second
	if rejected != "" {
		refresh = f.tokens.AccessToken == rejected
	}
	if !refresh {
		return f.tokens, nil
	}
	if s.failedToken == f.tokens.AccessToken && time.Since(s.failedAt) < 2*time.Second {
		return Tokens{}, errors.New("sign-in refresh failed; sign in again")
	}
	if f.tokens.RefreshToken == "" {
		return Tokens{}, errors.New("sign-in expired; sign in again")
	}
	body, _ := json.Marshal(map[string]string{"client_id": clientID, "grant_type": "refresh_token", "refresh_token": f.tokens.RefreshToken})
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(refreshCtx, http.MethodPost, s.tokenURL, bytes.NewReader(body))
	if err != nil {
		return Tokens{}, errors.New("sign-in refresh unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	s.failedToken = f.tokens.AccessToken
	s.failedAt = time.Now()
	defer func() {
		if s.failedToken != "" {
			s.failedAt = time.Now()
		}
	}()
	resp, err := s.client.Do(req)
	if err != nil {
		return Tokens{}, errors.New("sign-in refresh unavailable")
	}
	defer resp.Body.Close()
	var next Tokens
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&next) != nil || next.AccessToken == "" {
		s.failedToken = f.tokens.AccessToken
		s.failedAt = time.Now()
		return Tokens{}, errors.New("sign-in refresh failed; sign in again")
	}
	// A login or external refresh may have replaced the file during the request.
	current, err := s.load()
	if err != nil {
		return Tokens{}, err
	}
	if current.path != f.path || current.tokens.AccessToken != f.tokens.AccessToken {
		return current.tokens, nil
	}
	f.tokens.AccessToken = next.AccessToken
	if next.RefreshToken != "" {
		f.tokens.RefreshToken = next.RefreshToken
	}
	if next.IDToken != "" {
		f.tokens.IDToken = next.IDToken
	}
	if err := writeCredentials(f); err != nil {
		return Tokens{}, err
	}
	s.failedToken = ""
	return f.tokens, nil
}

func (s *Store) String() string   { return "[credential store]" }
func (s *Store) GoString() string { return fmt.Sprint(s) }
