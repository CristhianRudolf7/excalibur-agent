package ctxcompiler

import (
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// This file implements Section 11.6: every provider caches on exact
// prefix match in tools, system, messages order, and each adapter
// applies the same layered structure with its own marks, keys and
// TTLs. It also carries the drift watchdog (L0/L1 must never change
// inside a run), the Anthropic sibling prewarm, and the usage math
// the cache suite compares against the theoretical prefix.

// ProviderFamily groups providers by cache behavior.
type ProviderFamily string

// Provider families of Section 11.6.
const (
	FamilyAnthropic  ProviderFamily = "anthropic"
	FamilyOpenAI     ProviderFamily = "openai"
	FamilyGemini     ProviderFamily = "gemini"
	FamilyDeepSeek   ProviderFamily = "deepseek"
	FamilyQwen       ProviderFamily = "qwen"
	FamilyGLM        ProviderFamily = "glm"
	FamilyKimi       ProviderFamily = "kimi"
	FamilyOpenRouter ProviderFamily = "openrouter"
	FamilyLocal      ProviderFamily = "local"
	FamilyGeneric    ProviderFamily = "generic"
)

// ClassifyProvider maps a provider name to its cache family.
// Unknown names select the generic stable-prefix behavior.
func ClassifyProvider(name string) ProviderFamily {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "anthropic"):
		return FamilyAnthropic
	case strings.Contains(n, "openai"):
		return FamilyOpenAI
	case strings.Contains(n, "gemini"):
		return FamilyGemini
	case strings.Contains(n, "deepseek"):
		return FamilyDeepSeek
	case strings.Contains(n, "qwen"):
		return FamilyQwen
	case strings.Contains(n, "glm"):
		return FamilyGLM
	case strings.Contains(n, "kimi"):
		return FamilyKimi
	case strings.Contains(n, "openrouter"):
		return FamilyOpenRouter
	case strings.Contains(n, "ollama"),
		strings.Contains(n, "vllm"),
		strings.Contains(n, "llama"):
		return FamilyLocal
	default:
		return FamilyGeneric
	}
}

// Pace selects the cache TTL: human-paced master turns hold the
// cache for an hour, machine-paced execution steps for five minutes
// (same criterion as Hermes, 11.6).
type Pace int

// Paces.
const (
	HumanPace   Pace = iota // master and conversation rhythm: TTL 1h
	MachinePace             // execution rhythm: TTL 5min
)

// Breakpoint is one cache mark after a layer.
type Breakpoint struct {
	// After is the layer the mark closes.
	After Layer
	// Kind is "cache_control" (explicit) or "implicit".
	Kind string
}

// PrewarmPlan tells the orchestrator how to launch same-role
// siblings on Anthropic: an entry is only readable once the first
// response starts, so N parallel same-prefix requests would all pay
// full price. The leader goes first; the rest launch after its
// first token (11.6).
type PrewarmPlan struct {
	// Supported is true where prewarming pays (Anthropic).
	Supported bool
	// LeaderFirst waits for the leader first token before siblings.
	LeaderFirst bool
}

// CachePlan is the per-provider cache wiring for one role and scope.
type CachePlan struct {
	Family ProviderFamily
	// Breakpoints are the explicit marks in layer order.
	Breakpoints []Breakpoint
	// MaxBreakpoints caps explicit marks (Anthropic: 4).
	MaxBreakpoints int
	// PromptCacheKey is the stable key for automatic caches
	// (OpenAI): fixed per role and scope.
	PromptCacheKey string
	// TTL is the provider entry lifetime for this pace.
	TTL time.Duration
	// MinTokens is the minimum input for the automatic cache
	// (OpenAI: 1024).
	MinTokens int
	// ReasoningFixed pins reasoning effort per role so it never
	// breaks the prefix.
	ReasoningFixed bool
	// RestrictViaAllowlist prefers allowed_tools/tool_choice over
	// changing the tool list.
	RestrictViaAllowlist bool
	// Prewarm carries the sibling launch plan.
	Prewarm PrewarmPlan
	// Notes are the human-readable gotchas (amortization, 2050-token
	// rule, backward-search coverage).
	Notes []string
}

// Plan builds the cache wiring for family, role, scope and pace.
func Plan(family ProviderFamily, role, scopeID string, pace Pace) CachePlan {
	ttl := 5 * time.Minute
	if pace == HumanPace {
		ttl = time.Hour
	}
	switch family {
	case FamilyAnthropic:
		notes := []string{
			"reads need the first response byte; prewarm same-role siblings",
			"backward search covers 20 blocks",
		}
		if pace == HumanPace {
			notes = append(notes, "1h TTL amortizes at 3 requests")
		} else {
			notes = append(notes, "5min TTL amortizes at 2 requests")
		}
		return CachePlan{
			Family: family,
			Breakpoints: []Breakpoint{
				{After: LayerCore, Kind: "cache_control"},
				{After: LayerIdentity, Kind: "cache_control"},
				{After: LayerScope, Kind: "cache_control"},
				{After: LayerActivity, Kind: "cache_control"},
			},
			MaxBreakpoints: 4,
			TTL:            ttl,
			ReasoningFixed: true,
			Prewarm:        PrewarmPlan{Supported: true, LeaderFirst: true},
			Notes:          notes,
		}
	case FamilyOpenAI:
		key := "role/" + role
		if scopeID != "" {
			key += "/scope/" + scopeID
		}
		return CachePlan{
			Family:               family,
			PromptCacheKey:       key,
			TTL:                  ttl,
			MinTokens:            1024,
			ReasoningFixed:       true,
			RestrictViaAllowlist: true,
			Notes: []string{
				"automatic cache from 1024 tokens",
				"keep reasoning effort and output schema fixed per role",
				"restrict tools via allowed_tools or tool_choice, never by editing the list",
			},
		}
	case FamilyGemini:
		return CachePlan{
			Family: family,
			TTL:    ttl,
			Notes: []string{
				"implicit cache on stable prefix and close-timed requests",
				"a prior full request only reuses when the new one adds 2050+ tokens; small steps may not reuse",
			},
		}
	case FamilyQwen:
		return CachePlan{
			Family: family,
			Breakpoints: []Breakpoint{
				{After: LayerCore, Kind: "cache_control"},
				{After: LayerIdentity, Kind: "cache_control"},
				{After: LayerScope, Kind: "cache_control"},
			},
			MaxBreakpoints: 4,
			TTL:            ttl,
			Notes:          []string{"explicit marks supported; stable prefix otherwise"},
		}
	case FamilyLocal:
		return CachePlan{
			Family: family,
			TTL:    ttl,
			Notes:  []string{"server prefix cache; the same stable prefix activates it"},
		}
	default:
		// DeepSeek, GLM, Kimi, OpenRouter and generic providers:
		// automatic prefix cache, stable prefix only.
		return CachePlan{
			Family: family,
			TTL:    ttl,
			Notes:  []string{"automatic prefix cache; stable prefix only"},
		}
	}
}

// ShouldPrewarm reports whether same-role siblings must launch
// leader-first: Anthropic entries become readable only when the
// first response starts.
func ShouldPrewarm(family ProviderFamily, siblings int, sameRolePrefix bool) bool {
	return family == FamilyAnthropic && siblings > 1 && sameRolePrefix
}

// Drift is one unexpected stable-prefix change inside a run.
type Drift struct {
	RunID string
	Layer Layer
	Prev  string
	Curr  string
}

// DriftDetector watches layer hashes per run: L0 or L1 changing
// inside a session is a harness defect (11.6). Deltas keep L2/L3
// bytes stable until compaction, so only L0/L1 fire. It is safe for
// concurrent use.
type DriftDetector struct {
	mu   sync.Mutex
	last map[string][NumLayers]string
	seen map[string]bool
}

// NewDriftDetector returns an empty DriftDetector.
func NewDriftDetector() *DriftDetector {
	return &DriftDetector{
		last: make(map[string][NumLayers]string),
		seen: make(map[string]bool),
	}
}

// Observe records the layer hashes of one call and returns the
// defects: L0/L1 hashes differing from the previous call of the
// same run. The first call of a run never drifts.
func (d *DriftDetector) Observe(runID string, hashes [NumLayers]string) []Drift {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.seen[runID] {
		d.seen[runID] = true
		d.last[runID] = hashes
		return nil
	}
	prev := d.last[runID]
	d.last[runID] = hashes
	var out []Drift
	for _, l := range []Layer{LayerCore, LayerIdentity} {
		if prev[int(l)] != hashes[int(l)] {
			out = append(out, Drift{RunID: runID, Layer: l, Prev: prev[int(l)], Curr: hashes[int(l)]})
		}
	}
	return out
}

// Forget drops the baseline of a finished run.
func (d *DriftDetector) Forget(runID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.last, runID)
	delete(d.seen, runID)
}

// TheoreticalStableLayers counts the leading layers whose hashes
// match between two compilations: the prefix a provider could have
// served from cache.
func TheoreticalStableLayers(prev, curr [NumLayers]string) int {
	n := 0
	for i := 0; i < NumLayers; i++ {
		if prev[i] != curr[i] {
			break
		}
		n++
	}
	return n
}

// CacheStats accumulates the usage the provider returns per call.
// The cache suite compares the real hit rate against the theoretical
// prefix (11.8).
type CacheStats struct {
	Requests      int64
	InputTokens   int64
	CachedTokens  int64
	WrittenTokens int64
}

// Add folds one response usage into the stats. It is not safe for
// concurrent use; guard it at the call site.
func (s *CacheStats) Add(u providers.Usage) {
	s.Requests++
	s.InputTokens += u.Input
	s.CachedTokens += u.CacheRead
	s.WrittenTokens += u.CacheWrite
}

// HitRate is cached input over total input, 0 without data.
func (s CacheStats) HitRate() float64 {
	if s.InputTokens <= 0 {
		return 0
	}
	return float64(s.CachedTokens) / float64(s.InputTokens)
}
