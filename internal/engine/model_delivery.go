package engine

import (
	"context"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
)

const deliveryPending = "independent review deadline reached; verification is pending"

// Only autonomous exec/eval model reviews spend a delivery reserve. Computation
// tools retain their own deadlines. An assumed estimate is not a hard deadline;
// use the same extended safety horizon as the wall-budget tracker in that case.
func (e *Engine) modelDeliveryContext(ctx context.Context, run *Run, final bool) (context.Context, context.CancelFunc) {
	if !switchOn("EXCALIBUR_MODEL_DELIVERY_RESERVE") || !e.explicitFinishRequired(run) {
		return ctx, func() {}
	}
	run.mu.Lock()
	seconds, source, started := run.Tracker.Limit.MaxSeconds, run.Profile.WallSource, run.StartedAt
	run.mu.Unlock()
	remaining := time.Duration(HardWallSeconds(seconds, source))*time.Second - e.deps.Clock().Sub(started)
	hasHorizon := seconds > 0 && !started.IsZero()
	if !hasHorizon {
		remaining = 0
	}
	if deadline, ok := ctx.Deadline(); ok {
		parent := time.Until(deadline)
		if !hasHorizon || parent < remaining {
			remaining = parent
			hasHorizon = true
		}
	}
	// Without either a wall limit or a parent deadline there is no estimate to
	// spend. Do not invent an interactive time budget.
	if !hasHorizon {
		return ctx, func() {}
	}
	allowance := remaining / 2
	if final {
		allowance = remaining
	}
	// Provider latency (including retry waits on reduced-priority tiers) can
	// exceed a minute, so the caps only bite when the remaining horizon is short.
	cap := 180 * time.Second
	if final {
		cap = 90 * time.Second
	}
	if allowance > cap {
		allowance = cap
	}
	if allowance < 0 {
		allowance = 0
	}
	return context.WithTimeout(ctx, allowance)
}

func (r *Run) deliveryPendingReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !switchOn("EXCALIBUR_MODEL_DELIVERY_RESERVE") {
		return ""
	}
	return r.fin.deliveryPending
}

func (e *Engine) markDeliveryPending(ctx context.Context, run *Run, site string) {
	reason := deliveryPending
	if site == "engine.confirmation" {
		reason = "confirmation deadline reached; delivery confirmation is pending"
	}
	run.mu.Lock()
	run.fin.deliveryPending = reason
	run.mu.Unlock()
	run.InjectControlNote("finish.review_pending", reason+"; make one final delivery with available evidence")
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.Record(context.WithoutCancel(ctx), journal.EntryType("model_delivery_reserve"), journal.MarshalPayload(map[string]string{"site": site, "outcome": "unverified_pending"}), "", "", "")
	}
}
