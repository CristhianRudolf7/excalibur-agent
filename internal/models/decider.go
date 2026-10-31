// Calibrated bounded decider for Section 19.4.
//
// Ports of use (triage, ask-or-assume, skill and tool ranking,
// memory admission, loop judge, notifications) ask bounded questions
// of three kinds: choice (one option among several), score (level of
// a rubric) and noul (probability that a statement is true). The
// decider answers values with a calibrated probability, never text.
//
// Abstention rule: p >= tauHigh acts, tauLow < p < tauHigh defers
// the call to the LLM inside the normal flow, p <= tauLow takes the
// alternative. Thresholds calibrate on the harness's own labeled
// records; vendor calibration figures are not trusted.
//
// Backends are interchangeable: a Jev API, a pplx-decider API or
// local weights behind the same HTTP shape (backends.go), or the LLM
// fallback in this file. The decider is never the final verdict on
// whether a piece of work is correct.
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Question kinds of Section 19.4.
type QuestionKind string

// Question kinds.
const (
	// KindChoice picks one option among several.
	KindChoice QuestionKind = "choice"
	// KindScore rates the level of a rubric.
	KindScore QuestionKind = "score"
	// KindNoul is the probability that a statement is true.
	KindNoul QuestionKind = "noul"
)

// Question is one bounded decider question.
type Question struct {
	Kind QuestionKind `json:"kind"`
	// Prompt is the statement or task: for noul the claim whose
	// probability is wanted; for choice/score the item to judge.
	Prompt string `json:"prompt"`
	// Options holds the choice alternatives (KindChoice) or the
	// rubric levels (KindScore). Empty for KindNoul.
	Options []string `json:"options,omitempty"`
	// Context is the minimal state the question needs (message
	// plus recent turns, candidate scopes, similarity pair).
	Context string `json:"context,omitempty"`
	// FinalVerdict must stay false: the decider never judges
	// whether a piece of work is correct (Section 19.4).
	FinalVerdict bool `json:"-"`
}

// Validate checks the question shape and rejects final-verdict use.
func (q Question) Validate() error {
	if q.FinalVerdict {
		return fmt.Errorf("models: decider never decides the final verdict on work correctness")
	}
	if q.Prompt == "" {
		return fmt.Errorf("models: decider question needs a prompt")
	}
	switch q.Kind {
	case KindNoul:
		if len(q.Options) != 0 {
			return fmt.Errorf("models: noul questions take no options")
		}
	case KindChoice:
		if len(q.Options) < 2 {
			return fmt.Errorf("models: choice questions need at least two options")
		}
	case KindScore:
		if len(q.Options) < 2 {
			return fmt.Errorf("models: score questions need at least two rubric levels")
		}
	default:
		return fmt.Errorf("models: unknown decider question kind %q", string(q.Kind))
	}
	return nil
}

// Answer is one calibrated decider answer.
type Answer struct {
	Kind QuestionKind `json:"kind"`
	// Choice holds the picked option for KindChoice.
	Choice string `json:"choice,omitempty"`
	// Score holds the rubric level index for KindScore.
	Score int `json:"score,omitempty"`
	// Prob is the calibrated probability: for noul the truth
	// probability, for choice/score the confidence in the value.
	Prob float64 `json:"probability"`
}

// Normalize clamps Prob into [0,1] and checks the value shape.
func (a Answer) Normalize(kind QuestionKind, options []string) (Answer, error) {
	if a.Prob != a.Prob {
		return Answer{}, fmt.Errorf("models: decider probability is NaN")
	}
	if a.Prob < 0 {
		a.Prob = 0
	}
	if a.Prob > 1 {
		a.Prob = 1
	}
	a.Kind = kind
	switch kind {
	case KindChoice:
		if a.Choice == "" && len(options) > 0 {
			a.Choice = options[0]
		}
		found := false
		for _, o := range options {
			if o == a.Choice {
				found = true
				break
			}
		}
		if !found && len(options) > 0 {
			return Answer{}, fmt.Errorf("models: decider choice %q not among options", a.Choice)
		}
	case KindScore:
		if a.Score < 0 {
			a.Score = 0
		}
		if len(options) > 0 && a.Score >= len(options) {
			a.Score = len(options) - 1
		}
	}
	return a, nil
}

// Thresholds holds the abstention band of Section 19.4.
type Thresholds struct {
	// Low is tau_bajo: p <= Low takes the alternative.
	Low float64 `json:"low"`
	// High is tau_alto: p >= High acts.
	High float64 `json:"high"`
}

// Outcome is the abstention decision for one probability.
type Outcome int

// Outcomes.
const (
	// OutcomeAct means p >= tauHigh: act on the answer.
	OutcomeAct Outcome = iota
	// OutcomeDefer means tauLow < p < tauHigh: defer to the LLM
	// inside the normal flow (Section 2.1, no extra call).
	OutcomeDefer
	// OutcomeAlternative means p <= tauLow: take the alternative.
	OutcomeAlternative
)

// DefaultThresholds returns the initial calibrated band. The values
// are starting points per Appendix B and calibrate with own records.
// Ports with their own documented cutoffs (ask-or-assume 0.7,
// stuck judge 0.9) keep their constants below.
func DefaultThresholds() Thresholds {
	return Thresholds{Low: 0.35, High: 0.75}
}

// AskThreshold is the Section 6.4 cutoff: below 0.7 on the main
// interpretation the harness asks instead of assuming when rework
// is costly.
const AskThreshold = 0.7

// StuckJudgeThreshold is the Section 18.3 cutoff: the stuck judge
// only counts at or above 0.9.
const StuckJudgeThreshold = 0.9

// Validate checks Low < High with both inside [0,1].
func (t Thresholds) Validate() error {
	if t.Low < 0 || t.Low > 1 || t.High < 0 || t.High > 1 {
		return fmt.Errorf("models: thresholds out of [0,1]: %+v", t)
	}
	if t.Low >= t.High {
		return fmt.Errorf("models: thresholds need low < high: %+v", t)
	}
	return nil
}

// Decide applies the abstention rule to one probability.
func Decide(p float64, t Thresholds) Outcome {
	if p >= t.High {
		return OutcomeAct
	}
	if p <= t.Low {
		return OutcomeAlternative
	}
	return OutcomeDefer
}

// Decider answers bounded questions with calibrated
// probabilities. Implementations are the Jev API, the pplx-decider
// API or local weights (backends.go), the LLM fallback below, and
// the scripted fake for tests.
type Decider interface {
	Ask(ctx context.Context, q Question) (Answer, error)
	Close() error
}

// ChatCaller is the LLM seam the fallback uses. The gateway
// implements it with the decider role; tests stub it.
type ChatCaller interface {
	Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, CallMeta, error)
}

// LLMDecider is the Section 19.4 fallback: a cheap model with a
// restricted JSON output. Probabilities come from the model's own
// JSON number, clamped into [0,1]; provider logprobs are used where
// the H1 adapters expose them, which they do not yet, so the JSON
// value is the source and the discrepancy is recorded.
type LLMDecider struct {
	caller ChatCaller
	model  string
}

// NewLLMDecider returns a fallback decider that asks the caller
// with the decider role. Model names the decisions model for
// observability; an empty model falls back to DefaultDecisionsModel.
func NewLLMDecider(caller ChatCaller, model string) *LLMDecider {
	if model == "" {
		model = DefaultDecisionsModel
	}
	return &LLMDecider{caller: caller, model: model}
}

// Model reports the configured decisions model.
func (d *LLMDecider) Model() string { return d.model }

// Close releases nothing; the caller owns its providers.
func (d *LLMDecider) Close() error { return nil }

// Ask answers one bounded question through the cheap model. The
// request carries no reasoning_effort of its own: the gateway role
// entry fixes it per role, and adapters whose contract has no such
// field drop it (Section 19.2). Final-verdict questions are
// rejected before any call.
func (d *LLMDecider) Ask(ctx context.Context, q Question) (Answer, error) {
	if err := q.Validate(); err != nil {
		return Answer{}, err
	}
	if err := ctx.Err(); err != nil {
		return Answer{}, err
	}
	prompt := llmPrompt(q)
	resp, _, err := d.caller.Chat(ctx, types.RoleDecider, providers.ChatRequest{
		Messages: []providers.Message{{Role: providers.RoleUser, Content: prompt}},
	})
	if err != nil {
		return Answer{}, err
	}
	return parseLLMAnswer(q, resp.Message.Content)
}

func llmPrompt(q Question) string {
	var b strings.Builder
	b.WriteString("Answer with a single JSON object and no other text.\n")
	switch q.Kind {
	case KindChoice:
		fmt.Fprintf(&b, "Pick one option. State: %s\nItem: %s\nOptions: %s\n",
			q.Context, q.Prompt, strings.Join(q.Options, " | "))
		b.WriteString("Schema: {\"choice\": \"<one option>\", \"probability\": 0.0-1.0}\n")
	case KindScore:
		fmt.Fprintf(&b, "Rate the level. State: %s\nItem: %s\nLevels: %s\n",
			q.Context, q.Prompt, strings.Join(q.Options, " | "))
		b.WriteString("Schema: {\"score\": <0-based level index>, \"probability\": 0.0-1.0}\n")
	default:
		fmt.Fprintf(&b, "Estimate the probability the statement is true. State: %s\nStatement: %s\n",
			q.Context, q.Prompt)
		b.WriteString("Schema: {\"probability\": 0.0-1.0}\n")
	}
	return b.String()
}

type llmAnswerJSON struct {
	Choice      string  `json:"choice"`
	Score       *int    `json:"score"`
	Probability float64 `json:"probability"`
}

func parseLLMAnswer(q Question, raw string) (Answer, error) {
	trimmed := strings.TrimSpace(raw)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end < 0 || end <= start {
		return Answer{}, fmt.Errorf("models: llm decider returned no JSON object")
	}
	var v llmAnswerJSON
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &v); err != nil {
		return Answer{}, fmt.Errorf("models: llm decider returned invalid JSON: %w", err)
	}
	ans := Answer{Kind: q.Kind, Prob: v.Probability}
	switch q.Kind {
	case KindChoice:
		ans.Choice = v.Choice
	case KindScore:
		if v.Score != nil {
			ans.Score = *v.Score
		}
	}
	return ans.Normalize(q.Kind, q.Options)
}
