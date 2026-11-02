//go:build live

package websearch

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestLiveSearchReadWikipedia(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to enable live checks")
	}
	path := os.Getenv("EXCALIBUR_TAVILY_HEADER_FILE")
	if path == "" {
		t.Skip("EXCALIBUR_TAVILY_HEADER_FILE is required")
	}
	resolver := func(context.Context) (string, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", errors.New("header file unavailable")
		}
		line := strings.TrimSpace(string(raw))
		prefix := "Authorization: Bearer "
		if !strings.HasPrefix(line, prefix) {
			return "", errors.New("invalid header file")
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if value == "" || strings.ContainsAny(value, "\r\n") {
			return "", errors.New("invalid credential")
		}
		return value, nil
	}
	b, err := New(Config{Providers: []ProviderConfig{{Name: "tavily", KeyResolver: resolver}}, Order: []string{"tavily"}})
	if err != nil {
		t.Fatal("configuration failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	results, err := b.Search(ctx, "Go programming language official documentation", tools.SearchOptions{K: 1})
	if err != nil || len(results) == 0 {
		t.Fatal("live Tavily search failed")
	}
	t.Log("Tavily search passed")
	p, err := b.Read(ctx, "https://example.com")
	if err != nil || p.Markdown == "" {
		t.Fatal("live example.com read failed")
	}
	t.Log("example.com read passed")
	results, err = b.SourceSearch(ctx, "wikipedia", "Go programming language", 1)
	if err != nil || len(results) == 0 {
		t.Fatal("live Wikipedia search failed")
	}
	t.Log("Wikipedia search passed")
}
