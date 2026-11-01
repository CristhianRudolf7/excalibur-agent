package engine

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

// Embedded context-control switches for the full-window policy
// (owner decision 0051). Nil means enabled with the environment
// overlay (default ON); explicit false disables for ablations.

func (b *DefaultBuilder) maskEnabled() bool {
	if b.EnableMask != nil {
		return *b.EnableMask
	}
	return ctxcompiler.MaskEnabled(nil)
}

func (b *DefaultBuilder) compactEnabled() bool {
	if b.EnableCompact != nil {
		return *b.EnableCompact
	}
	return ctxcompiler.CompactEnabled(nil)
}

// applyContextControl runs observation masking (11.3) over the
// combined snapshot+history activity before the first compile and
// persists the masked history back to the run. snapshotLen is the
// snapshot-owned prefix length; historyLen is the run-history suffix
// length at build time. Only the activity changes; L0-L2 never move.
func (b *DefaultBuilder) applyContextControl(ctx context.Context, run *Run, snap *ctxcompiler.Snapshot, snapshotLen, historyLen int, model string, cfg ctxcompiler.Config, counter ctxcompiler.Counter) {
	if !b.maskEnabled() {
		return
	}
	if len(snap.Activity) == 0 {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	rev, batches, _, _ := run.compactionState()
	ctrl := ctxcompiler.NewController(cfg, counter, model)
	ctrl.Seed(rev, batches)
	if existing := run.compactionBlock(); existing != "" {
		ctrl.SetBlock(existing)
	}
	var pinned []string
	if b.Pins != nil {
		pinned = b.Pins()
	}
	outcome := ctrl.ApplyMask(snap.Activity, pinned, false)
	if !outcome.Applied {
		return
	}
	b.persistMaskedHistory(run, snapshotLen, historyLen, snap.Activity)
	run.recordMask()
	if b.OnMask != nil {
		b.OnMask(outcome.Batch, outcome.Dropped, "engine masking")
	}
}

// persistMaskedHistory writes the masked combined activity back to
// the run history: the history suffix (last historyLen entries of the
// mutated combined list) replaces the run history. The snapshot
// prefix is owned by the database and is never written back.
func (b *DefaultBuilder) persistMaskedHistory(run *Run, snapshotLen, historyLen int, combined []ctxcompiler.ActivityEvent) {
	if historyLen <= 0 {
		return
	}
	if len(combined) < historyLen {
		return
	}
	maskedHist := append([]ctxcompiler.ActivityEvent(nil), combined[len(combined)-historyLen:]...)
	run.persistMaskedHistory(maskedHist)
}

// persistCompactedHistory writes the compacted combined activity back
// to the run history, keeping only the history-owned suffix. The
// kept tail is a suffix of the pre-compaction combined list, so its
// history portion is its last up-to-historyLen entries.
func (b *DefaultBuilder) persistCompactedHistory(run *Run, snapshotLen int, kept []ctxcompiler.ActivityEvent) {
	run.mu.Lock()
	defer run.mu.Unlock()
	prevLen := len(run.History)
	if prevLen == 0 {
		return
	}
	if len(kept) >= prevLen {
		run.History = append([]ctxcompiler.ActivityEvent(nil), kept[len(kept)-prevLen:]...)
		return
	}
	// Kept tail shorter than the previous history: the whole kept
	// tail is history-owned in the embedded path (snapshot prefix
	// empty or fully discarded).
	run.History = append([]ctxcompiler.ActivityEvent(nil), kept...)
}
