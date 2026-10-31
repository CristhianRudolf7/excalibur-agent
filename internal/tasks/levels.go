// Promotion rules between work levels (Section 8.1).
//
// N0 Conversation is questions, chat and memory or state queries and
// produces messages only, so it has no representation here. N1 is a
// bounded action without a project, N2 a task inside a registered
// project and N3 a days-long initiative with milestones.
//
// Promotions are deterministic: N1 becomes N2 when the task touches
// files of a registered project or exceeds 15 steps; N2 becomes N3
// when the estimate exceeds one day, the plan has more than one
// milestone, or the user asks for it. The user can always force a
// level, which wins over the automatic rules.
package tasks

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Promotion thresholds from Section 8.1. The step count and the
// one-day estimate are initial values to calibrate (Appendix B).
const (
	// PromoteStepsN1N2 moves an N1 task to N2 past this many steps.
	PromoteStepsN1N2 = 15
	// PromoteEstimateN2N3 moves an N2 task to N3 past this estimate.
	PromoteEstimateN2N3 = 24 * time.Hour
)

// PromotionInput is the observed state of a task used to decide an
// automatic promotion. It carries no model judgement.
type PromotionInput struct {
	// Level is the current task level.
	Level types.TaskLevel
	// TouchesProject is true when the task touches files of a
	// registered project.
	TouchesProject bool
	// Steps is the number of steps taken so far.
	Steps int
	// Estimate is the current total time estimate.
	Estimate time.Duration
	// Milestones is the number of milestones in the plan.
	Milestones int
	// UserAskedN3 is true when the user requested N3 treatment.
	UserAskedN3 bool
	// Forced carries a user-forced level, which wins over automation.
	Forced *types.TaskLevel
	// RegisteredProject is true when the task scope names a registered
	// project (used only for the N1 to N2 file-touch rule context).
	RegisteredProject bool
}

// Promote returns the level the task should run at. A user-forced
// level always wins; otherwise the deterministic rules of Section 8.1
// apply. N0 never appears here: conversations produce messages only.
func Promote(in PromotionInput) types.TaskLevel {
	if in.Forced != nil {
		return *in.Forced
	}
	switch in.Level {
	case types.TaskN1:
		if in.TouchesProject && in.RegisteredProject {
			return types.TaskN2
		}
		if in.Steps > PromoteStepsN1N2 {
			return types.TaskN2
		}
		return types.TaskN1
	case types.TaskN2:
		if in.Estimate > PromoteEstimateN2N3 {
			return types.TaskN3
		}
		if in.Milestones > 1 {
			return types.TaskN3
		}
		if in.UserAskedN3 {
			return types.TaskN3
		}
		return types.TaskN2
	default:
		return in.Level
	}
}

// NeedsProject reports whether the level requires a registered
// project: N2 and N3 do, N1 does not.
func NeedsProject(level types.TaskLevel) bool {
	return level == types.TaskN2 || level == types.TaskN3
}
