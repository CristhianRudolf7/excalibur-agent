// Completion gate (Section 7.5) as engine-independent library code.
//
// The gate is deterministic and runs every time the model gives a
// final answer. Depending on the task level (Section 8.1) it checks:
//
//  1. Criteria: all satisfied with fresh evidence (8.6), or not
//     applicable with a recorded justification. Failed or unverified
//     criteria block.
//  2. Project tests: in software projects, the tests-folder
//     checklist ran complete on the current code version and passed
//     (8.8).
//  3. Subtasks and subagents: no children in non-terminal states and
//     no blocking unanswered questions. A paused or blocked child
//     forces the parent to decide before closing: resume it, cancel
//     it, or declare the subtask pending in the report.
//  4. Instructions: the final work used the latest instruction
//     revision (15.7); later changes stale the evidence.
//  5. Contrast with the original request (big tasks, once): a cheap
//     verifier-role call checks every part of the request is covered
//     and every ask is represented in the requirements. An omission
//     becomes a new requirement and the task goes back to work.
//  6. Independent verification (big tasks): the Section 8.7 verifier
//     subagent issued its verdict.
//
// How non-executable evidence counts:
//
//   - An inspection criterion admits judgment evidence (the model
//     examined the result). In N2 and N3 that evidence only counts
//     when the independent verifier agrees, and the report marks it
//     "verified by judgment".
//   - A code criterion (command, test, metric) never counts on
//     judgment.
//   - A user_confirmation criterion does not block forever: when
//     everything else passes, the run asks for confirmation through
//     the master. Without an answer it ends completed_with_pending
//     with "pending your confirmation", and the later confirmation
//     closes the criterion without redoing the work.
//
// On failure the engine injects a compact report ("C3.2 without
// evidence; test_login failed on the current commit") and the agent
// continues. After two failed attempts the run ends with an honest
// state: completed_with_pending (with the list) or blocked. It never
// declares success without evidence.
package tasks

import (
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// GateAttempts is the number of failed gate evaluations before the
// run ends with an honest state (Section 7.3).
const GateAttempts = 2

// EvidenceKind tells the gate how an evidence was produced. Only
// executable kinds satisfy code criteria; judgment only ever
// satisfies inspection, and in N2/N3 only with verifier agreement.
type EvidenceKind string

// Evidence kinds.
const (
	EvidenceExecutable   EvidenceKind = "executable"
	EvidenceObservation  EvidenceKind = "observation"
	EvidenceJudgment     EvidenceKind = "judgment"
	EvidenceIndependent  EvidenceKind = "independent"
	EvidenceConfirmation EvidenceKind = "user_confirmation"
)

// GateCriterion is one criterion with its computed state and the
// kind of its latest evidence.
type GateCriterion struct {
	Criterion types.Criterion
	State     types.CriterionState
	// FreshStale mirrors CriterionStatus.Stale for reporting.
	Stale bool
	// Kind is how the latest evidence was produced.
	Kind EvidenceKind
	// VerifierAgreed is true when the independent verifier confirmed
	// a judgment evidence.
	VerifierAgreed bool
	// Justification holds the recorded reason for not_applicable.
	Justification string
	// ConfirmationReceived closes a user_confirmation criterion
	// without redoing the work.
	ConfirmationReceived bool
}

// GateChild is one subtask/subagent with its state and its blocking
// questions.
type GateChild struct {
	ID               string
	State            types.RunState
	BlockingQuestion bool
}

// ContrastResult is the once-per-big-task check against the original
// request (Section 7.5.5): which request parts are covered and which
// asks never made it into the requirements.
type ContrastResult struct {
	Done      bool
	Covered   bool
	Omissions []string
}

// GateInput is everything the deterministic gate needs.
type GateInput struct {
	Level    types.TaskLevel
	Criteria []GateCriterion
	// ChecklistRequired marks software projects (7.5.2); Complete is
	// true when every checklist line ran on the current version and
	// passed. Detail names the failing lines for the injected note.
	ChecklistRequired bool
	ChecklistComplete bool
	ChecklistDetail   string
	Children          []GateChild
	// InstructionsRevSeen vs Current implement 7.5.4.
	InstructionsRevSeen int
	InstructionsCurrent int
	// ContrastRequired marks big tasks; Contrast carries the result.
	ContrastRequired bool
	Contrast         *ContrastResult
	// VerifierRequired marks big tasks needing the 8.7 verdict;
	// VerifierDone is true when it was issued.
	VerifierRequired bool
	VerifierDone     bool
}

// GateReport is the gate outcome: OK, or the blocking pendings the
// engine injects as a compact note.
type GateReport struct {
	OK       bool
	Pendings []string
}

// Evaluate runs the deterministic gate of Section 7.5.
func Evaluate(in GateInput) GateReport {
	var pendings []string
	for _, c := range in.Criteria {
		if p := criterionPending(in.Level, c); p != "" {
			pendings = append(pendings, p)
		}
	}
	if in.ChecklistRequired && !in.ChecklistComplete {
		if in.ChecklistDetail != "" {
			pendings = append(pendings, "project tests: "+in.ChecklistDetail)
		} else {
			pendings = append(pendings, "project tests did not run complete on the current version")
		}
	}
	for _, ch := range in.Children {
		if ch.State.IsTerminal() {
			continue
		}
		if ch.BlockingQuestion {
			pendings = append(pendings, fmt.Sprintf("child %s has a blocking unanswered question", ch.ID))
			continue
		}
		switch ch.State {
		case types.RunPaused, types.RunBlocked:
			pendings = append(pendings, fmt.Sprintf("child %s is %s: resume it, cancel it, or declare the subtask pending", ch.ID, ch.State))
		default:
			pendings = append(pendings, fmt.Sprintf("child %s still %s", ch.ID, ch.State))
		}
	}
	if in.InstructionsCurrent > in.InstructionsRevSeen {
		pendings = append(pendings, "work predates the latest instruction revision; evidence is stale")
	}
	if in.ContrastRequired {
		switch {
		case in.Contrast == nil || !in.Contrast.Done:
			pendings = append(pendings, "contrast with the original request not run")
		case len(in.Contrast.Omissions) > 0:
			pendings = append(pendings, "contrast omissions become new requirements: "+strings.Join(in.Contrast.Omissions, "; "))
		case !in.Contrast.Covered:
			pendings = append(pendings, "contrast: deliverable does not cover the original request")
		}
	}
	if in.VerifierRequired && !in.VerifierDone {
		pendings = append(pendings, "independent verifier verdict missing")
	}
	return GateReport{OK: len(pendings) == 0, Pendings: pendings}
}

// criterionPending applies Section 7.5.1 with the non-executable
// evidence rules; "" means the criterion passes the gate.
func criterionPending(level types.TaskLevel, c GateCriterion) string {
	code := c.Criterion.Code.String()
	if c.State == types.CriterionNotApplicable {
		if c.Justification != "" {
			return ""
		}
		return code + " marked not applicable without a recorded justification"
	}
	if c.Criterion.Method == types.CriterionUserConfirmation {
		if c.ConfirmationReceived || c.State == types.CriterionSatisfied {
			return ""
		}
		// Never blocks forever: the caller ends
		// completed_with_pending with "pending your confirmation"
		// when this is the only pending (see HonestState).
		return code + " pending your confirmation"
	}
	if c.State != types.CriterionSatisfied {
		if c.Stale {
			return code + " without fresh evidence (stale)"
		}
		return code + " without evidence"
	}
	// Satisfied: check the evidence kind was allowed to satisfy it.
	switch c.Criterion.Method {
	case types.CriterionCommand, types.CriterionTest, types.CriterionMetric:
		if c.Kind == EvidenceJudgment {
			return code + " is a code criterion and never counts on judgment"
		}
	case types.CriterionInspection:
		if c.Kind == EvidenceJudgment && (level == types.TaskN2 || level == types.TaskN3) && !c.VerifierAgreed {
			return code + " inspection by judgment needs the independent verifier to agree"
		}
	}
	return ""
}

// CompactNote renders the injected note after a failed gate
// evaluation: what is missing and why, in one line per pending.
func CompactNote(rep GateReport) string {
	return strings.Join(rep.Pendings, "; ")
}

// HonestState maps a failing gate after GateAttempts attempts to its
// honest terminal state (Section 7.3): blocked when the pendings
// name a loop, missing mandatory data, an uncertain effect or a
// verifier disagreement; completed_with_pending otherwise. A lone
// "pending your confirmation" always ends completed_with_pending so
// the later confirmation closes the criterion without redoing the
// work (Section 7.5).
func HonestState(rep GateReport) types.RunState {
	if len(rep.Pendings) == 0 {
		return types.RunCompleted
	}
	onlyConfirmation := true
	for _, p := range rep.Pendings {
		lower := strings.ToLower(p)
		switch {
		case strings.Contains(lower, "verifier disagreement"),
			strings.Contains(lower, "loop"),
			strings.Contains(lower, "missing mandatory"),
			strings.Contains(lower, "uncertain effect"):
			return types.RunBlocked
		}
		if !strings.Contains(lower, "pending your confirmation") {
			onlyConfirmation = false
		}
	}
	_ = onlyConfirmation
	return types.RunCompletedPending
}

// ConfirmationOnly reports whether every pending is a user
// confirmation wait: the run may stop asking and end
// completed_with_pending (Section 7.5).
func ConfirmationOnly(rep GateReport) bool {
	if len(rep.Pendings) == 0 {
		return false
	}
	for _, p := range rep.Pendings {
		if !strings.Contains(strings.ToLower(p), "pending your confirmation") {
			return false
		}
	}
	return true
}

// IsBigTask reports whether contrast (7.5.5) and the verifier
// verdict (7.5.6) apply: tasks with a macro plan behind them — N3,
// or N2 with at least VerifierRequirements requirements.
func IsBigTask(level types.TaskLevel, requirements int) bool {
	if level == types.TaskN3 {
		return true
	}
	return level == types.TaskN2 && requirements >= VerifierRequirements
}
