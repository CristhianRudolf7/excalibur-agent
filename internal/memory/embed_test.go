package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFakeEmbedderDeterministic pins the test double: same text,
// same vector; shared words score higher than disjoint ones.
func TestFakeEmbedderDeterministic(t *testing.T) {
	f := NewFakeEmbedder("fake", 32)
	a, err := f.Embed(context.Background(), []string{"the quick brown fox"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Embed(context.Background(), []string{"the quick brown fox"})
	if err != nil {
		t.Fatal(err)
	}
	if Cosine(a[0], b[0]) != 1 {
		t.Fatal("same text must score exactly 1")
	}
	c, err := f.Embed(context.Background(), []string{"quantum entanglement paradox"})
	if err != nil {
		t.Fatal(err)
	}
	if Cosine(a[0], c[0]) >= Cosine(a[0], b[0]) {
		t.Fatal("disjoint text should score lower")
	}
	if f.Calls() != 3 {
		t.Fatalf("calls = %d", f.Calls())
	}
}

// TestHTTPEmbedderAgainstSimulatedServer covers the provider path
// without paid calls: success, auth failure without retry, and the
// fallback key on rate limits (Section 10.8, task live-check rule
// for the model credentials file).
func TestHTTPEmbedderAgainstSimulatedServer(t *testing.T) {
	var bodies []string
	var auths []string
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		auths = append(auths, r.Header.Get("Authorization"))
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, req.Model)
		if strings.HasSuffix(r.Header.Get("Authorization"), "bad") {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"quota"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}],"model":"m"}`))
	}))
	defer srv.Close()

	emb, err := NewHTTPEmbedder(HTTPOptions{BaseURL: srv.URL, APIKey: "bad", FallbackKey: "good", Model: "text-embedding-3-small"})
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := emb.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 {
		t.Fatalf("vectors = %+v", vecs)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, want 1 primary + 1 fallback", hits)
	}
	if auths[1] != "Bearer good" {
		t.Fatalf("fallback key not used: %q", auths[1])
	}

	// Auth failures never retry and never touch the fallback.
	hits = 0
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()
	emb2, _ := NewHTTPEmbedder(HTTPOptions{BaseURL: bad.URL, APIKey: "nope", FallbackKey: "good", Model: "m"})
	if _, err := emb2.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected auth error")
	}
	if hits != 1 {
		t.Fatalf("auth failure retried: hits = %d", hits)
	}

	// Missing key is a constructor error, never a call.
	if _, err := NewHTTPEmbedder(HTTPOptions{BaseURL: srv.URL}); err == nil {
		t.Fatal("expected missing-key error")
	}
}

// TestPendingQueueDrains covers the Section 10.8 degradation: rows
// store without vectors while the provider is down, then DrainPending
// backfills them when it returns.
func TestPendingQueueDrains(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")
	svc.SetEmbedder(NewErrorEmbedder("down", nil))

	outcome, err := svc.Admit(context.Background(), Candidate{Scope: "general", Kind: KindFact, Statement: "Queued while provider down"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Action != ActionAdmitted {
		t.Fatalf("action = %q", outcome.Action)
	}
	ret, err := svc.Retrieve(context.Background(), Query{Text: "queued provider down", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if ret.Semantic {
		t.Fatal("semantic must report unavailable with the provider down")
	}
	if len(ret.Items) == 0 {
		t.Fatal("lexical list must still answer during degradation")
	}
	svc.SetEmbedder(NewFakeEmbedder("fake", 512))
	n, err := svc.DrainPending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("drained = %d, want >= 1", n)
	}
	ret, err = svc.Retrieve(context.Background(), Query{Text: "queued provider down", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if !ret.Semantic {
		t.Fatal("semantic should work after the drain")
	}
}
