//go:build live

package ctxcompiler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

// Tiny live check: one minimal chat call verifies the compiled
// request shape is accepted and compares the calibrated estimate
// against real usage. It never runs under plain go test: it needs
// the live tag, EXCALIBUR_LIVE=1 and the credentials file. The keys
// are only ever sent in the Authorization header, never logged.
func TestLiveEstimateVsUsage(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to run live checks")
	}
	creds, err := loadLiveCreds()
	if err != nil {
		t.Skipf("no credentials: %v", err)
	}
	cfg := ctxcompiler.DefaultConfig()
	prompt := "Reply with exactly: ok"
	c, err := ctxcompiler.Compile(cfg, liveSnapshot(prompt), ctxcompiler.NewEstimator(4.0))
	if err != nil {
		t.Fatal(err)
	}
	tools := ctxcompiler.ProviderTools(liveSnapshot(prompt).CoreTools)
	req := c.ToChatRequest(creds.model, tools, 8, "low", "auto")
	body, err := json.Marshal(map[string]any{
		"model":            req.Model,
		"messages":         req.Messages,
		"tools":            req.Tools,
		"max_tokens":       8,
		"reasoning_effort": "low",
		"prompt_cache_key": "ctxcompiler-live",
		"store":            false,
		"tool_choice":      "auto",
		"metadata":         map[string]string{"suite": "ctxcompiler-live"},
	})
	if err != nil {
		t.Fatal(err)
	}
	usage, status, err := postLive(creds, body, false)
	if err != nil {
		t.Fatal(err)
	}
	if status == 429 || status >= 500 {
		usage, status, err = postLive(creds, body, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	if status != 200 {
		t.Fatalf("live call status = %d", status)
	}
	est := ctxcompiler.NewEstimator(4.0)
	promptChars := 0
	for _, m := range req.Messages {
		promptChars += len(m.Content)
	}
	got := est.Estimate(repeatLen(promptChars))
	t.Logf("prompt chars=%d estimate=%d actual=%d", promptChars, got, usage.prompt)
	if usage.prompt <= 0 {
		t.Fatal("live usage must report prompt tokens")
	}
	diff := got - usage.prompt
	if diff < 0 {
		diff = -diff
	}
	if float64(diff) > 0.5*float64(usage.prompt) {
		t.Fatalf("uncalibrated estimate %d vs usage %d exceeds 2x smoke bound", got, usage.prompt)
	}
}

type liveCreds struct {
	baseURL  string
	apiKey   string
	fallback string
	model    string
}

func loadLiveCreds() (liveCreds, error) {
	path := os.Getenv("HOME") + "/.config/excalibur/credentials/openai.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		return liveCreds{}, err
	}
	var v struct {
		BaseURL        string `json:"base_url"`
		APIKey         string `json:"api_key"`
		FallbackKey    string `json:"fallback_api_key"`
		Model          string `json:"model"`
		EmbeddingModel string `json:"embedding_model"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return liveCreds{}, err
	}
	if v.BaseURL == "" || v.APIKey == "" || v.Model == "" {
		return liveCreds{}, fmt.Errorf("incomplete credentials")
	}
	return liveCreds{baseURL: v.BaseURL, apiKey: v.APIKey, fallback: v.FallbackKey, model: v.Model}, nil
}

type liveUsage struct {
	prompt int
}

func postLive(creds liveCreds, body []byte, fallback bool) (liveUsage, int, error) {
	key := creds.apiKey
	if fallback {
		if creds.fallback == "" {
			return liveUsage{}, 0, fmt.Errorf("no fallback key for retry")
		}
		key = creds.fallback
	}
	httpReq, err := http.NewRequest("POST", creds.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return liveUsage{}, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return liveUsage{}, 0, err
	}
	defer resp.Body.Close()
	var v struct {
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return liveUsage{}, resp.StatusCode, err
	}
	return liveUsage{prompt: v.Usage.PromptTokens}, resp.StatusCode, nil
}

func liveSnapshot(prompt string) ctxcompiler.Snapshot {
	return ctxcompiler.Snapshot{
		HarnessVersion: "h6-live",
		Role:           "child",
		CoreTools:      []ctxcompiler.CoreTool{{Name: "noop", Description: "no-op probe"}},
		AgentProfile:   "live-probe",
		ProjectContext: "live probe\n",
		Task:           ctxcompiler.TaskView{OriginalRequest: prompt},
	}
}

func repeatLen(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
