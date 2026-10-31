//go:build live

package openai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// liveCredentials mirrors the operator credential file. Keys never
// leave this test: nothing is logged and failures redact values.
type liveCredentials struct {
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	FallbackAPIKey  string `json:"fallback_api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	EmbeddingModel  string `json:"embedding_model"`
	DecisionsModel  string `json:"decisions_model"`
}

func loadLiveCredentials(t *testing.T) liveCredentials {
	t.Helper()
	path := os.Getenv("EXCALIBUR_OPENAI_CREDS")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no home dir: %v", err)
		}
		path = filepath.Join(home, ".config", "excalibur", "credentials", "openai.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no credentials file: %v", err)
	}
	var creds liveCredentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}
	if creds.APIKey == "" || creds.Model == "" {
		t.Skip("incomplete credentials file")
	}
	return creds
}

// TestLiveChat is a tiny opt-in check against the real OpenAI API: one
// short chat call with low reasoning effort. It only runs with
// -tags live and EXCALIBUR_LIVE=1, and never under plain go test.
func TestLiveChat(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to run live checks")
	}
	creds := loadLiveCredentials(t)
	a, err := New(Options{APIKey: creds.APIKey, BaseURL: creds.BaseURL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := a.Chat(ctx, providers.ChatRequest{
		Model:           creds.Model,
		Messages:        []providers.Message{{Role: providers.RoleUser, Content: "Reply with exactly: ok"}},
		MaxTokens:       16,
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("live chat failed: %v", err)
	}
	if resp.Message.Content == "" {
		t.Fatalf("empty live reply")
	}
	if resp.Usage.Input <= 0 {
		t.Fatalf("missing live usage: %+v", resp.Usage)
	}
	t.Logf("live reply bytes=%d usage=%+v", len(resp.Message.Content), resp.Usage)
}
