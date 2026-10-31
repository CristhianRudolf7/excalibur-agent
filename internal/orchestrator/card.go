package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// DelegationCard is the structured Section 15.4 handover: a child
// never inherits the parent history unless fork_context asks for it.
// Profile is the orchestrator's additive field: the Section 15.4
// table implies the profile through the toolset, but the identity
// (15.2) needs it named, so the card carries it explicitly. See
// docs/decisions/0014-orchestrator-delegation-card-profile.md.
type DelegationCard struct {
	// Name and Description form the Section 15.2 identity.
	Name        string
	Description string
	Profile     agents.ProfileName
	Goal        string
	Deliverable string
	Scope       types.Scope
	Paths       []string
	Inputs      []string
	Pinned      []string
	Skills      []string
	Tools       []string
	Budget      types.Budget
	Deadline    time.Time
	Done        []string
	Report      string
	ForkContext bool
	TaskID      types.TaskID
	Lane        Lane
}

// Validate checks the card: identity, goal, deliverable, done
// criteria, scope and profile must be present; the toolset must sit
// inside the profile (Section 15.9); the deadline must lie ahead.
func (c DelegationCard) Validate(now time.Time, level int) error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("orchestrator: delegation card needs an agent name")
	}
	if strings.TrimSpace(c.Description) == "" {
		return fmt.Errorf("orchestrator: delegation card needs a function description")
	}
	if strings.TrimSpace(c.Goal) == "" {
		return fmt.Errorf("orchestrator: delegation card needs a goal")
	}
	if strings.TrimSpace(c.Deliverable) == "" {
		return fmt.Errorf("orchestrator: delegation card needs a deliverable")
	}
	if len(c.Done) == 0 {
		return fmt.Errorf("orchestrator: delegation card needs done criteria")
	}
	if strings.TrimSpace(c.Report) == "" {
		return fmt.Errorf("orchestrator: delegation card needs a report contract")
	}
	if err := c.Scope.Validate(); err != nil {
		return fmt.Errorf("orchestrator: %w", err)
	}
	profile, err := agents.Lookup(c.Profile)
	if err != nil {
		return fmt.Errorf("orchestrator: %w", err)
	}
	if _, err := agents.ClampTools(profile, level, c.Tools); err != nil {
		return fmt.Errorf("orchestrator: %w", err)
	}
	if !c.Deadline.IsZero() && !c.Deadline.After(now) {
		return fmt.Errorf("orchestrator: delegation deadline is in the past")
	}
	return nil
}

// FanOutReason names one Section 15.3 criterion for creating children.
type FanOutReason string

// Fan-out criteria of Section 15.3.
const (
	FanOutParallel   FanOutReason = "parallel_subtasks"
	FanOutIsolation  FanOutReason = "context_isolation"
	FanOutVerify     FanOutReason = "independent_verification"
	FanOutProfile    FanOutReason = "distinct_profile"
	FanOutCoupled    FanOutReason = "coupled_sequential"
	FanOutParentWork FanOutReason = "parent_starts_alone"
)

// FanOutAdvice is the deterministic answer to "should this parent
// spawn children now": parents start doing the work and spawn only
// when a criterion holds; coupled sequential work never fans out
// (Section 15.3).
type FanOutAdvice struct {
	Spawn    bool
	Warnings []string
}

// AdviseFanOut applies the Section 15.3 rules. pathSets holds one
// file set per proposed child: overlaps warn to serialize, assign the
// shared file to one child, or switch to worktrees (partitioning
// itself is Section 16.3, owned by the concurrency milestone).
func AdviseFanOut(reasons []FanOutReason, pathSets [][]string) FanOutAdvice {
	advice := FanOutAdvice{}
	positive := false
	for _, r := range reasons {
		switch r {
		case FanOutParallel, FanOutIsolation, FanOutVerify, FanOutProfile:
			positive = true
		case FanOutCoupled:
			advice.Warnings = append(advice.Warnings,
				"coupled sequential edits stay with one agent: parallel agents lose 39-70% on sequential work")
		case FanOutParentWork:
			advice.Warnings = append(advice.Warnings,
				"parent starts alone and spawns when a fan-out criterion holds")
		}
	}
	advice.Spawn = positive
	for i := range pathSets {
		for j := i + 1; j < len(pathSets); j++ {
			if overlap, a, b := firstOverlap(pathSets[i], pathSets[j]); overlap {
				advice.Warnings = append(advice.Warnings,
					fmt.Sprintf("file sets overlap on %q vs %q: serialize, assign to one child, or use worktrees", a, b))
			}
		}
	}
	return advice
}

func firstOverlap(a, b []string) (bool, string, string) {
	for _, x := range a {
		for _, y := range b {
			if x == y || strings.HasPrefix(x, y+"/") || strings.HasPrefix(y, x+"/") {
				return true, x, y
			}
		}
	}
	return false, "", ""
}

// SplitBudget shares a parent remainder across children: every share
// is capped by what is left, and the shares never exceed the
// remainder (Sections 15.9, 18.4).
func SplitBudget(parent types.Budget, used Usage, shares []float64) []types.Budget {
	rest := RemainingBudget(parent, used)
	total := 0.0
	for _, s := range shares {
		total += s
	}
	out := make([]types.Budget, len(shares))
	for i, s := range shares {
		f := 0.0
		if total > 0 {
			f = s / total
		}
		out[i] = types.Budget{
			MaxSteps:   int(f * float64(rest.MaxSteps)),
			MaxTokens:  int(f * float64(rest.MaxTokens)),
			MaxCostUSD: f * rest.MaxCostUSD,
			MaxSeconds: int64(f * float64(rest.MaxSeconds)),
			MaxProcs:   rest.MaxProcs,
		}
	}
	return out
}

// RemainingBudget caps what one child may receive: never more than
// the parent has left (Section 15.9).
func RemainingBudget(parent types.Budget, used Usage) types.Budget {
	sub := func(max, u int) int {
		if max <= 0 {
			return 0
		}
		if left := max - u; left > 0 {
			return left
		}
		return 0
	}
	out := types.Budget{
		MaxSteps:   sub(parent.MaxSteps, used.Steps),
		MaxTokens:  sub(parent.MaxTokens, used.InputTokens+used.OutputTokens),
		MaxProcs:   parent.MaxProcs,
		MaxCostUSD: parent.MaxCostUSD - used.CostUSD,
		MaxSeconds: parent.MaxSeconds - int64(used.Elapsed/time.Second),
	}
	if out.MaxCostUSD < 0 {
		out.MaxCostUSD = 0
	}
	if out.MaxSeconds < 0 {
		out.MaxSeconds = 0
	}
	return out
}

// ClampChildBudget bounds a requested child budget by the parent
// remainder and fills unset fields with the profile default steps.
func ClampChildBudget(parent types.Budget, used Usage, requested types.Budget, defaultSteps int) types.Budget {
	rest := RemainingBudget(parent, used)
	if requested.MaxSteps <= 0 {
		requested.MaxSteps = defaultSteps
	}
	if requested.MaxSteps > rest.MaxSteps {
		requested.MaxSteps = rest.MaxSteps
	}
	if requested.MaxTokens <= 0 || requested.MaxTokens > rest.MaxTokens {
		requested.MaxTokens = rest.MaxTokens
	}
	if requested.MaxCostUSD <= 0 || requested.MaxCostUSD > rest.MaxCostUSD {
		requested.MaxCostUSD = rest.MaxCostUSD
	}
	if requested.MaxSeconds <= 0 || requested.MaxSeconds > rest.MaxSeconds {
		requested.MaxSeconds = rest.MaxSeconds
	}
	if requested.MaxProcs <= 0 {
		requested.MaxProcs = rest.MaxProcs
	}
	return requested
}
