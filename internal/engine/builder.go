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
}

// BuildRequest implements RequestBuilder.
func (b *DefaultBuilder) BuildRequest(ctx context.Context, run *Run, closing bool) (providers.ChatRequest, ctxcompiler.Compiled, error) {
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
	counter := b.Counter
	if counter == nil {
		counter = ctxcompiler.NewEstimator(0)
	}
	compiled, err := ctxcompiler.Compile(b.Config, snap, counter)
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
