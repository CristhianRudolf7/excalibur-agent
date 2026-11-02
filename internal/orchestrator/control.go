package orchestrator

import (
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Control orders of Section 15.8. Every order serializes on the
// orchestrator mutex in arrival order; a cancellation is definitive
// and later orders on the same run are rejected. Cascades walk the
// agent subtree bottom-up for cancel and top-down for pause.

// Pause moves a run to paused with an immediate interrupt and pauses
// its subtree in cascade. Active descendants take reason parent and
// record the order id in paused_by; resume only wakes what this order
// paused. orderID identifies the pause order (empty generates one).
func (o *Orchestrator) Pause(id types.RunID, reason types.PauseReason, orderID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if orderID == "" {
		orderID = string(types.NewMessageID())
	}
	return o.pauseLocked(id, reason, orderID, "control order")
}

func (o *Orchestrator) pauseLocked(id types.RunID, reason types.PauseReason, orderID, detail string) error {
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State.IsTerminal() {
		return fmt.Errorf("orchestrator: run %q is %q", id, rec.Run.State)
	}
	if rec.Run.State == types.RunCanceled {
		return fmt.Errorf("orchestrator: run %q already canceled", id)
	}
	if runner, ok := o.runners.Lookup(id); ok {
		runner.RequestInterrupt(false)
	}
	// Leases go first so a paused run never immobilizes files.
	if o.hooks.ReleaseLeases != nil {
		o.hooks.ReleaseLeases(id)
	}
	pausedBy := orderID
	rec.Run.PausedBy = pausedBy
	rec.Run.PauseReason = &reason
	if err := o.store.UpdateRun(rec); err != nil {
		return err
	}
	if err := o.transitionLocked(id, types.RunPaused, detail); err != nil {
		return err
	}
	o.emitReportLocked(id, "paused: "+detail)
	// Cascade: every active descendant pauses with reason parent.
	agent, err := o.store.GetAgent(rec.Run.AgentID)
	if err != nil {
		return err
	}
	for _, desc := range o.treeLocked(agent.ID)[1:] {
		drec, ok := o.parentRunLocked(desc.ID)
		if !ok || drec.Run.State.IsTerminal() || drec.Run.State == types.RunPaused {
			continue
		}
		dreason := types.PauseParent
		drec.Run.PausedBy = pausedBy
		drec.Run.PauseReason = &dreason
		_ = o.store.UpdateRun(drec)
		if runner, ok := o.runners.Lookup(drec.Run.ID); ok {
			runner.RequestInterrupt(false)
		}
		if o.hooks.ReleaseLeases != nil {
			o.hooks.ReleaseLeases(drec.Run.ID)
		}
		if err := o.transitionLocked(drec.Run.ID, types.RunPaused, "cascade from "+string(id)); err != nil {
			continue
		}
		o.emitReportLocked(drec.Run.ID, "paused by parent cascade")
	}
	o.admitLocked()
	return nil
}

// Resume returns a paused run to queued and re-wakes what the same
// pause order paused. Descendants paused by another cause stay
// paused (Section 15.8). An empty orderID resumes exactly what paused
// the target itself.
func (o *Orchestrator) Resume(id types.RunID, orderID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.resumeLocked(id, orderID)
}

func (o *Orchestrator) resumeLocked(id types.RunID, orderID string) error {
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State.IsTerminal() {
		return fmt.Errorf("orchestrator: run %q is %q", id, rec.Run.State)
	}
	if rec.Run.State != types.RunPaused {
		return fmt.Errorf("orchestrator: run %q is %q, only paused runs resume", id, rec.Run.State)
	}
	mark := orderID
	if mark == "" {
		mark = rec.Run.PausedBy
	}
	if rec.Run.PausedBy != mark {
		return fmt.Errorf("orchestrator: run %q paused by another order", id)
	}
	rec.Run.PausedBy = ""
	if err := o.store.UpdateRun(rec); err != nil {
		return err
	}
	if err := o.transitionLocked(id, types.RunQueued, "resumed; leases reacquired"); err != nil {
		return err
	}
	o.admitLocked()
	agent, err := o.store.GetAgent(rec.Run.AgentID)
	if err != nil {
		return err
	}
	for _, desc := range o.treeLocked(agent.ID)[1:] {
		drec, ok := o.parentRunLocked(desc.ID)
		if !ok || drec.Run.State != types.RunPaused || drec.Run.PausedBy != mark {
			continue
		}
		drec.Run.PausedBy = ""
		_ = o.store.UpdateRun(drec)
		if err := o.transitionLocked(drec.Run.ID, types.RunQueued, "resumed with parent"); err != nil {
			continue
		}
	}
	o.admitLocked()
	return nil
}

// Interrupt stops the in-flight model call and tool per its cancel
// class and leaves the run paused until resume or redirect. With
// toolOnly only the tool stops ("interrupted by the user") and the
// run decides how to continue (Section 15.8). Descendants keep
// working in both cases.
func (o *Orchestrator) Interrupt(id types.RunID, toolOnly bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State.IsTerminal() {
		return fmt.Errorf("orchestrator: run %q is %q", id, rec.Run.State)
	}
	runner, ok := o.runners.Lookup(id)
	if !ok {
		// Nothing in flight: a queued run just parks as paused.
		if rec.Run.State == types.RunQueued {
			reason := types.PauseUser
			rec.Run.PauseReason = &reason
			rec.Run.PausedBy = string(types.NewMessageID())
			_ = o.store.UpdateRun(rec)
			return o.transitionLocked(id, types.RunPaused, "interrupted while queued")
		}
		return fmt.Errorf("orchestrator: run %q has no attached runner", id)
	}
	runner.RequestInterrupt(toolOnly)
	if toolOnly {
		o.emit(Event{Type: EventNotice, RunID: id, Detail: "tool interrupted by the user; run decides how to continue"})
		return nil
	}
	if o.hooks.ReleaseLeases != nil {
		o.hooks.ReleaseLeases(id)
	}
	reason := types.PauseUser
	rec.Run.PauseReason = &reason
	rec.Run.PausedBy = string(types.NewMessageID())
	_ = o.store.UpdateRun(rec)
	if rec.Run.State == types.RunRunning || rec.Run.State == types.RunWaiting {
		if err := o.transitionLocked(id, types.RunPaused, "interrupted"); err != nil {
			return err
		}
	}
	o.emitReportLocked(id, "interrupted")
	return nil
}

// Cancel cancels a run and its descendants bottom-up: children first,
// then the run itself. Every canceled run records its state and
// delivers its report (Section 15.8). Cancellation is definitive.
func (o *Orchestrator) Cancel(id types.RunID) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State == types.RunCanceled {
		return fmt.Errorf("orchestrator: run %q already canceled", id)
	}
	agent, err := o.store.GetAgent(rec.Run.AgentID)
	if err != nil {
		return err
	}
	sub := o.treeLocked(agent.ID)
	// Bottom-up: deepest descendants first.
	for i := len(sub) - 1; i >= 0; i-- {
		drec, ok := o.parentRunLocked(sub[i].ID)
		if !ok || drec.Run.State.IsTerminal() {
			continue
		}
		if runner, ok := o.runners.Lookup(drec.Run.ID); ok {
			runner.RequestInterrupt(false)
		}
		if o.hooks.ReleaseLeases != nil {
			o.hooks.ReleaseLeases(drec.Run.ID)
		}
		if err := o.transitionLocked(drec.Run.ID, types.RunCanceled, "canceled"); err != nil {
			continue
		}
		o.emitReportLocked(drec.Run.ID, "canceled")
	}
	o.admitLocked()
	return nil
}

// Redirect sends an interrupt directive with new instructions; on a
// paused target it resumes with the directive pending. A normal
// directive to a paused run stays in its mailbox until resume
// (Section 15.8).
func (o *Orchestrator) Redirect(id types.RunID, text string, action DirectiveAction, interrupt bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State.IsTerminal() {
		return fmt.Errorf("orchestrator: run %q is %q", id, rec.Run.State)
	}
	priority := PriorityNormal
	if interrupt {
		priority = PriorityInterrupt
	}
	// Directives flow top-down: the sender is the target's parent, or
	// the target itself for a root the user redirects directly.
	from := rec.Run.AgentID
	if agent, err := o.store.GetAgent(rec.Run.AgentID); err == nil && agent.ParentID != nil {
		from = *agent.ParentID
	}
	if _, err := o.sendDirectiveLocked(from, rec.Run.AgentID, DirectiveAction(action), text, "", priority); err != nil {
		return err
	}
	if rec.Run.State == types.RunPaused && interrupt {
		rec.Run.PausedBy = ""
		_ = o.store.UpdateRun(rec)
		if err := o.transitionLocked(id, types.RunQueued, "redirect resumes paused run"); err != nil {
			return err
		}
		o.admitLocked()
	}
	return nil
}

// SetWaiting moves a running execution to waiting (Section 7.2).
func (o *Orchestrator) SetWaiting(id types.RunID, reason types.WaitReason) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	rec.Run.WaitReason = &reason
	if err := o.store.UpdateRun(rec); err != nil {
		return err
	}
	if err := o.transitionLocked(id, types.RunWaiting, "waiting "+string(reason)); err != nil {
		return err
	}
	o.admitLocked()
	return nil
}

// Wake returns a waiting execution to running on a Section 7.2 wake
// event: tool result, answer, child message, any directive, timer,
// capability update or freed lease.
func (o *Orchestrator) Wake(id types.RunID, event string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State != types.RunWaiting {
		return fmt.Errorf("orchestrator: run %q is %q, only waiting runs wake", id, rec.Run.State)
	}
	rec.Run.WaitReason = nil
	_ = o.store.UpdateRun(rec)
	if err := o.transitionLocked(id, types.RunRunning, "wake: "+event); err != nil {
		return err
	}
	o.admitLocked()
	return nil
}

// Block moves a running or waiting run to blocked (Section 7.2).
func (o *Orchestrator) Block(id types.RunID, reason types.BlockReason, detail string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	rec.Run.BlockReason = &reason
	if err := o.store.UpdateRun(rec); err != nil {
		return err
	}
	if o.hooks.ReleaseLeases != nil {
		o.hooks.ReleaseLeases(id)
	}
	if err := o.transitionLocked(id, types.RunBlocked, detail); err != nil {
		return err
	}
	o.emitReportLocked(id, "blocked: "+detail)
	o.admitLocked()
	return nil
}

// Unblock returns a blocked run to the queue.
func (o *Orchestrator) Unblock(id types.RunID) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if rec.Run.State != types.RunBlocked {
		return fmt.Errorf("orchestrator: run %q is %q, only blocked runs unblock", id, rec.Run.State)
	}
	rec.Run.BlockReason = nil
	_ = o.store.UpdateRun(rec)
	if err := o.transitionLocked(id, types.RunQueued, "unblocked"); err != nil {
		return err
	}
	o.admitLocked()
	return nil
}

// Transition walks one Section 7.2 edge for engine-driven flows such
// as verifying. Control orders use the dedicated methods above.
func (o *Orchestrator) Transition(id types.RunID, to types.RunState) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.transitionLocked(id, to, "engine transition")
}

// Finish records a terminal outcome. Successful runs pass through the gate;
// failures and cancellations follow their own state edges (Sections 7.3, 7.5).
func (o *Orchestrator) Finish(id types.RunID, to types.RunState, reason string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !to.IsTerminal() {
		return fmt.Errorf("orchestrator: finish needs a terminal state, got %q", to)
	}
	rec, err := o.store.GetRun(id)
	if err != nil {
		return err
	}
	if to == types.RunCompleted || to == types.RunCompletedPending {
		if rec.Run.State == types.RunWaiting {
			if err := o.transitionLocked(id, types.RunRunning, "completion resumed"); err != nil {
				return err
			}
		}
		if rec.Run.State == types.RunRunning || rec.Run.State == types.RunWaiting {
			if err := o.transitionLocked(id, types.RunVerifying, "completion gate"); err != nil {
				return err
			}
		}
	} else if rec.Run.State == types.RunVerifying || (rec.Run.State == types.RunWaiting && to == types.RunFailed) {
		if err := o.transitionLocked(id, types.RunRunning, "verification ended"); err != nil {
			return err
		}
	}
	if err := o.transitionLocked(id, to, reason); err != nil {
		return err
	}
	o.emitReportLocked(id, reason)
	o.journal(id, JournalCompletion, reason)
	o.admitLocked()
	return nil
}

// SetRecovering marks a run recovering during boot adoption; it
// leaves via queued, paused or blocked (Section 17.3).
func (o *Orchestrator) SetRecovering(id types.RunID) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.transitionLocked(id, types.RunRecovering, "boot adoption")
}

// emitReportLocked builds and stores the Section 7.6 delivery report
// from the run row, stored usage and the live runner snapshot.
func (o *Orchestrator) emitReportLocked(id types.RunID, summary string) {
	rec, err := o.store.GetRun(id)
	if err != nil {
		return
	}
	rep := DeliveryReport{
		RunID:     id,
		AgentID:   rec.Run.AgentID,
		State:     rec.Run.State,
		Summary:   summary,
		Usage:     o.store.GetUsage(id),
		CreatedAt: o.cfg.Clock(),
	}
	switch rec.Run.State {
	case types.RunPaused:
		if rec.Run.PauseReason != nil {
			rep.Reason = string(*rec.Run.PauseReason)
		}
	case types.RunBlocked:
		if rec.Run.BlockReason != nil {
			rep.Reason = string(*rec.Run.BlockReason)
		}
	case types.RunWaiting:
		if rec.Run.WaitReason != nil {
			rep.Reason = string(*rec.Run.WaitReason)
		}
	}
	if runner, ok := o.runners.Lookup(id); ok {
		snap := runner.Snapshot()
		rep.Usage = snap.Usage
		if snap.LastProgress != "" {
			rep.Pending = append(rep.Pending, "last progress: "+snap.LastProgress)
		}
	}
	_ = o.store.SaveReport(rep)
	o.emit(Event{Type: EventNotice, RunID: id, To: rec.Run.AgentID, Detail: rep.PlainSummary()})
}
