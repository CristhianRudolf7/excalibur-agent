// Package websearch implements transient web search and guarded page reading.
// Brave storage requires an explicit storage-rights opt-in. Training and
// benchmarking use remain excluded.
package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// KeyResolver resolves a credential at the execution boundary and honors context cancellation.
type KeyResolver func(context.Context) (string, error)

// Provider performs a bounded search without retaining response data.
type Provider interface {
	Name() string
	Search(context.Context, string, tools.SearchOptions) ([]tools.SearchResult, error)
}

// Renderer is a trusted browser hook that must enforce request-level SSRF guards.
type Renderer interface {
	Render(context.Context, string) (html string, finalURL string, err error)
}

// Limits bounds requests and output; zero values use safe defaults.
type Limits struct {
	Timeout       time.Duration
	ResponseBytes int64
	PageBytes     int64
	MarkdownBytes int
	RetryAfterCap time.Duration
}

// ProviderConfig binds a supported provider name to a deferred credential resolver.
type ProviderConfig struct {
	Name        string
	KeyRef      string
	KeyResolver KeyResolver
	// StorageRights permits durable Brave use only under a plan granting storage rights.
	StorageRights bool
}

// Config supplies collaborators without changing the shared configuration contract.
type Config struct {
	// Durable refuses Brave before requests unless its provider has StorageRights.
	Durable    bool
	Providers  []ProviderConfig
	Order      []string
	HTTPClient *http.Client
	Renderer   Renderer
	UserAgent  string
	Limits     Limits
	// ResolveKey binds a secret reference to an execution-boundary resolver.
	ResolveKey func(string) KeyResolver
	// GitHubKey is optional; nil uses anonymous repository search.
	GitHubKey KeyResolver
}

// Backend implements real web search, source search and guarded page reads.
type Backend struct {
	chain     *Chain
	client    *http.Client
	reader    *http.Client
	renderer  Renderer
	userAgent string
	limits    Limits
	githubKey KeyResolver
}

var _ tools.SearchBackend = (*Backend)(nil)

func defaults(l Limits) Limits {
	if l.Timeout <= 0 {
		l.Timeout = 15 * time.Second
	}
	if l.ResponseBytes <= 0 || l.ResponseBytes > 2<<20 {
		l.ResponseBytes = 2 << 20
	}
	if l.PageBytes <= 0 || l.PageBytes > 5<<20 {
		l.PageBytes = 5 << 20
	}
	if l.MarkdownBytes <= 0 || l.MarkdownBytes > 5<<20 {
		l.MarkdownBytes = 256 << 10
	}
	if l.MarkdownBytes < len(truncationMarker) {
		l.MarkdownBytes = len(truncationMarker)
	}
	if l.RetryAfterCap <= 0 || l.RetryAfterCap > 2*time.Second {
		l.RetryAfterCap = 2 * time.Second
	}
	return l
}

// New validates provider configuration and constructs independent API and reader clients.
func New(c Config) (*Backend, error) {
	if len(c.Order) == 0 {
		return nil, errors.New("websearch: provider order is empty")
	}
	l := defaults(c.Limits)
	client := &http.Client{}
	if c.HTTPClient != nil {
		*client = *c.HTTPClient
	}
	client.Timeout = l.Timeout
	client.Jar = nil
	// Do not forward provider credentials to redirect destinations.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ua := c.UserAgent
	if ua == "" {
		ua = "Mozilla/5.0 (compatible; Excalibur/1.0; https://github.com/CristhianRudolf7/excalibur-agent)"
	}
	ps := map[string]Provider{}
	for _, p := range c.Providers {
		if p.Name != "tavily" && p.Name != "brave" {
			return nil, errors.New("websearch: unknown provider")
		}
		if _, ok := ps[p.Name]; ok {
			return nil, errors.New("websearch: duplicate provider")
		}
		key := p.KeyResolver
		if key == nil && c.ResolveKey != nil {
			key = c.ResolveKey(p.KeyRef)
		}
		ps[p.Name] = &apiProvider{name: p.Name, key: key, client: client, limits: l, userAgent: ua}
		if c.Durable && p.Name == "brave" && !p.StorageRights {
			ps[p.Name] = durableBrave{}
		}
	}
	chain := &Chain{RetryAfterCap: l.RetryAfterCap}
	seen := map[string]bool{}
	for _, name := range c.Order {
		p, ok := ps[name]
		if !ok || seen[name] {
			return nil, errors.New("websearch: unknown or duplicate provider in order")
		}
		seen[name] = true
		chain.Providers = append(chain.Providers, p)
	}
	return &Backend{chain: chain, client: client, reader: newReaderClient(l.Timeout), renderer: c.Renderer, userAgent: ua, limits: l, githubKey: c.GitHubKey}, nil
}

// Search implements tools.SearchBackend without model-visible provider telemetry.
func (b *Backend) Search(ctx context.Context, q string, o tools.SearchOptions) ([]tools.SearchResult, error) {
	r, provider, err := b.SearchWithProvider(ctx, q, o)
	for i := range r {
		r[i].Provider = provider
	}
	return r, err
}

// SearchWithProvider returns provider identity separately for transient telemetry.
func (b *Backend) SearchWithProvider(ctx context.Context, q string, o tools.SearchOptions) ([]tools.SearchResult, string, error) {
	return b.chain.SearchWithProvider(ctx, q, o)
}
func count(k int) int {
	if k <= 0 {
		return 10
	}
	if k > 20 {
		return 20
	}
	return k
}
func filtered(in []tools.SearchResult, k int) []tools.SearchResult {
	out := make([]tools.SearchResult, 0)
	for _, r := range in {
		if !capabilities.BlockedBenchmarkSource(r.URL) {
			out = append(out, r)
			if len(out) >= count(k) {
				break
			}
		}
	}
	return out
}

// ProviderError intentionally excludes response bodies, request URLs and resolver errors.
type ProviderError struct {
	DurableRefused bool
	Provider       string
	Status         int
	Transient      bool
	MissingKey     bool
	RetryAfter     time.Duration
}

func (e *ProviderError) Error() string {
	if e.DurableRefused {
		return "websearch: Brave is unavailable in durable runs (transient use only)"
	}
	if e.MissingKey {
		return "websearch: provider key unavailable"
	}
	return fmt.Sprintf("websearch: %s request failed (status %d)", e.Provider, e.Status)
}

// AllFailedError reports exhaustion of all fallback-eligible providers.
type AllFailedError struct{ Failures []error }

func (e *AllFailedError) Error() string {
	for _, failure := range e.Failures {
		var p *ProviderError
		if errors.As(failure, &p) && p.DurableRefused {
			return "websearch: all providers failed; Brave is unavailable in durable runs (transient use only)"
		}
	}
	return "websearch: all providers failed"
}
func (e *AllFailedError) Unwrap() []error { return e.Failures }

// Chain uses configured provider order and bounded, cancellable fallback waits.
type Chain struct {
	Providers     []Provider
	RetryAfterCap time.Duration
}

// Search returns the first successful provider response.
func (c *Chain) Search(ctx context.Context, q string, o tools.SearchOptions) ([]tools.SearchResult, error) {
	r, _, e := c.SearchWithProvider(ctx, q, o)
	return r, e
}

// SearchWithProvider also reports the successful provider for transient telemetry.
func (c *Chain) SearchWithProvider(ctx context.Context, q string, o tools.SearchOptions) ([]tools.SearchResult, string, error) {
	failures := []error{}
	for _, p := range c.Providers {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		r, err := p.Search(ctx, q, o)
		if err == nil {
			return filtered(r, o.K), p.Name(), nil
		}
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		failures = append(failures, err)
		var pe *ProviderError
		if !errors.As(err, &pe) || (!pe.Transient && !pe.MissingKey) {
			return nil, "", err
		}
		delay := pe.RetryAfter
		cap := c.RetryAfterCap
		if cap <= 0 || cap > 2*time.Second {
			cap = 2 * time.Second
		}
		if delay > cap {
			delay = cap
		}
		if delay > 0 {
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, "", ctx.Err()
			case <-t.C:
			}
		}
	}
	return nil, "", &AllFailedError{Failures: failures}
}
