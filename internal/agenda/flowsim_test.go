package agenda

import (
	"fmt"
	"strings"
	"testing"
)

// 21.6/21.10: the flow-sim suite — prior count, decision and pair
// coverage, exhaustive-only-under-limit, lossless descriptors, planted
// errors detected.

const simFlow = `D1 decision:payment approved -> yes: N4 | no: N7
N4 step:reserve stock -> D2
D2 decision:express shipping -> yes: N5 | no: N6
N5 step:ship express -> END
N6 step:ship standard -> END
N7 step:notify customer -> END
END end:done`

func mustFlow(t *testing.T, id string, extra ...string) *CanonicalFlow {
	t.Helper()
	lines := append([]string{}, strings.Split(simFlow, "\n")...)
	lines = append(lines, extra...)
	f, err := ParseCanonicalFlow(id, lines)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSimCountPrecedes(t *testing.T) {
	f := mustFlow(t, "orders")
	if got := f.CountWalkthroughs(2); got != 3 {
		t.Fatalf("want 3 walkthroughs, counted %d", got)
	}
}

func TestSimDecisionCoverage(t *testing.T) {
	f := mustFlow(t, "orders")
	rep, err := Simulate(f, CoverageDecisions, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Exhaustive {
		t.Fatal("decision coverage is not exhaustive")
	}
	// Every outcome appears at least once.
	for _, want := range []string{"D1=yes", "D1=no", "D2=yes", "D2=no"} {
		if len(rep.DecisionCoverage[want]) == 0 {
			t.Fatalf("outcome %q uncovered: %v", want, rep.DecisionCoverage)
		}
	}
	if !rep.DescriptorRoundtrip {
		t.Fatal("descriptors must regenerate their walkthroughs")
	}
	if len(rep.Narrations) != len(rep.Walkthroughs) {
		t.Fatal("one narration per walkthrough")
	}
}

func TestSimExhaustive(t *testing.T) {
	f := mustFlow(t, "orders")
	rep, err := Simulate(f, CoverageFull, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Exhaustive || rep.Total != 3 || len(rep.Walkthroughs) != 3 {
		t.Fatalf("exhaustive report wrong: %+v", rep)
	}
}

func TestSimExhaustiveRefusedOverLimit(t *testing.T) {
	// A chain of 11 binary decisions: 2^11 = 2048 walkthroughs.
	var lines []string
	for i := 0; i < 11; i++ {
		next := "END"
		if i < 10 {
			next = fmt.Sprintf("D%d", i+1)
		}
		lines = append(lines, fmt.Sprintf("D%d decision:q%d -> yes: %s | no: %s", i, i, next, next))
	}
	lines = append(lines, "END end:done")
	f, err := ParseCanonicalFlow("wide", lines)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.CountWalkthroughs(1); got <= ExhaustiveLimit {
		t.Fatalf("fixture must exceed the limit, counted %d", got)
	}
	if _, err := Simulate(f, CoverageFull, 1); err == nil {
		t.Fatal("exhaustive over the limit must be refused")
	}
	rep, err := Simulate(f, CoveragePairs, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Exhaustive {
		t.Fatal("pair coverage is sampled, never exhaustive")
	}
	if len(rep.PairCoverage) == 0 {
		t.Fatal("pair coverage must index pairs")
	}
}

func TestSimLoopUnfoldAndRoundtrip(t *testing.T) {
	lines := []string{
		"D1 decision:items left -> yes: B1 | no: END",
		"B1 loop:retry up to 3 -> body: R1 -> END",
		"R1 decision:retry helps -> yes: N1 | no: N2",
		"N1 step:fix and continue -> END",
		"N2 step:escalate -> END",
		"END end:done",
	}
	f, err := ParseCanonicalFlow("loops", lines)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.CountWalkthroughs(2); got < 4 {
		t.Fatalf("loop unfolding must multiply walkthroughs, counted %d", got)
	}
	rep, err := Simulate(f, CoverageFull, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DescriptorRoundtrip {
		t.Fatal("loop descriptors must round-trip")
	}
	found := false
	for _, w := range rep.Walkthroughs {
		for _, c := range w.Choices {
			if strings.HasPrefix(c, "B1x") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("turn markers must appear in descriptors")
	}
}

func TestSimPlantedErrorsDetected(t *testing.T) {
	lines := []string{
		"D1 decision:payment approved -> yes: N4 | no: N7",
		"N4 step:charge card -> END",
		"N7 step:notify customer -> END",
		"END end:done",
	}
	f, err := ParseCanonicalFlow("errs", lines)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := f.FlowByID("N4")
	n.Kind = FlowErrorKind
	n.ErrorHere = "card declined (planted)"
	rep, err := Simulate(f, CoverageFull, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range rep.Findings {
		if strings.Contains(finding, "planted") {
			found = true
		}
	}
	if !found {
		t.Fatalf("planted errors must be reported: %v", rep.Findings)
	}
}

func TestSimDeadEndFinding(t *testing.T) {
	lines := []string{
		"D1 decision:go -> yes: STUCK | no: END",
		"STUCK step:nowhere -> ",
		"END end:done",
	}
	f, err := ParseCanonicalFlow("dead", lines)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := f.FlowByID("STUCK")
	n.NoExit = true
	rep, err := Simulate(f, CoverageFull, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range rep.Findings {
		if strings.Contains(finding, "dead end") {
			found = true
		}
	}
	if !found {
		t.Fatalf("dead ends must be reported: %v", rep.Findings)
	}
}

func TestSimNeverVaryingDecision(t *testing.T) {
	f := mustFlow(t, "vary")
	// Hand-made walkthroughs where D1 only ever takes "yes".
	ws := []Walkthrough{
		{Choices: []string{"D1=yes", "D2=yes"}, Visits: []string{"D1", "N4", "D2", "N5", "END"}},
		{Choices: []string{"D1=yes", "D2=no"}, Visits: []string{"D1", "N4", "D2", "N6", "END"}},
	}
	findings := Review(f, ws)
	found := false
	for _, finding := range findings {
		if strings.Contains(finding, "D1") && strings.Contains(finding, "never varies") {
			found = true
		}
	}
	if !found {
		t.Fatalf("single-outcome decisions must be flagged: %v", findings)
	}
}
