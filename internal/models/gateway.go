// Single-model gateway (Sections 19.2, 19.3, 19.5, 19.6, H3 subset).
//
// The gateway sits between the engine and the provider adapters of
// internal/providers. H3 implements the single-model path: each model
// role resolves to one primary provider/model plus an ordered fallback
// chain, with budget-bound overload retries, a per-provider circuit breaker and
// versioned price accounting. Escalation to the expert role (19.3),
// the decider (19.4) and multi-lane scheduling (5.2) stay out of scope:
// CallWithLane records the lane and reserves capacity for the
// interactive lane, and ConsultExpert sends one compact prompt without
// history so a later milestone can reuse it.
package models

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Lane names the scheduling lane of a call (Sections 5.2, 19.2). The
// interactive lane carries master turns and control reads; everything
// else runs on the background lane. H3 reserves capacity for the
// interactive lane instead of full lane scheduling.
type Lane string

// Lanes.
const (
	LaneInteractive Lane = "interactive"
	LaneBackground  Lane = "background"
)

// Default tuning (Sections 19.5, 19.6, Appendix B). All configurable
// per GatewayConfig and calibrated later.
const (
	// DefaultMaxAttempts bounds one provider call including the first try.
	DefaultMaxAttempts = 3
	// DefaultBaseDelay is the first backoff step; it doubles per retry.
	DefaultBaseDelay = 500 * time.Millisecond
	// DefaultBreakerFailures opens the breaker after this many
	// consecutive failures.
	DefaultBreakerFailures = 5
	// DefaultBreakerWindow is the error-rate window.
	DefaultBreakerWindow = time.Minute
	// DefaultBreakerCooldown is how long an open breaker refuses calls.
	DefaultBreakerCooldown = 60 * time.Second
)

// Price holds USD per million tokens for one model version (19.6).
// Input counts every input token including cached ones; cache read
// and write are subsets of input, following the ATIF and
// OpenTelemetry GenAI conventions.
type Price struct {
	Input      float64 `json:"input"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning"`
}

// Cost computes the USD cost of one call. Uncached input pays the
// input rate; cached subsets pay their own rates; reasoning tokens
// pay the reasoning rate (falling back to the output rate when zero).
func (p Price) Cost(u providers.Usage) float64 {
	const perMillion = 1_000_000.0
	cached := u.CacheRead + u.CacheWrite
	plain := u.Input - cached
	if plain < 0 {
		plain = 0
	}
	reason := p.Reasoning
	if reason <= 0 {
		reason = p.Output
	}
	return (float64(plain)*p.Input +
		float64(u.CacheRead)*p.CacheRead +
		float64(u.CacheWrite)*p.CacheWrite +
		float64(u.Output)*p.Output +
		float64(u.Reasoning)*reason) / perMillion
}

// ModelRef names one concrete provider/model endpoint.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// ReasoningEffort is fixed per role: changing it breaks the cache
	// on some providers (19.2).
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// MaxTokens caps the completion length for this role.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// RoleEntry is the routing for one model role: a primary plus an
// ordered fallback chain, possibly of another family (19.3, 19.5).
// The compiler re-renders the context for the fallback format; the
// cache loss is the price of availability.
type RoleEntry struct {
	Primary   ModelRef   `json:"primary"`
	Fallbacks []ModelRef `json:"fallbacks,omitempty"`
}

// Chain returns the ordered endpoints to try.
func (r RoleEntry) Chain() []ModelRef {
	return append([]ModelRef{r.Primary}, r.Fallbacks...)
}

// GatewayConfig tunes the gateway. Zero values select the defaults.
type GatewayConfig struct {
	Roles map[types.ModelRole]RoleEntry `json:"roles"`
	// DisableBudgetRetries restores the bounded overload policy for comparisons.
	DisableBudgetRetries bool `json:"disable_budget_retries,omitempty"`
	// Prices is the versioned price table by model id (19.6).
	Prices map[string]Price `json:"prices"`
	// PriceVersion pins which table version computed a cost; it is
	// recorded on every CallMeta for audit.
	PriceVersion    string        `json:"price_version,omitempty"`
	MaxAttempts     int           `json:"max_attempts,omitempty"`
	BaseDelay       time.Duration `json:"base_delay,omitempty"`
	BreakerAfter    int           `json:"breaker_after,omitempty"`
	BreakerWindow   time.Duration `json:"breaker_window,omitempty"`
	BreakerCooldown time.Duration `json:"breaker_cooldown,omitempty"`
	// MaxConcurrent caps simultaneous provider calls. The interactive
	// lane always keeps one slot: background calls wait while the pool
	// is full, interactive calls never wait behind background work.
	MaxConcurrent int `json:"max_concurrent,omitempty"`
}

func (c GatewayConfig) withDefaults() GatewayConfig {
	if c.MaxAttempts < 1 {
		c.MaxAttempts = DefaultMaxAttempts
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = DefaultBaseDelay
	}
	if c.BreakerAfter < 1 {
		c.BreakerAfter = DefaultBreakerFailures
	}
	if c.BreakerWindow <= 0 {
		c.BreakerWindow = DefaultBreakerWindow
	}
	if c.BreakerCooldown <= 0 {
		c.BreakerCooldown = DefaultBreakerCooldown
	}
	if c.Roles == nil {
		c.Roles = map[types.ModelRole]RoleEntry{}
	}
	if c.Prices == nil {
		c.Prices = map[string]Price{}
	}
	return c
}

// CallMeta describes how a call was served.
type CallMeta struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Role     types.ModelRole `json:"role"`
	Lane     Lane            `json:"lane"`
	// Attempts counts provider tries including the first.
	Attempts         int     `json:"attempts"`
	RetryWaitCount   int     `json:"retry_wait_count"`
	RetryWaitSeconds float64 `json:"retry_wait_seconds"`
	// Fallback is true when a non-primary endpoint served the call.
	Fallback bool `json:"fallback"`
	// Retried is true when any retry happened.
	Retried bool `json:"retried"`
	// CredentialSwitches counts moves to the fallback credential
	// after a confirmed quota or billing exhaustion (EXCALIBUR_QUOTA_PROBE).
	CredentialSwitches int `json:"credential_switches,omitempty"`
	// CostUSD is the price-table cost of the serving call.
	CostUSD      float64 `json:"cost_usd"`
	PriceVersion string  `json:"price_version,omitempty"`
}

// breaker is one provider circuit breaker (19.5): after BreakerAfter
// consecutive failures, or an error rate above 50% inside the window,
// the provider stays open for the cooldown and calls fail over.
type breaker struct {
	consec    int
	events    []breakerEvent
	openUntil time.Time
}

type breakerEvent struct {
	at     time.Time
	failed bool
}

func (b *breaker) open(now time.Time) bool { return now.Before(b.openUntil) }

func (b *breaker) record(now time.Time, failed bool, after int, window, cooldown time.Duration) {
	if failed {
		b.consec++
	} else {
		b.consec = 0
	}
	b.events = append(b.events, breakerEvent{at: now, failed: failed})
	cut := now.Add(-window)
	kept := b.events[:0]
	var fails, total int
	for _, e := range b.events {
		if e.at.Before(cut) {
			continue
		}
		kept = append(kept, e)
		total++
		if e.failed {
			fails++
		}
	}
	b.events = kept
	if b.consec >= after || (total >= 4 && fails*2 > total) {
		b.openUntil = now.Add(cooldown)
	}
}

// Gateway is the H3 single-model gateway: role routing, overload
// retries bounded by caller deadlines, per-provider breakers with failover,
// price accounting and a concurrency cap with an interactive reserve.
type Gateway struct {
	cfg GatewayConfig
	now func() time.Time

	mu        sync.Mutex
	providers map[string]providers.Provider
	breakers  map[string]*breaker
	inflight  int
}

// NewGateway returns a Gateway with no providers registered. Register
// each adapter before calling.
func NewGateway(cfg GatewayConfig) *Gateway {
	return &Gateway{
		cfg:       cfg.withDefaults(),
		now:       time.Now,
		providers: map[string]providers.Provider{},
		breakers:  map[string]*breaker{},
	}
}

// Register adds one provider adapter. Names must match the ModelRef
// Provider fields of the role table.
func (g *Gateway) Register(p providers.Provider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[p.Name()] = p
}

// Close releases every registered provider.
func (g *Gateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var first error
	for _, p := range g.providers {
		if err := p.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// BreakerOpen reports whether the provider breaker is currently open.
// It is exported for tests and status reads.
func (g *Gateway) BreakerOpen(provider string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, ok := g.breakers[provider]
	if !ok {
		return false
	}
	return b.open(g.now())
}

// CostOf prices one usage record with the table entry for model.
func (g *Gateway) CostOf(model string, u providers.Usage) float64 {
	return g.cfg.Prices[model].Cost(u)
}

func (g *Gateway) breakerFor(provider string) *breaker {
	b, ok := g.breakers[provider]
	if !ok {
		b = &breaker{}
		g.breakers[provider] = b
	}
	return b
}

// Call serves one model call on the background lane.
func (g *Gateway) Call(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, CallMeta, error) {
	return g.CallWithLane(ctx, LaneBackground, role, req)
}

// Chat serves one background-lane call. It is the engine seam:
// the engine always runs on the background lane, while master
// turns and control reads use the interactive lane above it.
func (g *Gateway) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, CallMeta, error) {
	return g.Call(ctx, role, req)
}

// CallWithLane serves one model call: it walks the role chain,
// retrying transient failures with backoff and failing over when the
// breaker is open or the attempts run out. The request Model is set
// from the endpoint being tried so the same compiled request serves
// every fallback.
func (g *Gateway) CallWithLane(ctx context.Context, lane Lane, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, CallMeta, error) {
	entry, ok := g.cfg.Roles[role]
	if !ok {
		return providers.ChatResponse{}, CallMeta{}, fmt.Errorf("models: no route for role %q", string(role))
	}
	chain := entry.Chain()
	if len(chain) == 0 {
		return providers.ChatResponse{}, CallMeta{}, fmt.Errorf("models: empty chain for role %q", string(role))
	}
	var lastErr error
	var waits retryWaitStats
	var failedAttempts int
	var failedRef ModelRef
	var failedFallback bool
	for i, ref := range chain {
		if err := ctx.Err(); err != nil {
			return providers.ChatResponse{}, CallMeta{}, err
		}
		g.mu.Lock()
		b := g.breakerFor(ref.Provider)
		isOpen := b.open(g.now())
		g.mu.Unlock()
		if isOpen {
			lastErr = &providers.ProviderError{
				Kind: providers.KindTransient, Provider: ref.Provider, Model: ref.Model,
				Err: fmt.Errorf("models: breaker open for %q", ref.Provider),
			}
			failedRef = ref
			failedFallback = i > 0
			continue
		}
		if err := g.acquire(ctx, lane); err != nil {
			return providers.ChatResponse{}, CallMeta{}, err
		}
		resp, attempts, err := g.tryEndpoint(context.WithValue(ctx, retryStatsKey{}, &waits), ref, req)
		g.release()
		now := g.now()
		g.mu.Lock()
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			g.breakerFor(ref.Provider).record(now, err != nil, g.cfg.BreakerAfter, g.cfg.BreakerWindow, g.cfg.BreakerCooldown)
		}
		g.mu.Unlock()
		if err == nil {
			cost := g.CostOf(ref.Model, resp.Usage)
			return resp, CallMeta{
				Provider: ref.Provider, Model: ref.Model, Role: role, Lane: lane,
				RetryWaitCount: waits.Count, RetryWaitSeconds: waits.Duration.Seconds(),
				Attempts: attempts, Fallback: i > 0, Retried: attempts > 1,
				CredentialSwitches: waits.Switches,
				CostUSD:            cost, PriceVersion: g.cfg.PriceVersion,
			}, nil
		}
		lastErr = err
		failedAttempts += attempts
		failedRef = ref
		failedFallback = i > 0
		if !retryableChain(err, ref, req) {
			return providers.ChatResponse{}, CallMeta{
				Provider: ref.Provider, Model: ref.Model, Role: role, Lane: lane,
				RetryWaitCount: waits.Count, RetryWaitSeconds: waits.Duration.Seconds(),
				Attempts: failedAttempts, Fallback: failedFallback, Retried: failedAttempts > 1,
				CredentialSwitches: waits.Switches,
			}, err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("models: all endpoints of role %q refused (breakers open)", string(role))
	}
	// Report how many provider tries happened even though none
	// served the call, so recorders and metrics account retries
	// (Appendix F) instead of reporting zero on failure.
	return providers.ChatResponse{}, CallMeta{
		Provider: failedRef.Provider, Model: failedRef.Model, Role: role, Lane: lane,
		RetryWaitCount: waits.Count, RetryWaitSeconds: waits.Duration.Seconds(),
		Attempts: failedAttempts, Fallback: failedFallback, Retried: failedAttempts > 1,
		CredentialSwitches: waits.Switches,
	}, lastErr
}

// tryEndpoint calls one endpoint with bounded transport retries and
// budget-bound overload retries, honoring server delays before exponential
// backoff with jitter. Content errors (refusal, truncation) get one
// retry with a larger output limit when the request sets one (19.5).
func (g *Gateway) tryEndpoint(ctx context.Context, ref ModelRef, req providers.ChatRequest) (providers.ChatResponse, int, error) {
	g.mu.Lock()
	p, ok := g.providers[ref.Provider]
	maxAttempts := g.cfg.MaxAttempts
	base := g.cfg.BaseDelay
	g.mu.Unlock()
	if !ok {
		return providers.ChatResponse{}, 0, &providers.ProviderError{
			Kind: providers.KindInput, Provider: ref.Provider, Model: ref.Model,
			Err: fmt.Errorf("models: provider %q not registered", ref.Provider),
		}
	}
	out := req
	out.Model = ref.Model
	if ref.ReasoningEffort != "" {
		out.ReasoningEffort = ref.ReasoningEffort
	}
	if ref.MaxTokens > 0 {
		out.MaxTokens = ref.MaxTokens
	}
	var resp providers.ChatResponse
	var err error
	attempts := 0
	credentialAttempt := 1
	for attempt := 1; ; attempt++ {
		attempts = attempt
		if ctxErr := ctx.Err(); ctxErr != nil {
			return providers.ChatResponse{}, attempts, ctxErr
		}
		resp, err = p.Chat(context.WithValue(ctx, retryAttemptKey{}, credentialAttempt), out)
		if err == nil {
			return resp, attempts, nil
		}
		var perr *providers.ProviderError
		if !errors.As(err, &perr) {
			return providers.ChatResponse{}, attempts, err
		}
		switch perr.Kind {
		case providers.KindContentRejected, providers.KindTruncated:
			if out.MaxTokens > 0 && attempt == 1 {
				out.MaxTokens *= 2
				continue
			}
			return providers.ChatResponse{}, attempts, err
		case providers.KindTransient, providers.KindRateLimited:
			if attempt >= maxAttempts && (g.cfg.DisableBudgetRetries || !budgetRetryAllowed(err, attempt)) {
				return providers.ChatResponse{}, attempts, err
			}
			credentialAttempt++
			wait := retryDelay(perr, base, attempt)
			// The sleep below ends when the caller's wall deadline does, so
			// the total wait never exceeds the remaining wall budget.
			started := time.Now()
			sleepErr := providers.SleepContext(ctx, wait)
			if stats, ok := ctx.Value(retryStatsKey{}).(*retryWaitStats); ok {
				stats.Count++
				stats.Duration += time.Since(started)
			}
			if sleepErr != nil {
				return providers.ChatResponse{}, attempts, ctx.Err()
			}
			continue
		default:
			return providers.ChatResponse{}, attempts, err
		}
	}
}

// retryableChain reports whether the chain should move to the next
// endpoint after err: only transient/rate-limited failures fail over.
// Auth and input errors return to the caller with no blind retry.
func retryableChain(err error, _ ModelRef, _ providers.ChatRequest) bool {
	perr, ok := err.(*providers.ProviderError)
	if !ok {
		return false
	}
	return perr.Retryable()
}

func (g *Gateway) acquire(ctx context.Context, lane Lane) error {
	if g.cfg.MaxConcurrent <= 0 {
		return nil
	}
	for {
		g.mu.Lock()
		reserve := 0
		if lane != LaneInteractive {
			reserve = 1
		}
		if g.inflight < g.cfg.MaxConcurrent-reserve {
			g.inflight++
			g.mu.Unlock()
			return nil
		}
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (g *Gateway) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight > 0 {
		g.inflight--
	}
}

// ConsultExpert sends one compact expert prompt without conversation
// history (19.3): objective, state, the exact error, what was tried
// and the relevant fragments. The cheap model keeps executing from
// the returned diagnosis and plan. H3 provides the plumbing; the
// escalate-on-loop policy lives in the engine.
func (g *Gateway) ConsultExpert(ctx context.Context, objective, state, exactError string, tried, fragments []string) (string, CallMeta, error) {
	body := "objective:\n" + objective + "\nstate:\n" + state + "\nexact error:\n" + exactError
	if len(tried) > 0 {
		body += "\ntried:\n"
		for _, t := range tried {
			body += "- " + t + "\n"
		}
	}
	if len(fragments) > 0 {
		body += "\nrelevant fragments:\n"
		for _, f := range fragments {
			body += "- " + f + "\n"
		}
	}
	resp, meta, err := g.Call(ctx, types.RoleExpert, providers.ChatRequest{
		Messages: []providers.Message{{Role: providers.RoleUser, Content: body}},
	})
	if err != nil {
		return "", meta, err
	}
	return resp.Message.Content, meta, nil
}
