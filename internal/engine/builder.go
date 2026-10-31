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
	// the run sets it; reasoning effort handling is untouched.
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
}

// BuildRequest implements RequestBuilder.
func (b *DefaultBuilder) BuildRequest(ctx context.Context, run *Run, closing bool) (providers.ChatRequest, ctxcompiler.Compiled, error) {
	q := effectiveQuality(b.Quality)
	snap, err := b.Snapshot(ctx, run)
	if err != nil {
		return providers.ChatRequest{}, ctxcompiler.Compiled{}, err
	}
	snap.Activity = append(append([]ctxcompiler.ActivityEvent(nil), snap.Activity...), run.historyCopy()...)
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
	compiled, err := ctxcompiler.Compile(cfg, snap, counter)
	if err != nil {
		return providers.ChatRequest{}, ctxcompiler.Compiled{}, err
	}
	toolChoice := ""
	if closing {
		toolChoice = "none"
	}
	var defs []providers.ToolDefinition
	if b.ToolDefs != nil {
		defs = b.ToolDefs(run)
	}
	req := compiled.ToChatRequest("", defs, b.MaxTokens, b.ReasoningEffort, toolChoice)
	compiled.AppendActivityMessages(&req, snap.Activity)
	return req, compiled, nil
}

func (r *Run) historyCopy() []ctxcompiler.ActivityEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ctxcompiler.ActivityEvent(nil), r.History...)
}
