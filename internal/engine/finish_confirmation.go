package engine

import (
	"context"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const confirmationReminder = "finish confirmation pending: call finish again to end the run, or change files if work remains"

type confirmationProgress struct {
	hash           string
	unchangedCalls int
	changed        bool
}

func (r *Run) confirmationFooter() string {
	if !switchOn("EXCALIBUR_FINISH_CONFIRM_PROGRESS") || !r.confirmArmed() {
		return ""
	}
	return confirmationReminder
}

func (r *Run) observeConfirmation(unchanged bool) {
	if !switchOn("EXCALIBUR_FINISH_CONFIRM_PROGRESS") {
		return
	}
	r.mu.Lock()
	if !r.finishConfirmPending {
		r.mu.Unlock()
		return
	}
	if !unchanged {
		r.confirmProgress.changed = true
		r.confirmProgress.unchangedCalls = 0
	} else {
		r.confirmProgress.unchangedCalls++
	}
	count := r.confirmProgress.unchangedCalls
	r.mu.Unlock()
	if count == 12 {
		r.InjectNote("Twelve consecutive calls changed no files after finish checks passed. End the run now: " + confirmationReminder)
	}
}

// Only explicit exec/eval finish rounds qualify. Incomplete snapshots never
// authorize automatic completion, nor does a file edit after the passed check.
func (e *Engine) completePendingConfirmation(ctx context.Context, r *Run, now time.Time) (bool, error) {
	// An incomplete optional review retains its one bounded delivery call even
	// when it consumed the ordinary confirmation reserve.
	if r.deliveryPendingReason() != "" {
		return false, nil
	}
	if r.confirmArmed() && e.reserveMayAccept(r, now, 85) {
		r.mu.Lock()
		var args *FinishArgs
		if r.Finish != nil && r.state == types.RunRunning {
			cp := *r.Finish
			args = &cp
		}
		r.mu.Unlock()
		if args != nil {
			return true, e.completeReservedFinish(ctx, r, *args)
		}
	}
	if !switchOn("EXCALIBUR_FINISH_CONFIRM_PROGRESS") || !e.explicitFinishRequired(r) || !r.confirmArmed() {
		return false, nil
	}
	r.mu.Lock()
	assumed := r.assumedWallLocked()
	r.mu.Unlock()
	if assumed || e.pendingCommandReason(r, now) != "" {
		return false, nil
	}
	hash := workspaceCheckHash(r.Workdir)
	r.mu.Lock()
	total := time.Duration(r.Tracker.Limit.MaxSeconds) * time.Second
	ready := total > 0 && now.Sub(r.StartedAt) >= total*95/100 && hash != "" && hash == r.confirmProgress.hash && !r.confirmProgress.changed && r.Finish != nil && r.state == types.RunRunning
	if !ready {
		r.mu.Unlock()
		return false, nil
	}
	args := *r.Finish
	from := r.state
	err := r.transition(types.RunVerifying)
	r.mu.Unlock()
	if err != nil {
		return false, err
	}
	r.storeFinish(args, "")
	const reason = "confirmation pending at deadline"
	r.auditFinish(now, "", "confirmation_deadline", "acceptance", reason)
	e.recordState(ctx, r, from, types.RunVerifying, reason)
	return true, e.complete(ctx, r, types.RunCompleted, reason)
}
