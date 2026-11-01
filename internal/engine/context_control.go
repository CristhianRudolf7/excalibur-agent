package engine

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

// Context control helpers for the full-window policy (Section 11,
// owner decision 0051): monotonic history sequence numbers across
// compactions plus the run-scoped masking/compaction state behind
// DefaultBuilder. Only the activity history and the compaction block
// ever change here; L0-L2 stay byte-identical so the provider prefix
// cache never churns except at a batch/compaction boundary.

// nextHistorySeq returns one past the maximum history Seq, or 0 when
// empty. Callers hold no lock.
func (r *Run) nextHistorySeq() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextHistorySeqLocked()
}

// nextHistorySeqLocked is nextHistorySeq with the run lock held.
func (r *Run) nextHistorySeqLocked() int64 {
	var max int64 = -1
	for _, e := range r.History {
		if e.Seq > max {
			max = e.Seq
		}
	}
	return max + 1
}

// compactionState returns the persisted masking/compaction counters
// and block. Callers hold no lock.
func (r *Run) compactionState() (revision, batches, compactions int, block string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.CompactionRevision, r.MaskBatches, r.Compactions, r.CompactionBlock
}

// recordMask persists one masking batch count. Callers hold no lock.
func (r *Run) recordMask() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.MaskBatches++
	return r.MaskBatches
}

// recordCompaction persists one compaction block. Callers hold no
// lock. History is replaced by the kept tail so future steps stay
// under the trigger; Seq numbers ride along unchanged and future
// appends continue past the maximum via nextHistorySeq.
func (r *Run) recordCompaction(block string, kept []ctxcompiler.ActivityEvent) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.CompactionRevision++
	r.Compactions++
	r.CompactionBlock = block
	r.History = append([]ctxcompiler.ActivityEvent(nil), kept...)
	return r.CompactionRevision
}

// persistMaskedHistory replaces the history with its masked form
// (same length and order, tool outputs replaced by markers).
// Callers hold no lock.
func (r *Run) persistMaskedHistory(masked []ctxcompiler.ActivityEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.History = append([]ctxcompiler.ActivityEvent(nil), masked...)
}

// compactionBlock returns the persisted block. Callers hold no lock.
func (r *Run) compactionBlock() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.CompactionBlock
}
