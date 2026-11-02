// Default request builder: snapshot to provider call (11.1, 19.2).
//
// SnapshotFunc loads the database-side snapshot (memories, skills,
// tools, task deltas); DefaultBuilder compiles it deterministically,
// folds the run history and volatile notes in, and projects the
// provider request with the exact-match prefix order tools, system
// (L0-L3), messages, volatile tail (11.6). In closing mode the tool
// definitions stay in the request and only tool_choice flips to
// none, so the prefix hash does not move (7.3).
package engine

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// SnapshotFunc loads the compiler snapshot for a run.
type SnapshotFunc func(ctx context.Context, run *Run) (ctxcompiler.Snapshot, error)

// DefaultBuilder is the production RequestBuilder.
type DefaultBuilder struct {
	Config  ctxcompiler.Config
	Counter ctxcompiler.Counter
	// Snapshot loads the run snapshot; Activity may be empty, the
	// builder appends the run history itself.
	Snapshot SnapshotFunc
	// ToolDefs lists the eligible provider tool definitions.
	ToolDefs func(run *Run) []providers.ToolDefinition
	// MaxTokens caps the completion; ReasoningEffort is fixed per
	// role so the cache never breaks (19.2).
	MaxTokens       int
	ReasoningEffort string
	// ModelID is the resolved model id for family adaptation
	// (agent quality): it only selects the L0 prompt variant. Empty
	// means the tuned GPT default. Whoever resolves the model for
	// the run sets it; reasoning effort handling is untouched. It
	// also selects the full-window limits row (owner decision
	// 0051): empty means the conservative fallback.
	ModelID string
	// Quality gates the agent-quality prompt mechanisms (working
	// method, family adaptation, working notes). Nil means
	// production defaults with the EXCALIBUR_Q_* overlay.
	Quality *QualityConfig
	// Budget, when non-nil, fills Volatile.BudgetState with the
	// compact remaining-budget line (18.4, L5) whenever the snapshot
	// leaves it empty. A snapshot-carried value always wins, so
	// callers can pin or test the line without the hook.
	Budget func(run *Run) string
	// ModelCeiling optionally lowers the compaction trigger for a
	// degraded model (11.4). Zero selects the configured ceiling.
	ModelCeiling int
	// EnableMask gates embedded observation masking (11.3). Nil
	// means enabled with the EXCALIBUR_CONTEXT_MASK overlay
	// (default ON); explicit false disables.
	EnableMask *bool
	// EnableCompact gates embedded compaction (11.4). Nil means
	// enabled with the EXCALIBUR_CONTEXT_COMPACT overlay (default
	// ON); explicit false disables.
	EnableCompact *bool
	// PairCheck gates the pre-request call/result pairing invariant
	// check (Section 19.2 strict wires). Nil means enabled with the
	// EXCALIBUR_CONTEXT_PAIR_CHECK overlay (default ON); explicit
	// false disables for ablations. When on, a request carrying an
	// orphan function_call_output (or a call with no result) is
	// repaired to plain-text summaries before it reaches the wire.
	PairCheck *bool
	// Pins returns pinned observation texts that masking must keep
	// literal (pin_observation). Nil means no pins.
	Pins func() []string
	// OnMask records one masking batch (trial recorder, journal).
	// Nil skips recording; masking still applies.
	OnMask func(batch, dropped int, note string)
	// OnCompact records one compaction (trial recorder, journal).
	// Nil skips recording; compaction still applies.
	OnCompact func(from, to, kept int, note string)
	// OnPairRepair records one pre-request pairing repair (trial
	// recorder). Nil skips recording; the repair still applies.
	OnPairRepair func(repaired int, note string)
}

// BuildRequest implements RequestBuilder.
func (b *DefaultBuilder) BuildRequest(ctx context.Context, run *Run, closing bool) (providers.ChatRequest, ctxcompiler.Compiled, error) {
	q := effectiveQuality(b.Quality)
	snap, err := b.Snapshot(ctx, run)
	if err != nil {
		return providers.ChatRequest{}, ctxcompiler.Compiled{}, err
	}
	promoteSnapshotDirectives(run, &snap)
	if run.earlyDeliverablesEnabled() {
		guidance := earlyDeliverablesGuidance
		if switchOn(tools.RuntimeProjectionSwitch) {
			guidance += " " + runtimeSizingGuidance
		}
		snap.Task.FixedInstructions = append(append([]string(nil), snap.Task.FixedInstructions...), guidance)
	}
	snapshotLen := len(snap.Activity)
	hist := run.historyCopy()
	snap.Activity = append(append([]ctxcompiler.ActivityEvent(nil), snap.Activity...), hist...)
	notes := run.VolatileNotes()
	if run.RecoveryNote != "" {
		notes = append([]string{run.RecoveryNote}, notes...)
	}
	if len(notes) > 0 {
		snap.Volatile.GateReports = append(append([]string(nil), snap.Volatile.GateReports...), notes...)
	}
	// Agent quality: family adaptation from the resolved model id,
	// and the pinned working notes into L3. Both are switch-gated
	// and leave the cache-stable prefix alone inside a run.
	if snap.ModelID == "" {
		snap.ModelID = b.ModelID
	}
	if q.WorkingNotes {
		if pinned := run.notesCopy(); len(pinned) > 0 {
			snap.Task.WorkingNotes = append(append([]string(nil), snap.Task.WorkingNotes...), pinned...)
		}
	}
	if snap.Volatile.BudgetState == "" && b.Budget != nil {
		snap.Volatile.BudgetState = b.Budget(run)
	}
	counter := b.Counter
	if counter == nil {
		counter = ctxcompiler.NewEstimator(0)
	}
	cfg := b.Config
	if !q.WorkingMethod {
		cfg.MethodPrompt = false
	}
	if !q.FamilyAdapt {
		cfg.FamilyAdapt = false
	}
	if !q.WorkingNotes {
		cfg.WorkingNotesMax = 0
	}
	// Full-window context control (11.3-11.4, owner decision 0051):
	// masking and compaction run inside BuildRequest so every step
	// of the embedded exec/eval path actually invokes them (the
	// threshold helpers were previously test-only). Only the
	// activity history and the L3 compaction block change; L0-L2
	// stay byte-identical so the provider prefix cache never
	// churns except at a batch/compaction boundary.
	model := snap.ModelID
	if model == "" {
		model = b.ModelID
	}
	if existing := run.compactionBlock(); existing != "" && snap.CompactionBlock == "" {
		snap.CompactionBlock = existing
	}
	b.applyContextControl(ctx, run, &snap, snapshotLen, len(hist), model, cfg, counter)
	compiled, err := ctxcompiler.Compile(cfg, snap, counter)
	if err != nil {
		return providers.ChatRequest{}, ctxcompiler.Compiled{}, err
	}
	// Compaction after the first compile: the estimate is the real
	// compiled total, so the request never exceeds the model's max
	// input. A second compile projects the compacted request.
	if b.compactEnabled() {
		if ctxcompiler.ShouldCompactForModelName(compiled.TotalTokens, model, 0, cfg, b.ModelCeiling) {
			state := ctxcompiler.StateFromTask(snap.Task)
			if switchOn("EXCALIBUR_CONTROL_TAIL") {
				state.FixedInstructions = append(state.FixedInstructions, run.literalDirectivesCopy()...)
			}
			ctrl := ctxcompiler.NewController(cfg, counter, model)
			rev, batches, _, _ := run.compactionState()
			ctrl.Seed(rev, batches)
			if existing := run.compactionBlock(); existing != "" {
				ctrl.SetBlock(existing)
			}
			outcome, kept, cerr := ctrl.ApplyCompact(ctx, state, snap.Activity, compiled.TotalTokens)
			if cerr == nil && outcome.Applied {
				snap.Activity = kept
				snap.CompactionBlock = outcome.Block
				b.persistCompactedHistory(run, snapshotLen, kept)
				run.recordCompaction(outcome.Block, kept)
				if b.OnCompact != nil {
					b.OnCompact(int(outcome.FromSeq), int(outcome.ToSeq), outcome.KeptEvents, "engine compaction")
				}
				compiled, err = ctxcompiler.Compile(cfg, snap, counter)
				if err != nil {
					return providers.ChatRequest{}, ctxcompiler.Compiled{}, err
				}
			}
		}
	}
	toolChoice := ""
	if closing {
		toolChoice = "none"
	}
	var defs []providers.ToolDefinition
	if b.ToolDefs != nil {
		defs = append([]providers.ToolDefinition(nil), b.ToolDefs(run)...)
	}
	// Muse tool-description variant (agent quality): the Meta Model
	// API discipline (exact JSON arguments in call order, finish
	// only via the finish tool) rides the provider tool definitions
	// for Muse runs. Every other family — including the GPT
	// reference — keeps byte-identical definitions, and adapt-off
	// keeps every family identical.
	if q.FamilyAdapt && ctxcompiler.DetectFamily(snap.ModelID).Default() == ctxcompiler.PromptMuse {
		for i := range defs {
			defs[i].Description = ctxcompiler.AdaptToolDescription(ctxcompiler.PromptMuse, defs[i].Name, defs[i].Description)
		}
	}
	if err := b.stabilizePrefix(run, &snap, &compiled, &defs, cfg, counter); err != nil {
		return providers.ChatRequest{}, ctxcompiler.Compiled{}, err
	}
	boundControlLayer(&compiled, counter, run, snap.Volatile)
	req := compiled.ToChatRequest("", defs, b.MaxTokens, b.ReasoningEffort, toolChoice)
	compiled.AppendActivityMessages(&req, snap.Activity)
	// Pairing invariant before every model request: strict wires
	// reject a split call/result pair with a 400, so a request
	// carrying an orphan half is repaired to plain-text summaries
	// here (the run history itself is untouched; only the 400 retry
	// path rewrites history).
	if b.pairCheckEnabled() {
		if rep := ctxcompiler.ValidateMessages(req.Messages); !rep.Valid() {
			req.Messages, rep = ctxcompiler.RepairMessages(req.Messages)
			run.recordPairRepair(rep.Repaired)
			if b.OnPairRepair != nil {
				b.OnPairRepair(rep.Repaired, rep.Note())
			}
		}
	}
	return req, compiled, nil
}

func (r *Run) historyCopy() []ctxcompiler.ActivityEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ctxcompiler.ActivityEvent(nil), r.History...)
}
