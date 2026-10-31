// Verification ladder and check selection (Sections 18.1, 18.2).
//
// The ladder, strongest first:
//
//  1. Executable checks: tests, build, typecheck, scripts, HTTP
//     probes, file assertions. They produce evidence automatically.
//  2. Tool observations: assertions over a browser snapshot, a
//     document content, an application state.
//  3. Model judgment with evidence, only when no executable check
//     exists. Recorded as judgment evidence, with lower weight. A
//     cheap model is never the only judge of code.
//  4. The independent verifier in big tasks (8.7).
//  5. User confirmation for criteria of that kind (taste, business
//     decisions).
//
// Selection: during the work, the impacted tests (from the changed
// files, via the code graph) plus the checks of the criteria in
// progress — cheap with fast feedback. Before closing, the full list
// of the project tests folder (8.8) and every criterion check. To
// choose between several candidate solutions a run generated, use
// distinguishing inputs and test execution, not a judge: selection
// is the bottleneck of repeated sampling and only captures between
// 50% and 76% of the possible gain.
package tasks

import (
	"sort"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// LadderRank orders evidence kinds strongest first. Executable
// evidence outranks observation, which outranks judgment; the
// independent verifier and the user confirmation sit on their own
// rungs for criteria of their kind.
func LadderRank(kind EvidenceKind) int {
	switch kind {
	case EvidenceExecutable:
		return 0
	case EvidenceObservation:
		return 1
	case EvidenceIndependent:
		return 2
	case EvidenceConfirmation:
		return 3
	case EvidenceJudgment:
		return 4
	default:
		return 5
	}
}

// CodeCriterionSatisfiedBy reports whether an evidence kind may
// satisfy a code criterion (command, test, metric): never judgment
// (Section 18.1). A cheap model is never the only judge of code.
func CodeCriterionSatisfiedBy(kind EvidenceKind) bool {
	return kind == EvidenceExecutable || kind == EvidenceIndependent
}

// InspectionSatisfiedBy reports whether an evidence kind may satisfy
// an inspection criterion: judgment counts, but in N2 and N3 only
// when the independent verifier agrees (Section 7.5).
func InspectionSatisfiedBy(level types.TaskLevel, kind EvidenceKind, verifierAgreed bool) bool {
	if kind == EvidenceExecutable || kind == EvidenceObservation || kind == EvidenceIndependent {
		return true
	}
	if kind != EvidenceJudgment {
		return false
	}
	if level == types.TaskN2 || level == types.TaskN3 {
		return verifierAgreed
	}
	return true
}

// CriterionChecks pairs a criterion with its executable checks for
// selection.
type CriterionChecks struct {
	Criterion types.Criterion
	Checks    []types.Check
}

// SelectDuringWork returns the checks to run during the work: the
// impacted tests (checks whose criterion impact area covers a
// changed file) plus the checks of the criteria in progress. It is
// cheap and gives fast feedback (Section 18.2).
func SelectDuringWork(all []CriterionChecks, changedFiles []string, inProgress map[types.CriterionID]bool) []types.Check {
	changed := map[string]bool{}
	for _, f := range changedFiles {
		changed[f] = true
	}
	var out []types.Check
	seen := map[types.CheckID]bool{}
	for _, cc := range all {
		impacted := false
		for _, p := range cc.Criterion.ImpactArea {
			if changed[p] {
				impacted = true
				break
			}
		}
		if impacted || inProgress[cc.Criterion.ID] {
			for _, chk := range cc.Checks {
				if !seen[chk.ID] {
					seen[chk.ID] = true
					out = append(out, chk)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SelectBeforeClose returns the checks to run before closing: the
// full list of the project tests folder plus every criterion check
// (Section 18.2).
func SelectBeforeClose(all []CriterionChecks, folder []types.Check) []types.Check {
	seen := map[types.CheckID]bool{}
	var out []types.Check
	for _, chk := range folder {
		if !seen[chk.ID] {
			seen[chk.ID] = true
			out = append(out, chk)
		}
	}
	for _, cc := range all {
		for _, chk := range cc.Checks {
			if !seen[chk.ID] {
				seen[chk.ID] = true
				out = append(out, chk)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Candidate is one solution a run generated, with the test outcomes
// that tell candidates apart.
type Candidate struct {
	ID string
	// Passed counts distinguishing inputs that pass; Failed counts
	// the ones that fail.
	Passed int
	Failed int
}

// SelectCandidate picks the winning solution by distinguishing
// inputs and test execution, never by a judge (Section 18.2).
// Candidates with any failure lose to clean ones; among clean ones
// the most passing inputs win. Ties break by id for determinism.
func SelectCandidate(cands []Candidate) (Candidate, bool) {
	if len(cands) == 0 {
		return Candidate{}, false
	}
	best := cands[0]
	bestKey := candidateKey(best)
	for _, c := range cands[1:] {
		if k := candidateKey(c); k > bestKey || (k == bestKey && c.ID < best.ID) {
			best = c
		}
	}
	return best, true
}

func candidateKey(c Candidate) int {
	if c.Failed > 0 {
		return -1<<30 + c.Passed - c.Failed
	}
	return c.Passed
}
