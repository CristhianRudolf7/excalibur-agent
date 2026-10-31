package openaiwire

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Price carries per-million-token USD rates for one model. It is the
// hook the model gateway price table (Section 19.6, H3) uses to turn
// reported usage into cost. Rates are snapshots: the gateway owns the
// versioned table, adapters only report usage in this shape.
type Price struct {
	Input      float64 `json:"input"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning"`
}

// Cost converts usage to USD with the given rates. Unknown or zero
// rates contribute zero, so a partially known table never invents
// cost. Reasoning tokens ride inside output on most wires; when the
// reasoning rate is zero they fall back to the output rate.
func Cost(u providers.Usage, p Price) float64 {
	reasonRate := p.Reasoning
	if reasonRate == 0 {
		reasonRate = p.Output
	}
	return float64(u.Input-u.CacheRead-u.CacheWrite)/1e6*p.Input +
		float64(u.CacheRead)/1e6*p.CacheRead +
		float64(u.CacheWrite)/1e6*p.CacheWrite +
		float64(u.Output-u.Reasoning)/1e6*p.Output +
		float64(u.Reasoning)/1e6*reasonRate
}

// NormalizeUsage builds internal usage from parts, enforcing the
// Section 19.6 invariant: cache read and cache write are subsets of
// input, reasoning is a subset of output. Adapters with their own
// usage shapes (e.g. Responses) map fields then normalize here.
func NormalizeUsage(input, cacheRead, cacheWrite, output, reasoning int64) providers.Usage {
	u := providers.Usage{
		Input:      input,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Output:     output,
		Reasoning:  reasoning,
	}
	if u.CacheRead < 0 {
		u.CacheRead = 0
	}
	if u.CacheWrite < 0 {
		u.CacheWrite = 0
	}
	if u.Reasoning < 0 {
		u.Reasoning = 0
	}
	if u.CacheRead > u.Input {
		u.CacheRead = u.Input
	}
	if u.CacheWrite > u.Input-u.CacheRead {
		u.CacheWrite = u.Input - u.CacheRead
		if u.CacheWrite < 0 {
			u.CacheWrite = 0
		}
	}
	if u.Reasoning > u.Output {
		u.Reasoning = u.Output
	}
	return u
}

// toUsage maps a wire usage payload to the internal convention: input
// includes cached tokens, cache read and cache write are subsets of
// input (Section 19.6). A nil payload yields zero usage, which some
// local servers omit entirely.
//
//   - CacheRead comes from prompt_tokens_details.cached_tokens, or
//     from the legacy DeepSeek prompt_cache_hit_tokens.
//   - CacheWrite comes from the legacy DeepSeek
//     prompt_cache_miss_tokens: tokens that missed the cache and were
//     computed at full price, populating it. Wires without an
//     explicit write counter report zero, never an estimate.
//   - Reasoning comes from completion_tokens_details.reasoning_tokens.
//
// Values are clamped so the subset invariant holds even when a server
// reports inconsistent counters.
func toUsage(w *wireUsage) providers.Usage {
	if w == nil {
		return providers.Usage{}
	}
	input := w.PromptTokens
	if input <= 0 && w.TotalTokens > 0 {
		input = w.TotalTokens - w.CompletionTokens
		if input < 0 {
			input = 0
		}
	}
	var cacheRead, cacheWrite, reasoning int64
	if w.PromptTokensDetails != nil {
		cacheRead = w.PromptTokensDetails.CachedTokens
	}
	if w.PromptCacheHitTokens > cacheRead {
		cacheRead = w.PromptCacheHitTokens
	}
	cacheWrite = w.PromptCacheMissTokens
	if w.CompletionTokensDetails != nil {
		reasoning = w.CompletionTokensDetails.ReasoningTokens
	}
	return NormalizeUsage(input, cacheRead, cacheWrite, w.CompletionTokens, reasoning)
}
