// Planning data model on top of the shared contracts (Section 8.2,
// Appendix A).
//
// The shared structs (Task, Requirement, Criterion, Check, Evidence)
// live in internal/types. This file adds the plan-side tables:
// projects, plans, plan items, dependencies, milestones, the
// task-to-requirement edges and the project logbook, plus the
// validation the harness applies before accepting a plan proposal
// (Section 8.4): existing references, an acyclic graph, every
// requirement with at least one criterion, and every criterion with
// a verification method.
package tasks

import (
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Plan states: a plan is drafted by the planner role, optionally
// approved by the user (N3, or whenever the user asks), then active;
// superseded versions stay queryable.
type PlanState string

// Plan states.
const (
	PlanDraft      PlanState = "draft"
	PlanPending    PlanState = "pending_approval"
	PlanActive     PlanState = "active"
	PlanArchived   PlanState = "archived"
	PlanSuperseded PlanState = "superseded"
)

// Plan is a macro plan per project or a micro plan per task
// (Section 8.2). A micro plan names its owning task; a macro plan
// does not.
type Plan struct {
	ID        string
	Scope     types.Scope
	Type      types.PlanType
	TaskID    types.TaskID
	Version   int
	State     PlanState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlanItem is one step of a plan. A macro step may carry the task
// that implements it; micro steps are the work list of one task.
type PlanItem struct {
	ID        string
	Code      string
	PlanID    string
	TaskID    types.TaskID
	Title     string
	Detail    string
	State     types.PlanItemState
	Priority  types.Priority
	Agent     string
	Estimate  time.Duration
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Dependency links two plan items or tasks. Type blocks orders the
// work; informs only notifies (Section 8.2).
type Dependency struct {
	From string
	To   string
	Type types.DependencyType
}

// Milestone closes when every listed criterion is satisfied.
type Milestone struct {
	ID         string
	Code       types.MilestoneRef
	PlanID     string
	Title      string
	Criteria   []types.CriterionID
	TargetDate time.Time
	Done       bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Project is the registered-project row backing scopes (Section 9.2,
// referenced by 8.10 project operations).
type Project struct {
	ID        types.ProjectID
	Name      string
	Alias     string
	Roots     []string
	Archived  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProjectLogEntry is one bitacora line (Section 8.2, project_log).
type ProjectLogEntry struct {
	ID        string
	ProjectID types.ProjectID
	Kind      string
	Text      string
	Refs      []string
	CreatedAt time.Time
}

// PlanSnapshot is everything the harness validates at once: the plan
// items keyed by id, the dependency edges, the requirements with
// their criteria, and the criteria with their methods.
type PlanSnapshot struct {
	Items        map[string]PlanItem
	Dependencies []Dependency
	// ReqCriteria maps a requirement id to its criterion ids.
	ReqCriteria map[types.RequirementID][]types.CriterionID
	// CriterionMethods maps a criterion id to its method; a missing
	// entry means the criterion declares no method.
	CriterionMethods map[types.CriterionID]types.CriterionMethod
}

// ValidatePlan enforces the Section 8.4 admission rules: dependency
// endpoints exist, the graph is acyclic, every requirement carries at
// least one criterion, and every criterion declares a method. It
// returns one error per violation so the planner can fix them all.
func ValidatePlan(snap PlanSnapshot) []error {
	var errs []error
	for _, d := range snap.Dependencies {
		if _, ok := snap.Items[d.From]; !ok {
			errs = append(errs, fmt.Errorf("tasks: dependency from unknown item %q", d.From))
		}
		if _, ok := snap.Items[d.To]; !ok {
			errs = append(errs, fmt.Errorf("tasks: dependency to unknown item %q", d.To))
		}
	}
	if cycle := FindCycle(nodesOf(snap.Items), snap.Dependencies); cycle != nil {
		errs = append(errs, fmt.Errorf("tasks: dependency graph has a cycle: %v", cycle))
	}
	for req, crits := range snap.ReqCriteria {
		if len(crits) == 0 {
			errs = append(errs, fmt.Errorf("tasks: requirement %q has no criteria", req))
		}
		for _, c := range crits {
			m, ok := snap.CriterionMethods[c]
			if !ok || m == "" {
				errs = append(errs, fmt.Errorf("tasks: criterion %q declares no method", c))
			}
		}
	}
	return errs
}

// ValidateRequirementCriteria checks the plan invariant for one
// requirement in isolation: at least one criterion, each with a
// method. The task_db tool uses it for incremental edits.
func ValidateRequirementCriteria(req types.RequirementID, crits []types.Criterion, _ map[string]PlanItem) []error {
	var errs []error
	if len(crits) == 0 {
		errs = append(errs, fmt.Errorf("tasks: requirement %q has no criteria", req))
	}
	for _, c := range crits {
		if c.Method == "" {
			errs = append(errs, fmt.Errorf("tasks: criterion %q declares no method", c.ID))
		}
	}
	return errs
}

func nodesOf(items map[string]PlanItem) []string {
	out := make([]string, 0, len(items))
	for id := range items {
		out = append(out, id)
	}
	return out
}
