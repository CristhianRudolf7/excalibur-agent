package orchestrator

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Resource management of Section 5.2: the RAM watchdog stops
// admitting runs, sheds load and resumes with hysteresis. The
// orchestrator never pauses master turns or the fast lane for this
// cause; those are small in memory and live outside runs.
//
// Sequence over the threshold: stop admitting, then pause the
// lowest-priority running execution at its next safe point (P3 first,
// then P2, then P1), and only as a last resort destroy the largest
// tool process still alive in a watchdog-paused run through
// Hooks.KillTool. Recovery: while RSS stays below 80% of the
// threshold for 60 s, admit again and resume watchdog-paused runs one
// by one by priority; runs the user paused stay paused.

// SetRAMThreshold sets the RSS bytes that trip the watchdog. Zero
// disables sampling-driven pauses (admission still applies).
func (o *Orchestrator) SetRAMThreshold(threshold uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.resThreshold = threshold
}

// watchdogLocked samples RSS and applies the Section 5.2 sequence.
func (o *Orchestrator) watchdogLocked() {
	if o.hooks.Sampler == nil {
		return
	}
	rss, err := o.hooks.Sampler()
	if err != nil {
		return
	}
	o.watchdogTickLocked(rss, o.cfg.Clock().Unix())
}

// WatchdogTick applies one watchdog sample at unix time nowSecs. It
// returns the runs it paused and, as a last resort, the run whose
// tool process it asked to kill.
func (o *Orchestrator) WatchdogTick(rss uint64, nowSecs int64) (paused []types.RunID, killed string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.watchdogTickLocked(rss, nowSecs)
}

func (o *Orchestrator) watchdogTickLocked(rss uint64, nowSecs int64) (paused []types.RunID, killed string) {
	threshold := o.resThreshold
	if threshold == 0 {
		return nil, ""
	}
	hystFrac := o.cfg.HysteresisFrac
	if hystFrac <= 0 || hystFrac >= 1 {
		hystFrac = 0.8
	}
	hyst := uint64(float64(threshold) * hystFrac)
	hystSecs := o.cfg.HysteresisSecs
	if hystSecs <= 0 {
		hystSecs = 60
	}
	switch {
	case rss >= threshold:
		o.haltAdmit = true
		o.lowSince = time.Time{}
		if p := o.shedLocked(); p != "" {
			return []types.RunID{p}, ""
		}
		// Nothing left to pause: the last resort destroys the
		// largest tool process still alive in a resource-paused
		// run, lowest priority first. The tool result becomes an
		// out-of-memory error.
		if o.hooks.KillTool != nil {
			if victim := o.liveToolLocked(); victim != "" {
				if what, err := o.hooks.KillTool(victim); err == nil && what != "" {
					o.emit(Event{Type: EventNotice, RunID: victim, Detail: "killed by memory watchdog: " + what})
					return nil, string(victim)
				}
			}
		}
		return nil, ""
	case rss < hyst:
		if o.lowSince.IsZero() {
			o.lowSince = time.Unix(nowSecs, 0).UTC()
		}
		if nowSecs-o.lowSince.Unix() >= hystSecs {
			o.haltAdmit = false
			if r := o.resumeOneResourceLocked(); r != "" {
				return []types.RunID{r}, ""
			}
		}
		return nil, ""
	default:
		o.lowSince = time.Time{}
		return nil, ""
	}
}

// watchdogOrderID marks pauses the watchdog ordered: the cascade
// keeps reason parent (Section 15.8) but shares this order id, so
// resume and the last-resort kill recognize their own subtree.
const watchdogOrderID = "watchdog"

// shedLocked pauses the lowest-priority running execution: P3 first,
// then P2, then P1, oldest heartbeat within a lane. It returns the
// paused run, if any.
func (o *Orchestrator) shedLocked() types.RunID {
	for _, lane := range []Lane{LaneP3, LaneP2, LaneP1} {
		var victim *RunRecord
		for _, rec := range o.store.RunsByState(types.RunRunning) {
			if rec.Lane != lane {
				continue
			}
			if victim == nil || rec.Run.HeartbeatAt.Before(victim.Run.HeartbeatAt) {
				r := rec
				victim = &r
			}
		}
		if victim != nil {
			o.pauseLocked(victim.Run.ID, types.PauseResource, watchdogOrderID, "memory pressure")
			return victim.Run.ID
		}
	}
	return ""
}

// liveToolLocked returns the lowest-priority watchdog-paused run
// whose tool is still alive: the last-resort kill target. Runs the
// user paused are never targets, even under pressure.
func (o *Orchestrator) liveToolLocked() types.RunID {
	for _, lane := range []Lane{LaneP3, LaneP2, LaneP1} {
		var cand *RunRecord
		for _, rec := range o.store.RunsByState(types.RunPaused) {
			if rec.Lane != lane {
				continue
			}
			if !o.watchdogPaused(rec.Run) {
				continue
			}
			runner, ok := o.runners.Lookup(rec.Run.ID)
			if !ok || !runner.Snapshot().HasTool {
				continue
			}
			if cand == nil || rec.Run.UpdatedAt.Before(cand.Run.UpdatedAt) {
				r := rec
				cand = &r
			}
		}
		if cand != nil {
			return cand.Run.ID
		}
	}
	return ""
}

// watchdogPaused reports whether the watchdog owns a paused run: a
// direct resource pause or a cascade of a watchdog order.
func (o *Orchestrator) watchdogPaused(run types.Run) bool {
	if run.PauseReason == nil {
		return false
	}
	if *run.PauseReason == types.PauseResource && run.PausedBy == watchdogOrderID {
		return true
	}
	return *run.PauseReason == types.PauseParent && run.PausedBy == watchdogOrderID
}

// resumeOneResourceLocked resumes one resource-paused run by priority
// (P1 first). Runs the user paused are never resumed here.
func (o *Orchestrator) resumeOneResourceLocked() types.RunID {
	for _, lane := range []Lane{LaneP1, LaneP2, LaneP3} {
		var cand *RunRecord
		for _, rec := range o.store.RunsByState(types.RunPaused) {
			if rec.Lane != lane {
				continue
			}
			// Direct watchdog targets only: the resume cascades to
			// the order subtree (Section 15.8).
			if rec.Run.PauseReason == nil || *rec.Run.PauseReason != types.PauseResource ||
				rec.Run.PausedBy != watchdogOrderID {
				continue
			}
			if cand == nil || rec.Run.UpdatedAt.Before(cand.Run.UpdatedAt) {
				r := rec
				cand = &r
			}
		}
		if cand != nil {
			o.resumeLocked(cand.Run.ID, watchdogOrderID)
			return cand.Run.ID
		}
	}
	return ""
}
