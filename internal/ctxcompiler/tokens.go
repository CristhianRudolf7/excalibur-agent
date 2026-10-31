package ctxcompiler

import (
	"math"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Counter counts tokens in text. The compiler takes one so tests can
// inject exact counters while production passes the calibrated
// estimator or a provider tokenizer (11.7).
type Counter interface {
	Count(text string) int
}

// CountMessages totals the content, tool-call argument and tool
// result costs of one message list.
func CountMessages(msgs []providers.Message, counter Counter) int {
	total := 0
	for _, m := range msgs {
		total += counter.Count(m.Content)
		for _, tc := range m.ToolCalls {
			total += counter.Count(tc.Name) + counter.Count(string(tc.Arguments))
		}
		if m.ToolResult != nil {
			total += counter.Count(m.ToolResult.Output)
		}
	}
	return total
}

// WithMargin inflates an estimate by margin (0.10 for the 11.3/11.4
// thresholds) so callers compare conservatively.
func WithMargin(tokens int, margin float64) int {
	return int(math.Ceil(float64(tokens) * (1 + margin)))
}

// DefaultCharsPerToken is the seed factor before calibration.
const DefaultCharsPerToken = 4.0

// calibrateAlpha is the moving-average weight of the previous factor
// when a new usage sample arrives.
const calibrateAlpha = 0.8

// Estimator is the character-based fallback counter of 11.7: when no
// provider tokenizer is available locally, tokens are estimated from
// characters and the factor is calibrated with the usage every
// response returns. One Estimator is safe for concurrent use.
type Estimator struct {
	mu      sync.RWMutex
	deflt   float64
	byModel map[string]float64
	samples map[string]int
}

// NewEstimator returns an Estimator seeded with charsPerToken. A
// non-positive seed selects DefaultCharsPerToken.
func NewEstimator(charsPerToken float64) *Estimator {
	if charsPerToken <= 0 {
		charsPerToken = DefaultCharsPerToken
	}
	return &Estimator{
		deflt:   charsPerToken,
		byModel: make(map[string]float64),
		samples: make(map[string]int),
	}
}

// factorFor returns the current factor for model under a read lock;
// callers must not mutate the result.
func (e *Estimator) factorFor(model string) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if model != "" {
		if f, ok := e.byModel[model]; ok {
			return f
		}
	}
	return e.deflt
}

// Estimate counts tokens in text with the default factor.
func (e *Estimator) Estimate(text string) int {
	return e.EstimateForModel("", text)
}

// EstimateForModel counts tokens in text with the model factor.
func (e *Estimator) EstimateForModel(model, text string) int {
	f := e.factorFor(model)
	if f <= 0 {
		f = DefaultCharsPerToken
	}
	return int(math.Ceil(float64(len(text)) / f))
}

// Count implements Counter with the default factor.
func (e *Estimator) Count(text string) int { return e.Estimate(text) }

// Observe folds one usage sample back into the model factor: the
// observed chars-per-token blends into the running factor with a
// moving average, so the error shrinks toward the 10% band of 11.7.
// Non-positive actual totals are ignored.
func (e *Estimator) Observe(model string, chars int, actualTokens int64) {
	if chars <= 0 || actualTokens <= 0 || model == "" {
		return
	}
	observed := float64(chars) / float64(actualTokens)
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, ok := e.byModel[model]
	if !ok {
		prev = e.deflt
	}
	e.byModel[model] = calibrateAlpha*prev + (1-calibrateAlpha)*observed
	e.samples[model]++
}

// Factor returns the current chars-per-token factor for model, and
// how many usage samples shaped it.
func (e *Estimator) Factor(model string) (float64, int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if f, ok := e.byModel[model]; ok {
		return f, e.samples[model]
	}
	return e.deflt, 0
}

// Tokenizer is an exact provider tokenizer available locally. It
// plugs into CounterForModel when one exists; see the decision
// record on why no network-fetching tokenizer is bundled
// (docs/decisions/0003-ctxcompiler-tokenizer.md).
type Tokenizer interface {
	Counter
	// Model returns the model the encoding was resolved for.
	Model() string
}

// CounterForModel returns tok when it counts for model, otherwise
// est. A nil est selects a fresh default Estimator. Callers pass the
// provider tokenizer where one is available locally without network
// (11.7, OpenAI family); everywhere else the calibrated estimator
// applies.
func CounterForModel(model string, tok Tokenizer, est *Estimator) Counter {
	if tok != nil && (model == "" || tok.Model() == model) {
		return tok
	}
	if est == nil {
		return NewEstimator(0)
	}
	return est
}
