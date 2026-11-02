package engine

import (
	"context"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const timeReserveNote = "time reserve: finish now with what works; refusals are advisory from here"

// Only autonomous explicit exec/eval finishes spend a delivery reserve.
// In particular, this uses wall time even when all step budgets are disabled.
func (e *Engine) inTimeReserve(run *Run, now time.Time, percent int) bool {
	if !switchOn("EXCALIBUR_FINISH_TIME_RESERVE") || !e.explicitFinishRequired(run) {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	total := time.Duration(run.Tracker.Limit.MaxSeconds) * time.Second
	return total > 0 && !run.StartedAt.IsZero() && now.Sub(run.StartedAt) >= total*time.Duration(percent)/100
}

func (e *Engine) announceTimeReserve(run *Run, now time.Time) {
	if !e.inTimeReserve(run, now, 70) {
		return
	}
	run.mu.Lock()
	noted := run.fin.reserveNoted
	run.fin.reserveNoted = true
	run.mu.Unlock()
	if !noted {
		run.InjectNote(timeReserveNote)
	}
}

func (e *Engine) completeReservedFinish(ctx context.Context, run *Run, args FinishArgs) error {
	run.storeFinish(args, "")
	run.clearRefusal()
	run.mu.Lock()
	from := run.state
	err := run.transition(types.RunVerifying)
	run.mu.Unlock()
	if err != nil {
		return err
	}
	const reason = "time reserve: accepted finish at 85% of wall budget"
	e.recordState(ctx, run, from, types.RunVerifying, reason)
	return e.complete(ctx, run, types.RunCompleted, reason)
}
