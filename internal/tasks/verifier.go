// Independent verifier subagent in big tasks (Section 8.7).
//
// The completion gate creates a verifier automatically for N2 tasks
// with at least five requirements, at every N3 milestone, and
// whenever the user asks. The verifier receives the literal original
// request, requirements, criteria, evidence and the change diff, with
// read-only access plus check execution — and never the reasoning of
// the agent that did the work, so its judgement is not anchored. It
// re-runs the checks instead of trusting prior evidence when it can
// reproduce them, reviews inspection criteria, and looks for
// uncovered requirements. Its verdict lands as
// independent-verification evidence. On disagreement the task goes
// back to the executor with the list (two rounds at most); then the
// user is informed with both positions. A different model family
// from the executor is preferred when possible.
//
// Spawning the subagent belongs to the orchestrator (H8): this file
// holds the engine-independent rules — when to verify, what the
// verifier may see, the verdict shape, and the round cap — plus a
// VerifierFunc seam the gate uses to request one verdict round.
package tasks

import (
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// VerifierRequirements is the N2 size that triggers automatic
// verification (Section 8.7, Appendix B).
const VerifierRequirements = 5

// MaxVerifierRounds caps executor/verifier disagreement rounds at 2
// (Section 8.7).
const MaxVerifierRounds = 2

// ShouldVerify reports whether the gate must request an independent
// verifier: N2 with at least five requirements, any N3 milestone
// check, or an explicit user request.
func ShouldVerify(level types.TaskLevel, requirements int, milestoneCheck, userAsked bool) bool {
	if userAsked {
		return true
	}
	if milestoneCheck && level == types.TaskN3 {
		return true
	}
	return level == types.TaskN2 && requirements >= VerifierRequirements
}

// VerifierInput is the context the verifier is compiled with. It
// carries no executor reasoning by construction: there is no field
// for it (Section 8.7).
type VerifierInput struct {
	// Request is the literal original request.
	Request string
	// Requirements, Criteria and Evidence mirror the task rows.
	Requirements []types.Requirement
	Criteria     []types.Criterion
	Evidence     []EvidenceRecord
	// Diff summarizes the change under review.
	Diff string
	// CodeVersion is the tree state the checks must run on.
	CodeVersion CodeVersion
	// Round counts the disagreement rounds so far.
	Round int
}

// Verdict is one criterion verdict with its own evidence,
// recorded as independent-verification evidence.
type Verdict struct {
	Criterion types.CriterionID
	Agree     bool
	Note      string
	Evidence  EvidenceRecord
}

// VerifierFunc runs one verifier round over the input and returns a
// verdict per criterion. The orchestrator provides it; tests stub it.
type VerifierFunc func(in VerifierInput) ([]Verdict, error)

// VerifierOutcome is the settled result after up to MaxVerifierRounds.
type VerifierOutcome struct {
	// Agreed is true when verifier and executor agree on every
	// criterion within the round cap.
	Agreed bool
	// Rounds counts the verifier rounds run.
	Rounds int
	// Disagreements lists the criteria still disputed.
	Disagreements []Verdict
	// Verdicts holds the last round, recorded as evidence.
	Verdicts []Verdict
}

// RunVerifier runs verifier rounds until agreement or the round cap,
// then reports both positions for the user (Section 8.7). The
// executor-fix step between rounds belongs to the engine: recheck
// rebuilds the input from the database after the executor retries.
func RunVerifier(run VerifierFunc, in VerifierInput, recheck func(round int) VerifierInput) (VerifierOutcome, error) {
	out := VerifierOutcome{}
	input := in
	for round := 1; round <= MaxVerifierRounds; round++ {
		input.Round = round
		verdicts, err := run(input)
		if err != nil {
			return out, fmt.Errorf("tasks: verifier round %d: %w", round, err)
		}
		out.Rounds = round
		out.Verdicts = verdicts
		var disputed []Verdict
		for _, v := range verdicts {
			if !v.Agree {
				disputed = append(disputed, v)
			}
		}
		if len(disputed) == 0 {
			out.Agreed = true
			return out, nil
		}
		out.Disagreements = disputed
		if recheck != nil && round < MaxVerifierRounds {
			input = recheck(round)
		}
	}
	return out, nil
}

// RecordVerdicts stores the last-round verdicts as
// independent-verification evidence, one record per criterion
// verdict, so the gate and the history can audit them.
func RecordVerdicts(store PlanningStore, run types.RunID, verdicts []Verdict, revSeen int) error {
	for _, v := range verdicts {
		rec := v.Evidence
		rec.Evidence.RunID = run
		rec.InstructionRevSeen = revSeen
		if err := store.RecordEvidence(rec); err != nil {
			return err
		}
	}
	return nil
}
