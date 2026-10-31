// Replanning (Section 8.5).
//
// Triggers: a check fails after 3 correction attempts; a
// requirement or instruction changes; an external block appears; a
// dependency changes state; the user asks. The affected subgraph is
// deterministic: the descendants in the dependency graph of the
// changed nodes, plus the tasks linked to changed requirements. What
// is finished and not invalidated stays frozen. The planner receives
// the affected subgraph, the motive and the state, and returns a
// diff of operations (add, remove, reorder, change criteria). The
// harness validates the diff and applies it as a new plan version,
// keeping the previous one, then propagates: directives to the
// executions whose tasks changed, and a notice to the master when
// deadlines or scope change.
package tasks

import (
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Replan triggers (Section 8.5).
const (
	TriggerCheckFailed3x   = "check_failed_3x"
	TriggerRequirementFlip = "requirement_changed"
	TriggerInstructionFlip = "instruction_changed"
	TriggerExternalBlock   = "external_block"
	TriggerDependencyFlip  = "dependency_changed"
	TriggerUserAsked       = "user_asked"
)

// CorrectionAttempts is the number of failed correction attempts on
// a check that triggers a replan (Section 8.5).
const CorrectionAttempts = 3

// ReplanRequest is the deterministic input: what changed and why.
type ReplanRequest struct {
	PlanID       string
	Motive       string
	ChangedNodes []string
	ChangedReqs  []types.RequirementID
}

// ReplanDiff is the planner proposal: structural operations over the
// affected subgraph.
type ReplanDiff struct {
	// Add holds new items to append.
	Add []PlanItem
	// Remove holds item ids to cancel.
	Remove []string
	// Reorder holds item ids in their new order (inform edges only;
	// blocks edges keep dependency order).
	Reorder []string
	// CriteriaChanged maps requirement ids to their replacement
	// criterion sets.
	CriteriaChanged map[types.RequirementID][]types.Criterion
}

// Directive is a propagation payload for an execution whose task
// changed (Section 15.7 shape, delivery owned by the orchestrator).
type Directive struct {
	RunID   string
	Kind    string
	Payload string
}

// ReplanResult is the applied outcome.
type ReplanResult struct {
	// Affected is the deterministic affected subgraph.
	Affected []string
	// Frozen lists finished nodes kept untouched.
	Frozen []string
	// NewVersion is the plan version the diff landed on.
	NewVersion int
	// Directives go to the executions whose tasks changed.
	Directives []Directive
	// NotifyMaster is true when deadlines or scope change.
	NotifyMaster bool
}

// ComputeAffected resolves the deterministic affected subgraph of a
// replan request: changed nodes, their descendants in the plan
// dependency graph, and the items backing the changed requirements.
func ComputeAffected(store PlanningStore, planID string, req ReplanRequest, itemReqs map[string][]string) []string {
	deps := store.ListDependencies(planID)
	link := map[string][]string{}
	for item, reqs := range itemReqs {
		for _, r := range reqs {
			link[r] = append(link[r], item)
		}
	}
	changed := append([]string(nil), req.ChangedNodes...)
	for _, r := range req.ChangedReqs {
		changed = append(changed, link[string(r)]...)
	}
	var changedReqStr []string
	for _, r := range req.ChangedReqs {
		changedReqStr = append(changedReqStr, string(r))
	}
	return AffectedSubgraph(deps, changed, itemReqs, changedReqStr)
}

// ApplyReplan validates the planner diff and applies it as a new plan
// version: finished nodes outside the affected subgraph stay frozen,
// removals only cancel non-finished nodes, and criteria replacements
// keep the plan invariant (every requirement with at least one
// criterion, every criterion with a method). Previous versions stay
// queryable via PlanVersions.
func ApplyReplan(store PlanningStore, planID string, req ReplanRequest, diff ReplanDiff, itemReqs map[string][]string, runOfItem map[string]string, scopeChanged bool) (ReplanResult, error) {
	items := map[string]PlanItem{}
	for _, it := range store.ListPlanItems(planID) {
		items[it.ID] = it
	}
	affected := ComputeAffected(store, planID, req, itemReqs)
	inAffected := map[string]bool{}
	for _, a := range affected {
		inAffected[a] = true
	}
	for _, c := range req.ChangedNodes {
		inAffected[c] = true
	}
	var frozen []string
	for id, it := range items {
		if it.State == types.PlanItemDone && !inAffected[id] {
			frozen = append(frozen, id)
		}
	}
	// removals must not touch frozen work.
	for _, id := range diff.Remove {
		for _, f := range frozen {
			if f == id {
				return ReplanResult{}, fmt.Errorf("tasks: replan removes frozen finished item %q", id)
			}
		}
		if _, ok := items[id]; !ok {
			return ReplanResult{}, fmt.Errorf("tasks: replan removes unknown item %q", id)
		}
	}
	// criteria replacements keep the plan invariant.
	for r, crits := range diff.CriteriaChanged {
		if errs := ValidateRequirementCriteria(r, crits, items); len(errs) > 0 {
			return ReplanResult{}, &ProposalError{Reasons: errs}
		}
	}
	// apply: cancel removals, append additions.
	for _, id := range diff.Remove {
		it := items[id]
		it.State = types.PlanItemCanceled
		if err := store.UpdatePlanItem(it, it.Version); err != nil {
			return ReplanResult{}, err
		}
	}
	for _, add := range diff.Add {
		add.PlanID = planID
		add.CreatedAt = time.Now().UTC()
		add.UpdatedAt = add.CreatedAt
		if err := store.AddPlanItem(add); err != nil {
			return ReplanResult{}, err
		}
	}
	p, err := store.GetPlan(planID)
	if err != nil {
		return ReplanResult{}, err
	}
	p.UpdatedAt = time.Now().UTC()
	if err := store.UpdatePlan(p, p.Version); err != nil {
		return ReplanResult{}, err
	}
	// propagate: one directive per affected item with a known run.
	var directives []Directive
	for _, a := range affected {
		if run, ok := runOfItem[a]; ok {
			directives = append(directives, Directive{RunID: run, Kind: "replan", Payload: req.Motive + ": item " + a + " changed"})
		}
	}
	return ReplanResult{
		Affected:     affected,
		Frozen:       frozen,
		NewVersion:   p.Version + 1,
		Directives:   directives,
		NotifyMaster: scopeChanged,
	}, nil
}
