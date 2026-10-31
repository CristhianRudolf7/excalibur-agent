//go:build live

package gemini

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// TestLiveChat runs one tiny real call. It only runs with
// -tags live and EXCALIBUR_LIVE=1, and is skipped without a key.
func TestLiveChat(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to run live checks")
	}
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("set GEMINI_API_KEY to run the Gemini live check")
	}
	p := New(Options{APIKey: key, Model: "gemini-2.5-flash"})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages:  []providers.Message{{Role: providers.RoleUser, Content: "Reply with the word ok."}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("live Chat: %v", err)
	}
	if resp.Message.Content == "" {
		t.Fatal("empty live response")
	}
	t.Logf("usage: %+v", resp.Usage)
}
