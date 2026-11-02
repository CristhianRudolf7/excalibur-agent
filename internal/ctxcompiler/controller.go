package ctxcompiler

import (
	"context"
	"sort"
	"strings"
)

// Embedded context control: observation masking (11.3) plus
// full-window compaction (11.4, owner decision 0051) for the
// exec/eval single-run path.
//
// The controller only ever touches the activity history and the
// compaction block. L0-L2 are never rewritten here, so the provider
// prefix cache stays stable inside a run: masking breaks it once per
// batch and compaction once per compaction, exactly as Sections
// 11.3-11.4 prescribe.

// MaskOutcome is one masking application.
type MaskOutcome struct {
	// Applied reports whether a batch masked anything.
	Applied bool
	// MaskedIDs are the masked call ids.
	MaskedIDs []string
	// Batch counts this masking batch (1-based per controller).
	Batch int
	// Dropped is the token cost removed from the literal context.
	Dropped int
	// Remaining is the unmasked total after the batch.
	Remaining int
}

// CompactOutcome is one compaction application.
type CompactOutcome struct {
	// Applied reports whether a compaction ran.
	Applied bool
	// Revision numbers the compaction.
	Revision int
	// FromSeq and ToSeq bound the discarded span.
	FromSeq int64
	ToSeq   int64
	// KeptEvents counts the literal tail kept.
	KeptEvents int
	// Block is the re-render material (structured state + summary).
	Block string
}

// Controller carries the per-run masking/compaction state for the
// embedded path. One Controller per run; it is not safe for
// concurrent use.
type Controller struct {
	cfg      Config
	counter  Counter
	model    string
	limits   ModelLimits
	masker   *Masker
	batches  int
	revision int
	block    string
}

// NewController builds the per-run controller for model. A nil
// counter selects a default Estimator.
func NewController(cfg Config, counter Counter, model string) *Controller {
	if counter == nil {
		counter = NewEstimator(0)
	}
	return &Controller{
		cfg:     cfg,
		counter: counter,
		model:   model,
		limits:  LimitsForModel(model, cfg),
		masker:  NewMasker(cfg, counter),
	}
}

// Model returns the controller model id.
func (c *Controller) Model() string { return c.model }

// Seed restores the per-run counters (revision, masking batches)
// loaded from the run before applying control.
func (c *Controller) Seed(revision, batches int) {
	c.revision = revision
	c.batches = batches
}

// SetBlock restores the latest compaction block loaded from the run.
func (c *Controller) SetBlock(block string) { c.block = block }

// Limits returns the effective model limits.
func (c *Controller) Limits() ModelLimits { return c.limits }

// Threshold returns the compaction trigger for the controller model.
func (c *Controller) Threshold() int {
	return ThresholdForModel(c.model, 0, c.cfg, 0)
}

// Block returns the latest compaction block, "" when none ran.
func (c *Controller) Block() string { return c.block }

// Revision returns the compaction count.
func (c *Controller) Revision() int { return c.revision }

// Batches returns the masking batch count.
func (c *Controller) Batches() int { return c.batches }

// observationsOf projects tool-result activity into maskable
// observations. Pinned texts (pin_observation) mark an observation
// pinned when either side contains the other; errors stay literal
// through the Masker protection rule.
func observationsOf(activity []ActivityEvent, pinned []string) []Observation {
	var out []Observation
	for _, e := range activity {
		if e.ToolResult == nil {
			continue
		}
		body := e.ToolResult.Output
		if body == "" {
			body = e.Content
		}
		obs := Observation{
			Mask:     e.Mask,
			CallID:   e.ToolResult.CallID,
			ToolName: e.ToolResult.Name,
			Label:    e.ToolResult.Name,
			Output:   body,
			IsError:  e.ToolResult.IsError,
		}
		for _, p := range pinned {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if strings.Contains(body, p) || strings.Contains(p, truncateForPin(body)) {
				obs.Pinned = true
				break
			}
		}
		out = append(out, obs)
	}
	return out
}

func truncateForPin(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// ApplyMask runs one masking batch over activity in place and
// returns the outcome. It returns Applied=false when the trigger is
// not reached. The L0-L2 prefix is untouched; only tool-result
// outputs become markers.
func (c *Controller) ApplyMask(activity []ActivityEvent, pinned []string, cacheExpired bool) MaskOutcome {
	obs := observationsOf(activity, pinned)
	before := c.masker.UnmaskedTotal(obs)
	plan := c.masker.Apply(obs, cacheExpired)
	masked := plan.ApplyToActivity(obs, activity)
	if len(masked) == 0 {
		return MaskOutcome{Remaining: plan.UnmaskedTokens}
	}
	c.batches++
	after := plan.UnmaskedTokens
	dropped := before - after
	if dropped < 0 {
		dropped = 0
	}
	return MaskOutcome{
		Applied:   true,
		MaskedIDs: masked,
		Batch:     c.batches,
		Dropped:   dropped,
		Remaining: after,
	}
}

// NeedsCompaction reports whether estimatedTotal trips the
// full-window trigger for the controller model.
func (c *Controller) NeedsCompaction(estimatedTotal int) bool {
	return ShouldCompactForModelName(estimatedTotal, c.model, 0, c.cfg, 0)
}

// ApplyCompact runs one state-only compaction pass when the estimate
// trips the trigger. State comes from the caller snapshot task view
// (requirements/criteria/plan preserved deterministically); the
// summary half is skipped (nil summarizer) so the builder never
// issues a model call to compact. It rewrites activity to the kept
// literal tail and returns the outcome. When the estimate is below
// the trigger it returns Applied=false with activity untouched.
func (c *Controller) ApplyCompact(ctx context.Context, state StructuredState, activity []ActivityEvent, estimatedTotal int) (CompactOutcome, []ActivityEvent, error) {
	if !c.NeedsCompaction(estimatedTotal) {
		return CompactOutcome{}, activity, nil
	}
	if len(activity) == 0 {
		return CompactOutcome{}, activity, nil
	}
	budget := c.cfg.PostCompactionLiteralTokens
	if budget <= 0 {
		budget = 20000
	}
	kept := SelectTail(activity, budget, c.counter)
	keptSet := make(map[int64]bool, len(kept))
	for _, e := range kept {
		keptSet[e.Seq] = true
	}
	var discarded []ActivityEvent
	for _, e := range activity {
		if !keptSet[e.Seq] {
			discarded = append(discarded, e)
		}
	}
	if len(discarded) == 0 {
		return CompactOutcome{}, activity, nil
	}
	c.revision++
	out, err := Compact(ctx, c.cfg, CompactInput{
		State:     state,
		Revision:  c.revision,
		Discarded: discarded,
		Tail:      activity,
		Counter:   c.counter,
	})
	if err != nil {
		c.revision--
		return CompactOutcome{}, activity, err
	}
	c.block = out.Block
	var from, to int64
	if len(discarded) > 0 {
		seqs := make([]int64, 0, len(discarded))
		for _, e := range discarded {
			seqs = append(seqs, e.Seq)
		}
		sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
		from, to = seqs[0], seqs[len(seqs)-1]
	}
	return CompactOutcome{
		Applied:    true,
		Revision:   c.revision,
		FromSeq:    from,
		ToSeq:      to,
		KeptEvents: len(out.KeptTail),
		Block:      out.Block,
	}, out.KeptTail, nil
}

// StateFromTask builds the deterministic compaction state from the
// snapshot task view: objective and original request plus the live
// requirements/criteria/plan. Callers fill commands/errors/decisions
// when they track them; the embedded path keeps them empty rather
// than inventing them.
func StateFromTask(task TaskView) StructuredState {
	return StructuredState{
		Objective:         task.OriginalRequest,
		OriginalRequest:   task.OriginalRequest,
		FixedInstructions: append([]string(nil), task.FixedInstructions...),
		Requirements:      append([]RequirementView(nil), task.Requirements...),
		Criteria:          append([]CriterionView(nil), task.Criteria...),
		Plan:              append([]PlanItemView(nil), task.Plan...),
	}
}

// EstimateActivity prices one activity list as rendered on the wire.
func (c *Controller) EstimateActivity(activity []ActivityEvent) int {
	return c.counter.Count(renderEvents(activity))
}
