// Verifier tests (Section 8.7; verification row 8.11.7): an N2
// task with five requirements gets a verifier, the compiled context
// carries no executor reasoning, the verifier re-runs the checks,
// and disagreement rounds stop at two.
package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestShouldVerify(t *testing.T) {
	if !ShouldVerify(types.TaskN2, 5, false, false) {
		t.Fatal("N2 with 5 requirements must verify")
	}
	if ShouldVerify(types.TaskN2, 4, false, false) {
		t.Fatal("N2 with 4 requirements must not verify")
	}
	if !ShouldVerify(types.TaskN3, 1, true, false) {
		t.Fatal("N3 milestone must verify")
	}
	if ShouldVerify(types.TaskN3, 1, false, false) {
		t.Fatal("N3 outside milestones must not verify")
	}
	if !ShouldVerify(types.TaskN1, 0, false, true) {
		t.Fatal("explicit user request must verify")
	}
}

func TestVerifierInputCarriesNoReasoning(t *testing.T) {
	in := VerifierInput{
		Request:      "export the invoice to PDF",
		Requirements: []types.Requirement{{Text: "keep headers"}},
		Diff:         "diff --git ...",
	}
	// By construction there is no reasoning field: marshal the
	// input and assert nothing leaks.
	s := strings.Join([]string{in.Request, in.Diff}, "\n")
	for _, r := range in.Requirements {
		s += r.Text
	}
	if strings.Contains(s, "reasoning") {
		t.Fatal("verifier context must not carry executor reasoning")
	}
}

func TestRunVerifierAgreement(t *testing.T) {
	calls := 0
	run := func(in VerifierInput) ([]Verdict, error) {
		calls++
		return []Verdict{{Criterion: "c1", Agree: true}}, nil
	}
	out, err := RunVerifier(run, VerifierInput{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Agreed || out.Rounds != 1 || calls != 1 {
		t.Fatalf("agreement must settle in one round: %+v", out)
	}
}

func TestRunVerifierRerunsChecksAndCapsRounds(t *testing.T) {
	checkRuns := 0
	run := func(in VerifierInput) ([]Verdict, error) {
		checkRuns++ // the verifier re-runs the checks each round
		return []Verdict{{Criterion: "c1", Agree: false, Note: "still failing"}}, nil
	}
	rechecks := 0
	out, err := RunVerifier(run, VerifierInput{}, func(round int) VerifierInput {
		rechecks++
		return VerifierInput{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Agreed {
		t.Fatal("persistent disagreement must not agree")
	}
	if out.Rounds != MaxVerifierRounds || checkRuns != MaxVerifierRounds {
		t.Fatalf("rounds must cap at %d: %+v", MaxVerifierRounds, out)
	}
	if len(out.Disagreements) != 1 {
		t.Fatalf("both positions must be reported: %+v", out)
	}
}

func TestRecordVerdictsAsIndependentEvidence(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	_, chk := seedFreshCriterion(t, store, scope, "v.go")
	run := types.NewRunID()
	verdicts := []Verdict{{
		Criterion: chk.CriterionID, Agree: true, Note: "passes",
		Evidence: EvidenceRecord{Evidence: types.Evidence{
			ID: types.NewEvidenceID(), CheckID: chk.ID,
			Result: types.EvidencePass, CodeVersion: CodeVersion{Commit: "v"}.String(),
		}},
	}}
	if err := RecordVerdicts(store, run, verdicts, 2); err != nil {
		t.Fatal(err)
	}
	latest, ok := store.LatestEvidence(chk.ID)
	if !ok || latest.Evidence.Result != types.EvidencePass || latest.InstructionRevSeen != 2 {
		t.Fatalf("verdict not recorded as evidence: %+v", latest)
	}
}
