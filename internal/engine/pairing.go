package engine

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Orphan-pair repair and single retry (Section 19.2 strict wires).
//
// A strict wire rejects a request whose call/result pairs are split
// (Responses 400 "No function call found ...", or the Chat Completions
// mirror). Masking, compaction and history trimming keep pairs
// atomically and every built request is validated before it reaches
// the wire, so this path only fires for history that was already
// split upstream (for example a resumed journal that predates the
// invariant). The repair converts the orphan halves in the run history
// to plain-text summaries, rebuilds the request and retries the model
// call exactly once; a second rejection fails the run as before.

// pairRetryEnabled reports whether the orphan-400 repair-and-retry
// applies. A Deps override wins; otherwise the
// EXCALIBUR_CONTEXT_PAIR_RETRY overlay selects the default (ON).
func (e *Engine) pairRetryEnabled() bool {
	if e.deps.PairRetry != nil {
		return *e.deps.PairRetry
	}
	return ctxcompiler.PairRetryEnabled(nil)
}

// orphanRetry carries a successful single retry back to the step loop.
type orphanRetry struct {
	resp     providers.ChatResponse
	meta     models.CallMeta
	compiled ctxcompiler.Compiled
	err      error
}

// retryOrphanPairs repairs the run history and retries the model call
// once when firstErr is a strict-wire orphan rejection. It reports
// false when the error is not an orphan rejection, the switch is off,
// the history holds no orphans, the rebuild fails or the retry fails:
// the caller handles a rebuild or retry error when present, otherwise
// the original error.
func (e *Engine) retryOrphanPairs(ctx context.Context, run *Run, builderClosing bool, firstErr error) (orphanRetry, bool) {
	if !e.pairRetryEnabled() {
		return orphanRetry{}, false
	}
	if !ctxcompiler.IsOrphanPairError(firstErr) {
		return orphanRetry{}, false
	}
	repaired := run.repairHistoryPairs()
	req, compiled, err := e.deps.Context.BuildRequest(ctx, run, builderClosing)
	if err != nil {
		return orphanRetry{err: err}, false
	}
	// A provider can reject a pair that appears complete internally (for
	// example, snapshot activity or a provider-side ordering constraint).
	// In that case use plain text for all tool exchanges in this one retry.
	// No original call/output items survive to repeat the same rejection.
	if repaired == 0 {
		var messages []providers.Message
		for _, message := range req.Messages {
			converted, report := ctxcompiler.RepairMessages([]providers.Message{message})
			messages = append(messages, converted...)
			repaired += report.Repaired
		}
		if repaired == 0 {
			return orphanRetry{}, false
		}
		req.Messages = messages
		run.recordPairRepair(repaired)
	}
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.ModelRequest(ctx, journal.HashRequest(prefixHash(compiled)))
	}
	run.SetWaitingModel(true)
	resp, meta, err := e.deps.Models.Chat(WithModelCallSite(ctx, "engine.step"), run.Profile.Role, req)
	run.SetWaitingModel(false)
	run.recordPairRetry()
	if e.deps.OnPairRetry != nil {
		e.deps.OnPairRetry(repaired, firstErr.Error())
	}
	if err != nil {
		return orphanRetry{err: err}, false
	}
	return orphanRetry{resp: resp, meta: meta, compiled: compiled}, true
}
