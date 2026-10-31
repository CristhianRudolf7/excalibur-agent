// Macro and micro plans (Section 8.4).
//
// The macro plan is created per project at registration or on a big
// commission; the planner role proposes requirements, criteria,
// steps, dependencies and milestones as structured operations, and
// the harness validates them (ValidatePlan) before applying. In N3,
// or whenever the user asks, the plan waits for user approval, and
// user comments land as directives. The micro plan exists only when
// a task starts and needs one: it is the step list of that task,
// kept by the agent through task_db, and archived with the task when
// the task ends. The current micro plan is always injected in the
// task layer in compact form (CompactMicro).
package tasks

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// EnsureMacroPlan returns the active macro plan of a scope, creating
// a draft when none exists. It never duplicates an active plan.
func EnsureMacroPlan(store PlanningStore, scope types.Scope) (Plan, error) {
	for _, p := range store.ListPlans(scope, types.PlanMacro) {
		if p.State == PlanActive || p.State == PlanPending || p.State == PlanDraft {
			return p, nil
		}
	}
	p := Plan{
		ID:        string(types.NewID(types.PrefixProject)) + "-macro",
		Scope:     scope,
		Type:      types.PlanMacro,
		Version:   1,
		State:     PlanDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreatePlan(p); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// NeedsApproval reports whether a plan must wait for user approval:
// N3 plans, or any plan when the user asks (Section 8.4).
func NeedsApproval(level types.TaskLevel, userAsked bool) bool {
	return level == types.TaskN3 || userAsked
}

// SubmitForApproval moves a draft macro plan to pending approval.
func SubmitForApproval(store PlanningStore, planID string) error {
	p, err := store.GetPlan(planID)
	if err != nil {
		return err
	}
	if p.State != PlanDraft {
		return fmt.Errorf("tasks: plan %q is %q, only drafts go to approval", planID, p.State)
	}
	p.State = PlanPending
	return store.UpdatePlan(p, p.Version)
}

// ApprovePlan activates a pending plan. Comments become directives:
// the caller turns them into directive payloads for the affected
// executions (Section 15.7 adapter owns delivery).
func ApprovePlan(store PlanningStore, planID string, comments []string) ([]string, error) {
	p, err := store.GetPlan(planID)
	if err != nil {
		return nil, err
	}
	if p.State != PlanPending {
		return nil, fmt.Errorf("tasks: plan %q is %q, only pending plans approve", planID, p.State)
	}
	p.State = PlanActive
	if err := store.UpdatePlan(p, p.Version); err != nil {
		return nil, err
	}
	return comments, nil
}

// ApplyProposal validates a planner proposal and applies it as a new
// plan version, keeping the previous one queryable. Items,
// requirements, criteria, checks and dependencies apply only when
// the whole snapshot validates (Section 8.4).
func ApplyProposal(store *MemoryStore, planID string, snap PlanSnapshot, apply func(*MemoryStore) error) error {
	if errs := ValidatePlan(snap); len(errs) > 0 {
		return &ProposalError{Reasons: errs}
	}
	p, err := store.GetPlan(planID)
	if err != nil {
		return err
	}
	p.UpdatedAt = time.Now().UTC()
	if err := store.UpdatePlan(p, p.Version); err != nil {
		return err
	}
	if apply != nil {
		return apply(store)
	}
	return nil
}

// ProposalError carries every validation violation of a planner
// proposal so the planner can fix them all at once.
type ProposalError struct {
	Reasons []error
}

// Error implements error.
func (e *ProposalError) Error() string {
	parts := make([]string, 0, len(e.Reasons))
	for _, r := range e.Reasons {
		parts = append(parts, r.Error())
	}
	return "tasks: invalid plan proposal: " + strings.Join(parts, "; ")
}

// EnsureMicroPlan returns the micro plan of a task, creating and
// archiving it with the task lifecycle: created when the task starts
// and needs one, archived when the task ends (Section 8.4). Every
// incarnation gets its own id so archived ones stay queryable.
func EnsureMicroPlan(store PlanningStore, task types.TaskID, scope types.Scope) (Plan, error) {
	owned := 0
	for _, p := range store.ListPlans(scope, types.PlanMicro) {
		if p.TaskID != task {
			continue
		}
		owned++
		if p.State != PlanArchived && p.State != PlanSuperseded {
			return p, nil
		}
	}
	id := string(task) + "-micro"
	if owned > 0 {
		id = fmt.Sprintf("%s-micro-%d", string(task), owned+1)
	}
	p := Plan{
		ID:        id,
		Scope:     scope,
		Type:      types.PlanMicro,
		TaskID:    task,
		Version:   1,
		State:     PlanActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreatePlan(p); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// ArchiveMicroPlan archives the micro plan when its task ends.
func ArchiveMicroPlan(store PlanningStore, task types.TaskID, scope types.Scope) error {
	for _, p := range store.ListPlans(scope, types.PlanMicro) {
		if p.TaskID == task && p.State != PlanArchived {
			p.State = PlanArchived
			if err := store.UpdatePlan(p, p.Version); err != nil {
				return err
			}
		}
	}
	return nil
}

// CompactMicro renders the current micro plan in the compact form
// injected every turn in the task layer (Section 8.4): one line per
// step with its state, owner and estimate.
func CompactMicro(store PlanningStore, planID string) string {
	items := store.ListPlanItems(planID)
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	var b strings.Builder
	done := 0
	for _, it := range items {
		if it.State == types.PlanItemDone {
			done++
		}
		mark := " "
		switch it.State {
		case types.PlanItemDone:
			mark = "x"
		case types.PlanItemInProgress:
			mark = ">"
		case types.PlanItemBlocked:
			mark = "!"
		case types.PlanItemCanceled:
			mark = "-"
		}
		fmt.Fprintf(&b, "[%s] %s %s", mark, it.Code, it.Title)
		if it.Agent != "" {
			fmt.Fprintf(&b, " @%s", it.Agent)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%d/%d done", done, len(items))
	return b.String()
}
