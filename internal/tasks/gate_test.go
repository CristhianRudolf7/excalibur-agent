// Gate tests (Section 7.5; verification row 7.7.1): a gate with a
// criterion without evidence, stale evidence, a failed test, a
// paused child, a later instruction revision, an inspection
// judgement without a verifier, or a user_confirmation without an
// answer never completes; after two attempts the state is honest;
// a later confirmation closes its criterion without redoing work.
package tasks

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func gateCriterion(code string, method types.CriterionMethod, state types.CriterionState, kind EvidenceKind) GateCriterion {
	ref, _ := types.ParseCriterionRef(code)
	return GateCriterion{
		Criterion: types.Criterion{Code: ref, Method: method},
		State:     state, Kind: kind,
	}
}

func passingGate() GateInput {
	return GateInput{Level: types.TaskN1, ContrastRequired: false, VerifierRequired: false}
}

func TestGateBlocksEveryDefect(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*GateInput)
		want string
	}{
		{"no evidence", func(in *GateInput) {
			in.Criteria = []GateCriterion{gateCriterion("C1.1", types.CriterionTest, types.CriterionUnverified, EvidenceExecutable)}
		}, "C1.1 without evidence"},
		{"stale evidence", func(in *GateInput) {
			c := gateCriterion("C1.2", types.CriterionTest, types.CriterionUnverified, EvidenceExecutable)
			c.Stale = true
			in.Criteria = []GateCriterion{c}
		}, "stale"},
		{"failed test", func(in *GateInput) {
			in.Criteria = []GateCriterion{gateCriterion("C1.3", types.CriterionTest, types.CriterionFailed, EvidenceExecutable)}
		}, "C1.3 without evidence"},
		{"paused child", func(in *GateInput) {
			in.Children = []GateChild{{ID: "run_x", State: types.RunPaused}}
		}, "resume it"},
		{"later instruction revision", func(in *GateInput) {
			in.InstructionsRevSeen = 2
			in.InstructionsCurrent = 3
		}, "instruction revision"},
		{"inspection by judgement without verifier", func(in *GateInput) {
			in.Level = types.TaskN2
			in.Criteria = []GateCriterion{gateCriterion("C2.1", types.CriterionInspection, types.CriterionSatisfied, EvidenceJudgment)}
		}, "verifier to agree"},
		{"confirmation without answer", func(in *GateInput) {
			in.Criteria = []GateCriterion{gateCriterion("C1.4", types.CriterionUserConfirmation, types.CriterionUnverified, EvidenceConfirmation)}
		}, "pending your confirmation"},
		{"code criterion on judgement", func(in *GateInput) {
			in.Criteria = []GateCriterion{gateCriterion("C1.5", types.CriterionCommand, types.CriterionSatisfied, EvidenceJudgment)}
		}, "never counts on judgment"},
		{"missing contrast", func(in *GateInput) {
			in.ContrastRequired = true
		}, "contrast"},
		{"missing verifier verdict", func(in *GateInput) {
			in.VerifierRequired = true
		}, "verifier verdict"},
	}
	for _, tc := range cases {
		in := passingGate()
		tc.mut(&in)
		rep := Evaluate(in)
		if rep.OK {
			t.Fatalf("%s: gate must not complete", tc.name)
		}
		if !strings.Contains(strings.ToLower(CompactNote(rep)), strings.ToLower(tc.want)) {
			t.Fatalf("%s: pending %q must mention %q", tc.name, CompactNote(rep), tc.want)
		}
	}
}

func TestGatePassesClean(t *testing.T) {
	in := passingGate()
	in.Criteria = []GateCriterion{
		gateCriterion("C1.1", types.CriterionTest, types.CriterionSatisfied, EvidenceExecutable),
		{Criterion: types.Criterion{Code: mustRef("C1.2"), Method: types.CriterionUserConfirmation},
			State: types.CriterionNotApplicable, Justification: "out of scope for this task"},
	}
	in.Children = []GateChild{{ID: "run_x", State: types.RunCompleted}}
	if rep := Evaluate(in); !rep.OK {
		t.Fatalf("clean gate must pass: %v", rep.Pendings)
	}
}

func TestGateAttemptsThenHonestState(t *testing.T) {
	in := passingGate()
	in.Criteria = []GateCriterion{gateCriterion("C1.1", types.CriterionTest, types.CriterionFailed, EvidenceExecutable)}
	first := Evaluate(in)
	if first.OK {
		t.Fatal("failing gate must not pass")
	}
	second := Evaluate(in) // engine retries with the injected note
	if second.OK {
		t.Fatal("still failing after two attempts")
	}
	if got := HonestState(second); got != types.RunCompletedPending {
		t.Fatalf("honest state must be completed_with_pending, got %q", got)
	}
}

func TestHonestStateBlockedOnDisagreement(t *testing.T) {
	rep := GateReport{OK: false, Pendings: []string{"verifier disagreement on C2.1: evidence missing"}}
	if got := HonestState(rep); got != types.RunBlocked {
		t.Fatalf("verifier disagreement must block, got %q", got)
	}
}

func TestLaterConfirmationClosesWithoutRedo(t *testing.T) {
	in := passingGate()
	c := gateCriterion("C1.4", types.CriterionUserConfirmation, types.CriterionUnverified, EvidenceConfirmation)
	in.Criteria = []GateCriterion{c}
	if rep := Evaluate(in); rep.OK || !ConfirmationOnly(rep) {
		t.Fatalf("confirmation-only gate must pend, not fail: %+v", rep)
	}
	in.Criteria[0].ConfirmationReceived = true
	if rep := Evaluate(in); !rep.OK {
		t.Fatalf("later confirmation must close the criterion: %v", rep.Pendings)
	}
}

func TestInspectionJudgementWithVerifierAgreement(t *testing.T) {
	in := passingGate()
	in.Level = types.TaskN2
	c := gateCriterion("C2.1", types.CriterionInspection, types.CriterionSatisfied, EvidenceJudgment)
	c.VerifierAgreed = true
	in.Criteria = []GateCriterion{c}
	if rep := Evaluate(in); !rep.OK {
		t.Fatalf("agreed judgement must count in N2: %v", rep.Pendings)
	}
}

func mustRef(s string) types.CriterionRef {
	ref, err := types.ParseCriterionRef(s)
	if err != nil {
		panic(err)
	}
	return ref
}
