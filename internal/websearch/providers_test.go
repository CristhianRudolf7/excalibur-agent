package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

const testKey = "fixture-credential-do-not-expose"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Local fixture transport retains the original API host, path and query for assertions.
func fixtureClient(t *testing.T, h http.HandlerFunc) *http.Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		r := req.Clone(req.Context())
		u := *req.URL
		u.Scheme = "http"
		u.Host = strings.TrimPrefix(s.URL, "http://")
		r.URL = &u
		return s.Client().Transport.RoundTrip(r)
	})}
}
func key(context.Context) (string, error) { return testKey, nil }
func newTestBackend(t *testing.T, client *http.Client) *Backend {
	t.Helper()
	b, err := New(Config{Providers: []ProviderConfig{{Name: "tavily", KeyResolver: key}, {Name: "brave", KeyResolver: key}}, Order: []string{"tavily", "brave"}, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestProviderMapping(t *testing.T) {
	for _, name := range []string{"tavily", "brave"} {
		t.Run(name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") == "" {
					t.Error("missing polite headers")
				}
				if name == "tavily" {
					if r.Method != "POST" || r.URL.Path != "/search" || r.Header.Get("Authorization") != "Bearer "+testKey {
						t.Error("Tavily request")
					}
					var m map[string]any
					if json.NewDecoder(r.Body).Decode(&m) != nil {
						t.Fatal("invalid request body")
					}
					if m["query"] != "golang" || m["max_results"] != float64(3) || m["search_depth"] != "basic" || m["topic"] != "general" || m["time_range"] != "week" || m["language"] != "en" {
						t.Errorf("mapping: %#v", m)
					}
					if len(m["include_domains"].([]any)) != 1 {
						t.Error("domains")
					}
					fmt.Fprint(w, `{"results":[{"title":"Go","url":"https://go.dev","content":"Docs","published_date":"2026-01-01"},{"url":"https://tbench.ai/tasks"}]}`)
				} else {
					v := r.URL.Query()
					if r.Method != "GET" || r.Header.Get("X-Subscription-Token") != testKey || v.Get("q") != "golang (site:go.dev)" || v.Get("count") != "3" || v.Get("freshness") != "pw" || v.Get("search_lang") != "en" || v.Get("result_filter") != "web" {
						t.Error("Brave request mapping")
					}
					fmt.Fprint(w, `{"web":{"results":[{"title":"Go","url":"https://go.dev","description":"Docs","page_age":"2026-01-01","age":"yesterday"},{"url":"https://tbench.ai"}]}}`)
				}
			})
			p := &apiProvider{name: name, key: key, client: client, limits: defaults(Limits{}), userAgent: "Excalibur contact@example.org"}
			r, err := p.Search(context.Background(), "golang", tools.SearchOptions{K: 3, Freshness: "week", Domains: []string{"go.dev"}, Lang: "en"})
			if err != nil || len(r) != 1 || r[0].Title != "Go" || r[0].Snippet != "Docs" || r[0].PublishedAt != "2026-01-01" {
				t.Fatalf("mapping: %+v, %v", r, err)
			}
		})
	}
}
func TestFreshness(t *testing.T) {
	for _, name := range []string{"tavily", "brave"} {
		for _, f := range []string{"day", "week", "month", "year"} {
			t.Run(name+"/"+f, func(t *testing.T) {
				client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
					if name == "tavily" {
						var body map[string]any
						if json.NewDecoder(r.Body).Decode(&body) != nil {
							t.Error("body")
						}
						if body["time_range"] != f {
							t.Error("Tavily freshness")
						}
						fmt.Fprint(w, `{"results":[]}`)
					} else {
						if r.URL.Query().Get("freshness") != map[string]string{"day": "pd", "week": "pw", "month": "pm", "year": "py"}[f] {
							t.Error("Brave freshness")
						}
						fmt.Fprint(w, `{"web":{"results":[]}}`)
					}
				})
				p := &apiProvider{name: name, key: key, client: client, limits: defaults(Limits{})}
				if _, err := p.Search(context.Background(), "go", tools.SearchOptions{Freshness: f}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	if freshness("invalid") != "" {
		t.Fatal("invalid freshness")
	}
}
func TestProviderErrorsAndSecrets(t *testing.T) {
	for _, name := range []string{"tavily", "brave"} {
		for _, status := range []int{401, 429, 500, 200} {
			t.Run(fmt.Sprintf("%s/%d", name, status), func(t *testing.T) {
				client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Retry-After", "120")
					w.WriteHeader(status)
					fmt.Fprint(w, strings.Repeat(testKey, 50))
				})
				p := &apiProvider{name: name, key: key, client: client, limits: defaults(Limits{ResponseBytes: 50})}
				_, err := p.Search(context.Background(), "q", tools.SearchOptions{})
				if err == nil || strings.Contains(err.Error(), testKey) {
					t.Fatalf("unsafe error: %v", err)
				}
				var pe *ProviderError
				if !errors.As(err, &pe) {
					t.Fatal("untyped provider error")
				}
				if pe.Transient != (status == 429 || status == 500) {
					t.Fatal("wrong classification")
				}
				if status == 429 && pe.RetryAfter != 120*time.Second {
					t.Fatal("retry-after")
				}
			})
		}
	}
	for _, name := range []string{"tavily", "brave"} {
		t.Run(name+"/timeout", func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(100 * time.Millisecond):
				}
			})
			client.Timeout = 20 * time.Millisecond
			p := &apiProvider{name: name, key: key, client: client, limits: defaults(Limits{})}
			_, err := p.Search(context.Background(), "q", tools.SearchOptions{})
			var pe *ProviderError
			if !errors.As(err, &pe) || !pe.Transient {
				t.Fatalf("timeout: %v", err)
			}
			p.key = func(context.Context) (string, error) { return "", errors.New(testKey) }
			_, err = p.Search(context.Background(), "q", tools.SearchOptions{})
			if err == nil || strings.Contains(err.Error(), testKey) {
				t.Fatal("resolver secret leaked")
			}
		})
	}
}

type scriptedProvider struct {
	name    string
	err     error
	calls   *[]string
	results []tools.SearchResult
}

func (p scriptedProvider) Name() string { return p.name }
func (p scriptedProvider) Search(context.Context, string, tools.SearchOptions) ([]tools.SearchResult, error) {
	*p.calls = append(*p.calls, p.name)
	return p.results, p.err
}
func TestChain(t *testing.T) {
	calls := []string{}
	c := Chain{RetryAfterCap: time.Millisecond, Providers: []Provider{
		scriptedProvider{"missing", &ProviderError{MissingKey: true}, &calls, nil},
		scriptedProvider{"busy", &ProviderError{Transient: true, RetryAfter: time.Hour}, &calls, nil},
		scriptedProvider{"ok", nil, &calls, []tools.SearchResult{{URL: "https://example.org"}, {URL: "https://tbench.ai"}}},
	}}
	start := time.Now()
	r, name, err := c.SearchWithProvider(context.Background(), "q", tools.SearchOptions{})
	if err != nil || name != "ok" || len(r) != 1 || strings.Join(calls, ",") != "missing,busy,ok" || time.Since(start) > time.Second {
		t.Fatalf("fallback: %s %v %v", name, r, err)
	}
	c.Providers = c.Providers[:2]
	_, err = c.Search(context.Background(), "q", tools.SearchOptions{})
	var all *AllFailedError
	if !errors.As(err, &all) || len(all.Failures) != 2 {
		t.Fatal("all-failed error")
	}
	c.Providers = []Provider{scriptedProvider{"denied", &ProviderError{Status: 401}, &calls, nil}, c.Providers[0]}
	calls = nil
	_, err = c.Search(context.Background(), "q", tools.SearchOptions{})
	if err == nil || len(calls) != 1 {
		t.Fatal("permanent error should stop")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Search(ctx, "q", tools.SearchOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c.Providers = []Provider{scriptedProvider{"busy", &ProviderError{Transient: true, RetryAfter: time.Hour}, &calls, nil}}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	c.RetryAfterCap = time.Second
	_, err = c.Search(ctx, "q", tools.SearchOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestConfig(t *testing.T) {
	for _, c := range []Config{{}, {Order: []string{"unknown"}}, {Providers: []ProviderConfig{{Name: "other"}}, Order: []string{"other"}}, {Providers: []ProviderConfig{{Name: "brave"}, {Name: "brave"}}, Order: []string{"brave"}}, {Providers: []ProviderConfig{{Name: "brave"}}, Order: []string{"brave", "brave"}}} {
		if _, err := New(c); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	b, err := New(Config{Providers: []ProviderConfig{{Name: "tavily", KeyRef: "secret://search"}}, Order: []string{"tavily"}, ResolveKey: func(ref string) KeyResolver {
		if ref != "secret://search" {
			t.Fatal("wrong ref")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Search(context.Background(), "q", tools.SearchOptions{})
	var all *AllFailedError
	if !errors.As(err, &all) {
		t.Fatal(err)
	}
}
func TestRetryAfter(t *testing.T) {
	if retryAfter("garbage") != 0 || retryAfter("-1") != 0 || retryAfter(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)) <= 0 {
		t.Fatal("retry-after parsing")
	}
}
func TestTransportAndDecodeErrorsAreSanitized(t *testing.T) {
	for _, client := range []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New(testKey) })}, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"results":%q}`, testKey) })} {
		p := &apiProvider{name: "tavily", key: key, client: client, limits: defaults(Limits{})}
		_, err := p.Search(context.Background(), "q", tools.SearchOptions{})
		if err == nil || strings.Contains(err.Error(), testKey) {
			t.Fatal("secret in error")
		}
	}
}

func TestProviderBodyTimeoutFallback(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
			return
		}
		fmt.Fprint(w, `{"web":{"results":[{"title":"Go","url":"https://go.dev"}]}}`)
	})
	b, err := New(Config{Providers: []ProviderConfig{{Name: "tavily", KeyResolver: key}, {Name: "brave", KeyResolver: key}}, Order: []string{"tavily", "brave"}, HTTPClient: client, Limits: Limits{Timeout: 30 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	results, name, err := b.SearchWithProvider(context.Background(), "go", tools.SearchOptions{})
	if err != nil || name != "brave" || len(results) != 1 {
		t.Fatal(name, results, err)
	}
}

func TestAPIRedirectDoesNotForwardCredentials(t *testing.T) {
	calls := 0
	b := newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/leak" {
			t.Error("credential redirect was followed")
		}
		http.Redirect(w, r, "https://untrusted.example/leak", http.StatusFound)
	}))
	_, err := b.Search(context.Background(), "go", tools.SearchOptions{})
	if err == nil || calls != 1 {
		t.Fatal("redirect policy", err, calls)
	}
}

func TestSearchRetainsPrivateProviderIdentity(t *testing.T) {
	calls := []string{}
	b := &Backend{chain: &Chain{Providers: []Provider{scriptedProvider{name: "brave", calls: &calls, results: []tools.SearchResult{{URL: "https://example.org", Title: "Example", Snippet: "transient"}}}}}}
	results, err := b.Search(context.Background(), "example", tools.SearchOptions{})
	if err != nil || len(results) != 1 || results[0].Provider != "brave" {
		t.Fatalf("provider identity missing: %v", err)
	}
	raw, err := json.Marshal(results)
	if err != nil || strings.Contains(string(raw), "brave") || strings.Contains(string(raw), "Provider") {
		t.Fatal("private provider identity entered JSON")
	}
}
