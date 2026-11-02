package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestDurableBraveNeverResolvesOrRequests(t *testing.T) {
	b, err := New(Config{Durable: true, Order: []string{"brave"}, Providers: []ProviderConfig{{Name: "brave", KeyResolver: func(context.Context) (string, error) { t.Fatal("durable Brave resolved a key"); return "", nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Search(context.Background(), "query", tools.SearchOptions{})
	if err == nil || !strings.Contains(err.Error(), "transient use only") {
		t.Fatalf("missing retention refusal: %v", err)
	}
	if _, err = b.SourceSearch(context.Background(), "youtube", "query", 1); err == nil {
		t.Fatal("source search bypassed retention policy")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = b.Search(ctx, "query", tools.SearchOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDurableBraveStorageRightsOptIn(t *testing.T) {
	keyCalls := 0
	var requests atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.Header.Get("X-Subscription-Token") != testKey {
			t.Error("missing Brave request authentication")
		}
		fmt.Fprint(w, `{"web":{"results":[{"title":"Stored with rights","url":"https://example.com","description":"Fixture result"}]}}`)
	})
	b, err := New(Config{Durable: true, Order: []string{"brave"}, HTTPClient: client, Providers: []ProviderConfig{{Name: "brave", StorageRights: true, KeyResolver: func(context.Context) (string, error) { keyCalls++; return testKey, nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	results, provider, err := b.SearchWithProvider(context.Background(), "query", tools.SearchOptions{K: 1})
	if err != nil || provider != "brave" || len(results) != 1 || results[0].Title != "Stored with rights" || keyCalls != 1 || requests.Load() != 1 {
		t.Fatalf("Brave opt-in did not execute: %v", err)
	}
}

func TestDurableBraveRefusalFallsThrough(t *testing.T) {
	var requests atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Error("refusal did not fall through to Tavily")
		}
		fmt.Fprint(w, `{"results":[{"title":"Fallback","url":"https://example.com","content":"Fixture result"}]}`)
	})
	b, err := New(Config{Durable: true, Order: []string{"brave", "tavily"}, HTTPClient: client, Providers: []ProviderConfig{
		{Name: "brave", KeyResolver: func(context.Context) (string, error) { t.Fatal("refused Brave resolved a key"); return "", nil }},
		{Name: "tavily", KeyResolver: key},
	}})
	if err != nil {
		t.Fatal(err)
	}
	results, provider, err := b.SearchWithProvider(context.Background(), "query", tools.SearchOptions{})
	if err != nil || provider != "tavily" || len(results) != 1 || requests.Load() != 1 {
		t.Fatalf("fallback failed: %v", err)
	}
}
