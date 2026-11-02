package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSourceAdapters(t *testing.T) {
	cases := []struct{ source, path, param, body, title, url, snippet, date string }{
		{"wikipedia", "/w/api.php", "srsearch", `{"query":{"search":[{"title":"Go language","pageid":123,"snippet":"A <span>language</span>","timestamp":"2026-01-01"}]}}`, "Go language", "https://en.wikipedia.org/?curid=123", "A language", "2026-01-01"},
		{"arxiv", "/api/query", "search_query", `<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>https://arxiv.org/abs/1234.5678</id><title>Go language</title><summary>A language</summary><published>2026-01-01</published></entry></feed>`, "Go language", "https://arxiv.org/abs/1234.5678", "A language", "2026-01-01"},
		{"hackernews", "/api/v1/search", "query", `{"hits":[{"title":"Go language","url":"https://go.dev","story_text":"A language","created_at":"2026-01-01"}]}`, "Go language", "https://go.dev", "A language", "2026-01-01"},
		{"github", "/search/repositories", "q", `{"items":[{"full_name":"golang/go","html_url":"https://github.com/golang/go","description":"A language","created_at":"2026-01-01"}]}`, "golang/go", "https://github.com/golang/go", "A language", "2026-01-01"},
		{"stackoverflow", "/2.3/search/advanced", "q", `{"items":[{"title":"Go &amp; interfaces","link":"https://stackoverflow.com/q/1","excerpt":"A language","creation_date":123}]}`, "Go & interfaces", "https://stackoverflow.com/q/1", "A language", "123"},
		{"openalex", "/works", "search", `{"results":[{"display_name":"Go language","doi":"https://doi.org/example","publication_date":"2026-01-01"}]}`, "Go language", "https://doi.org/example", "", "2026-01-01"},
		{"semantic-scholar", "/graph/v1/paper/search", "query", `{"data":[{"title":"Go language","url":"https://semanticscholar.org/paper/example","abstract":"A language","publicationDate":"2026-01-01"}]}`, "Go language", "https://semanticscholar.org/paper/example", "A language", "2026-01-01"},
		{"huggingface", "/api/models", "search", `[{"id":"org/go","pipeline_tag":"text-generation","lastModified":"2026-01-01"}]`, "org/go", "https://huggingface.co/org/go", "text-generation", "2026-01-01"},
		{"npm", "/-/v1/search", "text", `{"objects":[{"package":{"name":"go","description":"A language","date":"2026-01-01","links":{"npm":"https://www.npmjs.com/package/go"}}}]}`, "go", "https://www.npmjs.com/package/go", "A language", "2026-01-01"},
		{"pypi", "/pypi/go/json", "", `{"info":{"name":"go","summary":"A language","package_url":"https://pypi.org/project/go/"}}`, "go", "https://pypi.org/project/go/", "Exact package lookup (PyPI has no public search API). A language", ""},
		{"crates", "/api/v1/crates", "q", `{"crates":[{"id":"go","description":"A language","updated_at":"2026-01-01"}]}`, "go", "https://crates.io/crates/go", "A language", "2026-01-01"},
		{"dev", "/api/articles/search", "q", `[{"title":"Go language","url":"https://dev.to/author/go","description":"A language","published_at":"2026-01-01","tag_list":["go"]}]`, "Go language", "https://dev.to/author/go", "A language", "2026-01-01"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Method != "GET" {
					t.Error("wrong API path")
				}
				v := r.URL.Query()
				if tc.param != "" {
					want := "go"
					if tc.source == "arxiv" {
						want = "all:go"
					}
					if v.Get(tc.param) != want {
						t.Error("query mapping")
					}
				}
				if r.Header.Get("User-Agent") != "Excalibur contact: operator@example.org" {
					t.Error("user agent")
				}
				if tc.source == "github" && r.Header.Get("Authorization") != "Bearer "+testKey {
					t.Error("optional GitHub key")
				}
				switch tc.source {
				case "dev":
					if v.Get("per_page") != "2" || r.Header.Get("Accept") != "application/vnd.forem.api-v1+json" {
						t.Error("DEV parameters")
					}
				case "wikipedia":
					if v.Get("srlimit") != "2" || v.Get("action") != "query" || v.Get("list") != "search" {
						t.Error("wiki parameters")
					}
				case "stackoverflow":
					if v.Get("site") != "stackoverflow" || v.Get("pagesize") != "2" {
						t.Error("stack parameters")
					}
				case "npm":
					if v.Get("size") != "2" {
						t.Error("npm limit")
					}
				case "semantic-scholar":
					if v.Get("fields") == "" {
						t.Error("semantic fields")
					}
				}
				fmt.Fprint(w, tc.body)
			})
			b := newTestBackend(t, client)
			b.userAgent = "Excalibur contact: operator@example.org"
			b.githubKey = key
			r, err := b.SourceSearch(context.Background(), tc.source, "go", 2)
			if err != nil || len(r) != 1 {
				t.Fatalf("%+v %v", r, err)
			}
			hit := r[0]
			if hit.SourceID != tc.source || hit.Title != tc.title || hit.URL != tc.url || hit.Snippet != tc.snippet || hit.PublishedAt != tc.date {
				t.Fatalf("mapping: %+v", hit)
			}
		})
	}
}
func TestPubMed(t *testing.T) {
	calls := 0
	b := newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		v := r.URL.Query()
		if v.Get("db") != "pubmed" || v.Get("retmode") != "json" {
			t.Error("PubMed params")
		}
		switch r.URL.Path {
		case "/entrez/eutils/esearch.fcgi":
			if v.Get("term") != "go" || v.Get("retmax") != "1" {
				t.Error("search params")
			}
			fmt.Fprint(w, `{"esearchresult":{"idlist":["123"]}}`)
		case "/entrez/eutils/esummary.fcgi":
			if v.Get("id") != "123" {
				t.Error("summary params")
			}
			fmt.Fprint(w, `{"result":{"uids":["123"],"123":{"uid":"123","title":"Go study","pubdate":"2026 Jan"}}}`)
		default:
			t.Error("path")
		}
	}))
	r, err := b.SourceSearch(context.Background(), "pubmed", "go", 1)
	if err != nil || len(r) != 1 || r[0].URL != "https://pubmed.ncbi.nlm.nih.gov/123/" || r[0].Title != "Go study" || r[0].PublishedAt != "2026 Jan" || r[0].SourceID != "pubmed" || calls != 2 {
		t.Fatal(r, err, calls)
	}
}
func TestSourceFailuresAndPolicies(t *testing.T) {
	for _, source := range []string{"unknown", "polymarket"} {
		b := newTestBackend(t, nil)
		if _, err := b.SourceSearch(context.Background(), source, "go", 1); err == nil {
			t.Fatal(source)
		}
	}
	b := newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("blocked query reached network") }))
	if _, err := b.SourceSearch(context.Background(), "github", "terminal-bench", 1); err == nil || !strings.HasPrefix(err.Error(), "blocked:") {
		t.Fatal(err)
	}
	if _, err := b.SourceSearch(context.Background(), "pypi", "a/b", 1); err == nil {
		t.Fatal("invalid package accepted")
	}
	for _, response := range []string{`{`, `{"error":"detail"}`, `{"error_id":502}`, `{}`, `null`, `[]`} {
		b = newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
		if _, err := b.SourceSearch(context.Background(), "github", "go", 1); err == nil {
			t.Fatal("source error accepted")
		}
	}
	b = newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429); fmt.Fprint(w, testKey) }))
	if _, err := b.SourceSearch(context.Background(), "github", "go", 1); err == nil || strings.Contains(err.Error(), testKey) {
		t.Fatal(err)
	}
	b.githubKey = func(context.Context) (string, error) { return "", errors.New(testKey) }
	if _, err := b.SourceSearch(context.Background(), "github", "go", 1); err == nil || strings.Contains(err.Error(), testKey) {
		t.Fatal(err)
	}
	b = newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("anonymous key")
		}
		fmt.Fprint(w, `{"items":[{"html_url":"https://tbench.ai"},{"html_url":"https://github.com/go/go","full_name":"go/go"},{"html_url":"https://example.com"}]}`)
	}))
	r, err := b.SourceSearch(context.Background(), "github", "go", 1)
	if err != nil || len(r) != 1 || r[0].Title != "go/go" {
		t.Fatal(r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = b.SourceSearch(ctx, "wikipedia", "go", 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSourceSiteFallback(t *testing.T) {
	for _, source := range []string{"techmeme", "digg", "youtube"} {
		t.Run(source, func(t *testing.T) {
			b := newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Query().Get("q"), "site:"+source+".com") {
					t.Error("missing site restriction")
				}
				fmt.Fprint(w, `{"web":{"results":[{"title":"Go","url":"https://example.org"}]}}`)
			}))
			b.chain.Providers = b.chain.Providers[1:]
			r, err := b.SourceSearch(context.Background(), source, "go", 1)
			if err != nil || len(r) != 1 || r[0].SourceID != source {
				t.Fatal(r, err)
			}
		})
	}
}

func TestSourceBoundsAndPubMedFailures(t *testing.T) {
	b := newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", 101)) }))
	b.limits.ResponseBytes = 100
	if _, err := b.SourceSearch(context.Background(), "github", "go", 1); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatal(err)
	}
	for _, stage := range []string{"search", "summary", "record", "empty"} {
		t.Run(stage, func(t *testing.T) {
			calls := 0
			b := newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if strings.HasSuffix(r.URL.Path, "esearch.fcgi") {
					switch stage {
					case "search":
						fmt.Fprint(w, `{`)
					case "empty":
						fmt.Fprint(w, `{"esearchresult":{"idlist":[]}}`)
					default:
						fmt.Fprint(w, `{"esearchresult":{"idlist":["123"]}}`)
					}
				} else {
					if stage == "summary" {
						fmt.Fprint(w, `{`)
					} else {
						fmt.Fprint(w, `{"result":{"123":"bad"}}`)
					}
				}
			}))
			r, err := b.SourceSearch(context.Background(), "pubmed", "go", 1)
			if stage == "empty" {
				if err != nil || len(r) != 0 || calls != 1 {
					t.Fatal(r, err, calls)
				}
			} else if err == nil {
				t.Fatal("invalid PubMed accepted")
			}
		})
	}
}

func TestGitHubEscapedCredentialIsRedacted(t *testing.T) {
	escaped := strings.ReplaceAll(testKey, "fixture", "\\u0066ixture")
	b := newTestBackend(t, fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"items":[{"full_name":"repo","html_url":"https://github.com/org/repo","description":"%s"}]}`, escaped)
	}))
	b.githubKey = key
	r, err := b.SourceSearch(context.Background(), "github", "go", 1)
	if err != nil || len(r) != 1 || r[0].Snippet != "[redacted]" {
		t.Fatal("credential redaction failed")
	}
}
