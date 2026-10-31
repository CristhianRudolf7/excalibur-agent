package orchestrator

import (
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Correction propagation of Section 15.7: persist the new
// instruction revision first, compute the affected runs
// deterministically, decide one priority per run, fan directives out
// top-down, invalidate stale evidence and confirm to the user.

// Correction is one user fix or instruction change mid-work.
type Correction struct {
	// From is the agent the directive is attributed to (parent or
	// master side).
	From types.AgentID
	// TaskIDs links the task/requirement graph rows the change
	// touches (Section 15.7 point 3).
	TaskIDs []types.TaskID
	// Text is the literal new instruction; Summary is the change
	// note for the confirmation.
	Text    string
	Summary string
	Action  DirectiveAction
	// ExplicitStop forces interrupt priority (user asked to stop).
	ExplicitStop bool
	// ImpactArea lists paths/resources the change affects (8.3): the
	// priority intersection runs against it.
	ImpactArea []string
}

// PriorityInfo is the per-run input to the Section 15.7 point-4
// decision.
type PriorityInfo struct {
	ExplicitStop       bool
	ResourceKeys       []string
	ImpactArea         []string
	Action             string
	Instruction        string
	HasPendingExternal bool
}

// DecidePriority applies the Section 15.7 point-4 rules. Interrupt
// when the user asked to stop, the in-flight tool touches the impact
// area, the change forbids the current action (decider, high
// confidence), or an affected external action is pending. Otherwise
// normal.
func DecidePriority(info PriorityInfo, prohibits func(action, instruction string) (bool, float64)) DirectivePriority {
	if info.ExplicitStop {
		return PriorityInterrupt
	}
	if len(MatchResourceKeys(info.ResourceKeys, info.ImpactArea)) > 0 {
		return PriorityInterrupt
	}
	if info.HasPendingExternal {
		return PriorityInterrupt
	}
	if prohibits != nil && info.Action != "" && info.Instruction != "" {
		if no, conf := prohibits(info.Action, info.Instruction); no && conf >= 0.7 {
			return PriorityInterrupt
		}
	}
	return PriorityNormal
}

// MatchResourceKeys intersects in-flight tool keys with the impact
// area: exact match or path-prefix overlap in either direction.
func MatchResourceKeys(keys, area []string) []string {
	var out []string
	for _, k := range keys {
		for _, a := range area {
			if k == a || strings.HasPrefix(k, a+"/") || strings.HasPrefix(a, k+"/") {
				out = append(out, k)
			}
		}
	}
	return out
}

// CorrectionResult is the per-run outcome plus the master
// confirmation (Section 15.7 point 8): what adjusted, what had
// already finished, and what waits on a non-interruptible tool.
type CorrectionResult struct {
	Run       types.RunID
	Priority  DirectivePriority
	Delivered bool
	Note      string
}

// PropagateCorrection runs the Section 15.7 pipeline: persist the
// revision, fan out per-run directives, invalidate evidence and
// report back what the master confirms to the user. Task-scoped
// corrections pass the task scope (task:<id>) so only that branch is
// re-sent; project or general instructions pass the wider scope and
// reach every descendant automatically instead of trusting the parent
// to forward them.
func (o *Orchestrator) PropagateCorrection(c Correction, scope types.Scope, origin string) ([]CorrectionResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rev, err := o.bumpInstructionLocked(scope, c.Text, origin)
	if err != nil {
		return nil, err
	}
	affected := o.affectedLocked(c.TaskIDs, &scope)
	var out []CorrectionResult
	var finished, waiting []types.RunID
	for _, rec := range affected {
		info := PriorityInfo{
			ExplicitStop: c.ExplicitStop,
			ImpactArea:   c.ImpactArea,
			Action:       string(c.Action),
			Instruction:  c.Text,
		}
		if runner, ok := o.runners.Lookup(rec.Run.ID); ok {
			snap := runner.Snapshot()
			info.ResourceKeys = snap.ToolResourceKeys
		}
		if o.hooks.PendingExternal != nil {
			info.HasPendingExternal = o.hooks.PendingExternal(rec.Run.ID)
		}
		priority := DecidePriority(info, o.hooks.Prohibits)
		if rec.Run.State.IsTerminal() {
			finished = append(finished, rec.Run.ID)
			out = append(out, CorrectionResult{Run: rec.Run.ID, Priority: priority, Note: "already finished"})
			continue
		}
		from := c.From
		if from == "" {
			from = rec.Run.AgentID
			if agent, err := o.store.GetAgent(rec.Run.AgentID); err == nil && agent.ParentID != nil {
				from = *agent.ParentID
			}
		}
		d, err := o.sendDirectiveLocked(from, rec.Run.AgentID, c.Action, c.Text, c.Summary, priority)
		if err != nil {
			out = append(out, CorrectionResult{Run: rec.Run.ID, Priority: priority, Note: err.Error()})
			continue
		}
		_ = d
		if rec.Run.State == types.RunPaused && priority == PriorityNormal {
			out = append(out, CorrectionResult{Run: rec.Run.ID, Priority: priority, Delivered: true, Note: "queued in mailbox until resume"})
			continue
		}
		if runner, ok := o.runners.Lookup(rec.Run.ID); ok {
			snap := runner.Snapshot()
			if snap.HasTool && (snap.ToolCancel == types.CancelAfterCompletion || snap.ToolCancel == types.CancelCooperative) && priority == PriorityNormal {
				waiting = append(waiting, rec.Run.ID)
			}
		}
		out = append(out, CorrectionResult{Run: rec.Run.ID, Priority: priority, Delivered: true})
	}
	if o.hooks.InvalidateEvidence != nil {
		o.hooks.InvalidateEvidence(c.TaskIDs, rev)
	}
	confirm := fmt.Sprintf("revision %d applied: %d adjusted, %d already finished, %d waiting on a non-interruptible tool",
		rev, len(out)-len(finished), len(finished), len(waiting))
	o.emit(Event{Type: EventNotice, Detail: confirm})
	return out, nil
}

// bumpInstructionLocked persists the next literal instruction
// revision for scope (Section 15.7 point 2).
func (o *Orchestrator) bumpInstructionLocked(scope types.Scope, text, origin string) (int, error) {
	rev := 1
	if latest, ok := o.store.LatestInstruction(scope); ok {
		rev = latest.Revision + 1
	}
	ins := types.Instruction{
		ID: string(types.NewMessageID()), Scope: scope, Text: text,
		Revision: rev, Origin: origin, State: "active", CreatedAt: o.cfg.Clock(),
	}
	if err := o.store.SaveInstruction(ins); err != nil {
		return 0, err
	}
	return rev, nil
}

// affectedLocked computes the affected runs deterministically: runs
// whose task is linked to the change, plus every run in the scope
// when the instruction is project- or general-wide (Section 15.7
// point 3).
func (o *Orchestrator) affectedLocked(taskIDs []types.TaskID, scope *types.Scope) []RunRecord {
	want := map[types.TaskID]bool{}
	for _, t := range taskIDs {
		want[t] = true
	}
	var out []RunRecord
	for _, rec := range o.store.ListRuns() {
		if want[rec.Run.TaskID] {
			out = append(out, rec)
			continue
		}
		if scope != nil && rec.Scope == *scope {
			out = append(out, rec)
		}
	}
	return out
}
