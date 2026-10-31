//go:build live

// Tiny live check for the decisions path (Section 19.4): one short
// bounded question through the LLM fallback against the real
// decisions model, plus the contract rule that embedding operations
// never carry reasoning_effort. It only runs with -tags live and
// EXCALIBUR_LIVE=1, and never under plain go test. Keys are only
// sent in the Authorization header, never logged.
package models

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
)

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
	if creds.APIKey == "" || creds.BaseURL == "" {
		t.Skip("incomplete credentials file")
	}
	if creds.DecisionsModel == "" {
		creds.DecisionsModel = DefaultDecisionsModel
	}
	return creds
}

// quotaLike reports whether err looks like a quota/rate-limit
// failure worth one bounded retry with the fallback key.
func quotaLike(err error) bool {
	if err == nil {
		return false
	}
	if perr, ok := err.(*providers.ProviderError); ok {
		return perr.Kind == providers.KindRateLimited || perr.Kind == providers.KindTransient
	}
	return false
}

func TestLiveDecisionsFallback(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to run live checks")
	}
	creds := loadLiveCredentials(t)
	effort := creds.ReasoningEffort
	if effort == "" {
		effort = DefaultDecisionsEffort
	}
	a, err := openai.New(openai.Options{APIKey: creds.APIKey, BaseURL: creds.BaseURL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// One bounded noul question through the real decisions model.
	req := providers.ChatRequest{
		Model:           creds.DecisionsModel,
		Messages:        []providers.Message{{Role: providers.RoleUser, Content: "Reply with exactly: {\"probability\": 1.0}"}},
		MaxTokens:       16,
		ReasoningEffort: effort,
	}
	resp, err := a.Chat(ctx, req)
	if quotaLike(err) && creds.FallbackAPIKey != "" {
		b, berr := openai.New(openai.Options{APIKey: creds.FallbackAPIKey, BaseURL: creds.BaseURL})
		if berr != nil {
			t.Fatalf("fallback New: %v", berr)
		}
		defer b.Close()
		resp, err = b.Chat(ctx, req)
	}
	if err != nil {
		t.Fatalf("live decisions call failed: %v", err)
	}
	if resp.Message.Content == "" {
		t.Fatal("empty live reply")
	}
	t.Logf("live decisions reply bytes=%d usage=%+v", len(resp.Message.Content), resp.Usage)

	// Contract rule without network: embedding operations never
	// carry reasoning_effort, chat decisions keep it.
	cfg := DecisionsConfig{Model: creds.DecisionsModel, ReasoningEffort: effort, EmbeddingModel: creds.EmbeddingModel}
	emb := cfg.EmbeddingRequest("live probe")
	if _, ok := emb["reasoning_effort"]; ok {
		t.Fatal("embedding request must not carry reasoning_effort")
	}
	chat := cfg.ChatRequest(cfg.ModelName(), []providers.Message{{Role: providers.RoleUser, Content: "hi"}})
	if chat.ReasoningEffort == "" {
		t.Fatal("chat decisions should carry the configured effort")
	}
}
