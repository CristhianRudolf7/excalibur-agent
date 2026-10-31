// Verification ladder and selection tests (Sections 18.1, 18.2;
// verification rows 18.5.1 and 18.5.2): a code criterion never
// counts on judgement, an inspection judgement in N2 needs the
// verifier to agree, impacted tests run during the work and the full
// list before closing, and candidates win on tests, never on judges.
package tasks

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestCodeCriterionNeverOnJudgement(t *testing.T) {
	for _, m := range []types.CriterionMethod{types.CriterionCommand, types.CriterionTest, types.CriterionMetric} {
		_ = m
	}
	if CodeCriterionSatisfiedBy(EvidenceJudgment) {
		t.Fatal("a code criterion must never count on judgement")
	}
	if !CodeCriterionSatisfiedBy(EvidenceExecutable) || !CodeCriterionSatisfiedBy(EvidenceIndependent) {
		t.Fatal("executable and independent evidence must satisfy code criteria")
	}
}

func TestInspectionJudgementNeedsVerifierInN2(t *testing.T) {
	if InspectionSatisfiedBy(types.TaskN2, EvidenceJudgment, false) {
		t.Fatal("N2 inspection on judgement needs verifier agreement")
	}
	if !InspectionSatisfiedBy(types.TaskN2, EvidenceJudgment, true) {
		t.Fatal("agreed judgement must satisfy N2 inspection")
	}
	if !InspectionSatisfiedBy(types.TaskN1, EvidenceJudgment, false) {
		t.Fatal("N1 inspection on judgement counts alone")
	}
	if LadderRank(EvidenceExecutable) >= LadderRank(EvidenceJudgment) {
		t.Fatal("executable evidence must outrank judgement")
	}
}

func seedChecks() ([]CriterionChecks, types.Check, types.Check) {
	c1 := types.Criterion{ID: "crt_impacted", ImpactArea: []string{"pdf/export.go"}}
	c2 := types.Criterion{ID: "crt_progress", ImpactArea: []string{"other.go"}}
	c3 := types.Criterion{ID: "crt_idle", ImpactArea: []string{"idle.go"}}
	chkImpacted := types.Check{ID: "chk_impacted", CriterionID: c1.ID, Definition: "pytest export"}
	chkProgress := types.Check{ID: "chk_progress", CriterionID: c2.ID, Definition: "pytest other"}
	chkUnrelated := types.Check{ID: "chk_unrelated", CriterionID: c3.ID, Definition: "pytest slow"}
	return []CriterionChecks{
		{Criterion: c1, Checks: []types.Check{chkImpacted}},
		{Criterion: c2, Checks: []types.Check{chkProgress}},
		{Criterion: c3, Checks: []types.Check{chkUnrelated}},
	}, chkImpacted, chkProgress
}

func TestSelectDuringWork(t *testing.T) {
	all, impacted, progress := seedChecks()
	inProgress := map[types.CriterionID]bool{"crt_progress": true}
	got := SelectDuringWork(all, []string{"pdf/export.go"}, inProgress)
	ids := map[types.CheckID]bool{}
	for _, c := range got {
		ids[c.ID] = true
	}
	if !ids[impacted.ID] || !ids[progress.ID] {
		t.Fatalf("impacted and in-progress checks must run: %v", got)
	}
	if ids["chk_unrelated"] {
		t.Fatalf("unrelated check must wait: %v", got)
	}
}

func TestSelectBeforeClose(t *testing.T) {
	all, _, _ := seedChecks()
	folder := []types.Check{{ID: "chk_folder", Definition: "make test"}}
	got := SelectBeforeClose(all, folder)
	if len(got) != 4 {
		t.Fatalf("close runs the full list: %v", got)
	}
}

func TestSelectCandidateByTests(t *testing.T) {
	cands := []Candidate{
		{ID: "b", Passed: 10, Failed: 0},
		{ID: "a", Passed: 12, Failed: 1},
		{ID: "c", Passed: 8, Failed: 0},
	}
	best, ok := SelectCandidate(cands)
	if !ok || best.ID != "b" {
		t.Fatalf("any failure loses to a clean run: %+v", best)
	}
	if _, ok := SelectCandidate(nil); ok {
		t.Fatal("no candidates must not select")
	}
}
