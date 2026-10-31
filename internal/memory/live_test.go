//go:build live

package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// liveCredentials mirrors the operator credential file. Keys never
// leave this test: nothing is logged and failures redact values.
type liveCredentials struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	FallbackAPIKey string `json:"fallback_api_key"`
	EmbeddingModel string `json:"embedding_model"`
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
	if creds.EmbeddingModel == "" {
		creds.EmbeddingModel = DefaultEmbeddingModel
	}
	return creds
}

// TestLiveEmbeddings is a tiny opt-in check against the real
// provider: one short embeddings call. It only runs with -tags live
// and EXCALIBUR_LIVE=1, and never under plain go test.
func TestLiveEmbeddings(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to run live checks")
	}
	creds := loadLiveCredentials(t)
	emb, err := NewHTTPEmbedder(HTTPOptions{
		BaseURL: creds.BaseURL, APIKey: creds.APIKey,
		FallbackKey: creds.FallbackAPIKey, Model: creds.EmbeddingModel,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}
	defer emb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	vecs, err := emb.Embed(ctx, []string{"the cat sits on the mat", "a feline rests on a rug"})
	if err != nil {
		t.Fatalf("live embeddings failed: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) == 0 {
		t.Fatal("empty live embedding")
	}
	// Near-paraphrases must score positively; this asserts the wire
	// format, not the model.
	if s := Cosine(vecs[0], vecs[1]); s <= 0 {
		t.Fatalf("paraphrase cosine = %v, want positive", s)
	}
	t.Logf("live embedding dims=%d cosine=%.4f", len(vecs[0]), Cosine(vecs[0], vecs[1]))
}
