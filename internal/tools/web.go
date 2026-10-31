package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// SearchResult is one web/source hit (Section 14.3).
type SearchResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Snippet     string `json:"snippet"`
	PublishedAt string `json:"published_at,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
}

// Page is one light HTTP read (Section 14.3).
type Page struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url"`
	Title       string `json:"title"`
	Markdown    string `json:"markdown"`
	ReadAt      string `json:"read_at"`
	ContentHash string `json:"content_hash"`
}

// SourceEvent records one tool-driven reading for the Sources listing
// (Sections 10.9, 14.3).
type SourceEvent struct {
	Tool      string `json:"tool"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	SourceID  string `json:"source_id,omitempty"`
	At        string `json:"at"`
	Blocked   bool   `json:"blocked"`
	BlockWhy  string `json:"block_why,omitempty"`
	ContentID string `json:"content_hash,omitempty"`
}

// SourceRecorder keeps source events.
type SourceRecorder interface {
	Record(e SourceEvent)
	Events() []SourceEvent
}

// MemorySourceRecorder is an in-memory SourceRecorder.
type MemorySourceRecorder struct {
	mu     sync.Mutex
	events []SourceEvent
}

// Record implements SourceRecorder.
func (r *MemorySourceRecorder) Record(e SourceEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

// Events implements SourceRecorder.
func (r *MemorySourceRecorder) Events() []SourceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SourceEvent(nil), r.events...)
}

// SearchBackend is the harness-side of the search capability (14.3).
// Production wires the own search API; tests use the memory fake.
type SearchBackend interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
	Read(ctx context.Context, url string) (Page, error)
	SourceSearch(ctx context.Context, source, query string, k int) ([]SearchResult, error)
}

// SearchOptions tunes web_search (Section 14.3).
type SearchOptions struct {
	Freshness string
	Domains   []string
	Lang      string
	K         int
}

// KnownSources are the source_search adapters with their own API or
// syntax (Section 13.3).
var KnownSources = []string{
	"github", "hackernews", "arxiv", "semantic-scholar", "openalex",
	"pubmed", "polymarket", "dev", "techmeme", "digg", "youtube",
	"stackoverflow", "huggingface", "crates", "npm", "pypi", "wikipedia",
}

func validSource(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, k := range KnownSources {
		if s == k {
			return true
		}
	}
	return false
}

// MemorySearchBackend is the deterministic harness fake for the search
// capability: preset documents, substring matching, benchmark-source
// blocklist enforced exactly like the eval profile gate.
type MemorySearchBackend struct {
	mu       sync.Mutex
	Docs     []SearchResult
	Pages    map[string]Page
	Reads    int
	Searches int
}

// NewMemorySearchBackend returns a backend with two neutral docs.
func NewMemorySearchBackend() *MemorySearchBackend {
	return &MemorySearchBackend{
		Docs: []SearchResult{
			{URL: "https://docs.python.org/3/library/os.html", Title: "os — Misc OS interfaces", Snippet: "os module docs for path and process operations", SourceID: "docs.python.org"},
			{URL: "https://example.com/go-sqlite-guide", Title: "SQLite guide", Snippet: "WAL readers, one writer, group commit", SourceID: "example.com"},
		},
		Pages: map[string]Page{},
	}
}

// Search implements SearchBackend.
func (b *MemorySearchBackend) Search(_ context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Searches++
	k := opts.K
	if k <= 0 {
		k = 10
	}
	q := strings.ToLower(query)
	var out []SearchResult
	for _, d := range b.Docs {
		if capabilities.BlockedBenchmarkSource(d.URL) {
			continue
		}
		if len(opts.Domains) > 0 {
			matched := false
			for _, dom := range opts.Domains {
				if strings.Contains(strings.ToLower(d.URL), strings.ToLower(dom)) {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		hay := strings.ToLower(d.Title + " " + d.Snippet + " " + d.URL)
		score := 0
		for _, tok := range strings.Fields(q) {
			if strings.Contains(hay, tok) {
				score++
			}
		}
		if score > 0 || q == "" {
			out = append(out, d)
		}
		if len(out) >= k {
			break
		}
	}
	return out, nil
}

// Read implements SearchBackend.
func (b *MemorySearchBackend) Read(_ context.Context, url string) (Page, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Reads++
	if capabilities.BlockedBenchmarkSource(url) {
		return Page{}, fmt.Errorf("blocked: benchmark source %q is not readable (reward hacking, Section 23.2)", url)
	}
	if p, ok := b.Pages[url]; ok {
		return p, nil
	}
	for _, d := range b.Docs {
		if d.URL == url {
			body := "# " + d.Title + "\n\n" + d.Snippet + "\n"
			sum := sha256.Sum256([]byte(body))
			return Page{URL: url, FinalURL: url, Title: d.Title, Markdown: body, ReadAt: time.Now().UTC().Format(time.RFC3339), ContentHash: "sha256:" + hex.EncodeToString(sum[:])}, nil
		}
	}
	return Page{}, fmt.Errorf("unknown url %q (memory backend only serves preset docs)", url)
}

// SourceSearch implements SearchBackend.
func (b *MemorySearchBackend) SourceSearch(_ context.Context, source, query string, k int) ([]SearchResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !validSource(source) {
		return nil, fmt.Errorf("unknown source %q (want one of %s)", source, strings.Join(KnownSources, ", "))
	}
	if k <= 0 {
		k = 10
	}
	// Benchmark copies published on a source are still benchmark
	// sources when the query names them.
	if capabilities.BlockedBenchmarkSource(query) {
		return nil, fmt.Errorf("blocked: query names a benchmark source (Section 23.2)")
	}
	q := strings.ToLower(query)
	var out []SearchResult
	for _, d := range b.Docs {
		if capabilities.BlockedBenchmarkSource(d.URL) {
			continue
		}
		hay := strings.ToLower(d.Title + " " + d.Snippet)
		matched := false
		for _, tok := range strings.Fields(q) {
			if strings.Contains(hay, tok) {
				matched = true
			}
		}
		if matched || q == "" {
			cp := d
			cp.SourceID = source
			out = append(out, cp)
		}
		if len(out) >= k {
			break
		}
	}
	return out, nil
}

var webSearchSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"query":     map[string]any{"type": "string", "minLength": 1},
		"freshness": map[string]any{"type": "string"},
		"domains":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"lang":      map[string]any{"type": "string"},
		"k":         map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
	},
	"required":             []string{"query"},
	"additionalProperties": false,
})

var webReadSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"url": map[string]any{"type": "string", "minLength": 1},
	},
	"required":             []string{"url"},
	"additionalProperties": false,
})

var sourceSearchSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"source": map[string]any{"type": "string", "minLength": 1},
		"query":  map[string]any{"type": "string", "minLength": 1},
		"k":      map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
	},
	"required":             []string{"source", "query"},
	"additionalProperties": false,
})

// WebDeps wires the search backend and the source-event log.
type WebDeps struct {
	Backend SearchBackend
	Sources SourceRecorder
	Now     func() time.Time
}

func (d *WebDeps) backend() SearchBackend {
	if d != nil && d.Backend != nil {
		return d.Backend
	}
	return NewMemorySearchBackend()
}

func (d *WebDeps) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *WebDeps) record(e SourceEvent) {
	if d == nil || d.Sources == nil {
		return
	}
	if e.At == "" {
		e.At = d.now().UTC().Format(time.RFC3339)
	}
	d.Sources.Record(e)
}

// WebSearchTool implements web_search (Sections 13.2 core, 14.3).
type WebSearchTool struct{ Deps *WebDeps }

func (t *WebSearchTool) Definition() Definition {
	return Definition{
		Name: "web_search", Description: "Search the internet with the own search API and register each search as a source",
		InputSchema: webSearchSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 30_000_000_000, OutputBudget: DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "web", Tags: []string{"web", "search"}, Keywords: []string{"web", "search", "internet", "find", "lookup"},
	}
}

func (t *WebSearchTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Query     string   `json:"query"`
		Freshness string   `json:"freshness"`
		Domains   []string `json:"domains"`
		Lang      string   `json:"lang"`
		K         int      `json:"k"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.Query) == "" {
		return Result{Status: StatusInputError, Output: "query is required"}, nil
	}
	results, err := t.deps().backend().Search(ctx, a.Query, SearchOptions{Freshness: a.Freshness, Domains: a.Domains, Lang: a.Lang, K: a.K})
	if err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	for _, r := range results {
		t.deps().record(SourceEvent{Tool: "web_search", URL: r.URL, Title: r.Title, SourceID: r.SourceID, ContentID: ""})
	}
	if len(results) == 0 {
		return Result{Status: StatusOK, Output: "no results"}, nil
	}
	raw, _ := json.MarshalIndent(map[string]any{"results": results}, "", "  ")
	return Result{Status: StatusOK, Output: string(raw)}, nil
}

func (t *WebSearchTool) deps() *WebDeps {
	if t.Deps == nil {
		return &WebDeps{}
	}
	return t.Deps
}

func (t *WebSearchTool) ResourceKeys(_ json.RawMessage) ([]string, error) {
	return nil, nil
}

// WebReadTool implements web_read (Sections 13.2 core, 14.3).
type WebReadTool struct{ Deps *WebDeps }

func (t *WebReadTool) Definition() Definition {
	return Definition{
		Name: "web_read", Description: "Read a page by URL with light HTTP reading as Markdown and register the source",
		InputSchema: webReadSchema, Effect: "read_only", Idempotency: "idempotent",
		Cancel: "immediate", Timeout: 30_000_000_000, OutputBudget: DefaultOutputBudget, Fast: false,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "web", Tags: []string{"web", "read"}, Keywords: []string{"web", "read", "page", "url", "fetch", "article"},
	}
}

func (t *WebReadTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.URL) == "" {
		return Result{Status: StatusInputError, Output: "url is required"}, nil
	}
	if capabilities.BlockedBenchmarkSource(a.URL) {
		t.deps().record(SourceEvent{Tool: "web_read", URL: a.URL, Blocked: true, BlockWhy: "benchmark source (Section 23.2)"})
		return Result{Status: StatusDenied, Output: fmt.Sprintf("blocked: %q is a benchmark source and is never readable (Section 23.2)", a.URL)}, nil
	}
	page, err := t.deps().backend().Read(ctx, a.URL)
	if err != nil {
		if strings.HasPrefix(err.Error(), "blocked:") {
			t.deps().record(SourceEvent{Tool: "web_read", URL: a.URL, Blocked: true, BlockWhy: err.Error()})
			return Result{Status: StatusDenied, Output: err.Error()}, nil
		}
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	t.deps().record(SourceEvent{Tool: "web_read", URL: page.URL, Title: page.Title, SourceID: page.URL, ContentID: page.ContentHash})
	raw, _ := json.MarshalIndent(page, "", "  ")
	return Result{Status: StatusOK, Output: string(raw)}, nil
}

func (t *WebReadTool) deps() *WebDeps {
	if t.Deps == nil {
		return &WebDeps{}
	}
	return t.Deps
}

func (t *WebReadTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"url:" + a.URL}, nil
}

// SourceSearchTool implements source_search (Section 13.3 deferred).
type SourceSearchTool struct{ Deps *WebDeps }

func (t *SourceSearchTool) Definition() Definition {
	return Definition{
		Name: "source_search", Description: "Search a specific source with its own API or syntax: GitHub, Hacker News, arXiv, package registries and Wikipedia",
		InputSchema: sourceSearchSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 30_000_000_000, OutputBudget: DefaultOutputBudget, Fast: false,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "web", Tags: []string{"web", "source"}, Keywords: []string{"source", "search", "github", "arxiv", "packages", "wikipedia", "registry"},
	}
}

func (t *SourceSearchTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Source string `json:"source"`
		Query  string `json:"query"`
		K      int    `json:"k"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if !validSource(a.Source) {
		return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown source %q (want one of %s)", a.Source, strings.Join(KnownSources, ", "))}, nil
	}
	if strings.TrimSpace(a.Query) == "" {
		return Result{Status: StatusInputError, Output: "query is required"}, nil
	}
	results, err := t.deps().backend().SourceSearch(ctx, strings.ToLower(a.Source), a.Query, a.K)
	if err != nil {
		if strings.HasPrefix(err.Error(), "blocked:") {
			t.deps().record(SourceEvent{Tool: "source_search", URL: a.Query, Title: a.Source, Blocked: true, BlockWhy: err.Error()})
			return Result{Status: StatusDenied, Output: err.Error()}, nil
		}
		// Backend input errors (unknown source) surface as input errors.
		if strings.HasPrefix(err.Error(), "unknown source") {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	for _, r := range results {
		t.deps().record(SourceEvent{Tool: "source_search", URL: r.URL, Title: r.Title, SourceID: a.Source})
	}
	if len(results) == 0 {
		return Result{Status: StatusOK, Output: "no results"}, nil
	}
	raw, _ := json.MarshalIndent(map[string]any{"source": strings.ToLower(a.Source), "results": results}, "", "  ")
	return Result{Status: StatusOK, Output: string(raw)}, nil
}

func (t *SourceSearchTool) deps() *WebDeps {
	if t.Deps == nil {
		return &WebDeps{}
	}
	return t.Deps
}

func (t *SourceSearchTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"source:" + strings.ToLower(a.Source)}, nil
}

// WireWebTools connects the real web providers to the core stubs, or
// registers them when missing. web_search and web_read stay core;
// source_search is deferred. The prefix set never changes.
func WireWebTools(reg *MapRegistry, cat *DeferredCatalog, deps *WebDeps) {
	for _, real := range []Tool{
		&WebSearchTool{Deps: deps},
		&WebReadTool{Deps: deps},
		&SourceSearchTool{Deps: deps},
	} {
		name := real.Definition().Name
		existing, err := reg.Get(name)
		if err != nil {
			if cat != nil && name == "source_search" {
				_ = cat.RegisterDeferred(real)
			} else {
				_ = reg.Register(real)
			}
			continue
		}
		if stub, ok := existing.(*StubTool); ok {
			provider := real
			stub.Provider = func(ctx context.Context, inv Invocation) (Result, error) {
				return provider.Invoke(ctx, inv)
			}
		}
		if cat != nil && name == "source_search" {
			cat.MarkDeferred(name)
		}
	}
}
