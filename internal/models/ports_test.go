// Decider ports tests (Sections 2.1, 5.4, 6.1, 6.4, 10.5, 12.3,
// 12.4, 13.3, 18.3): every port builds bounded questions, the
// deterministic bands decide without a model, and the decider only
// fires in genuine ambiguity.
package models

import (
	"context"
	"testing"
)

func TestTriageQuestionsCoverPorts(t *testing.T) {
	qs := TriageQuestions("pause Lena", "active runs: Lena(tests)", []string{"general", "gastos"})
	if len(qs) != 4 {
		t.Fatalf("triage questions = %d, want 4", len(qs))
	}
	kinds := map[QuestionKind]int{}
	for _, q := range qs {
		if err := q.Validate(); err != nil {
			t.Fatalf("triage question invalid: %v", err)
		}
		kinds[q.Kind]++
	}
	if kinds[KindChoice] != 2 || kinds[KindNoul] != 2 {
		t.Fatalf("triage kinds = %v, want 2 choice + 2 noul", kinds)
	}
	// Fast lane: resolved control query with high confidence acts.
	if !FastLaneClass("status_query", true, 0.9, DefaultThresholds()) {
		t.Fatal("resolved query with high confidence should use the fast lane")
	}
	if FastLaneClass("conversation", true, 0.9, DefaultThresholds()) {
		t.Fatal("open conversation must not use the fast lane")
	}
	if FastLaneClass("status_query", false, 0.9, DefaultThresholds()) {
		t.Fatal("ambiguous objective must not use the fast lane")
	}
}

func TestShouldAskRules(t *testing.T) {
	if !ShouldAsk(0.99, true, false, false) {
		t.Fatal("mandatory data without default always asks")
	}
	if !ShouldAsk(0.99, false, true, false) {
		t.Fatal("irreversible action always asks")
	}
	if !ShouldAsk(0.6, false, false, true) {
		t.Fatal("p < 0.7 with costly rework asks")
	}
	if ShouldAsk(0.8, false, false, true) {
		t.Fatal("p >= 0.7 with costly rework assumes")
	}
	if ShouldAsk(0.1, false, false, false) {
		t.Fatal("trivial doubt assumes")
	}
}

func TestAdmissionBands(t *testing.T) {
	if class, needs := ClassifyAdmission(0.97, true); needs || class != AdmissionDuplicate {
		t.Fatalf("0.97 same entity: %q needs=%v, want duplicate without decider", class, needs)
	}
	if class, needs := ClassifyAdmission(0.5, true); needs || class != AdmissionNew {
		t.Fatalf("0.5: %q needs=%v, want new without decider", class, needs)
	}
	for _, sim := range []float64{0.80, 0.86, 0.93, 0.949} {
		if _, needs := ClassifyAdmission(sim, true); !needs {
			t.Fatalf("sim %.2f must need the decider (ambiguous band)", sim)
		}
	}
	q := AdmissionQuestion("lives in Berlin", "lives in Berlin!")
	if err := q.Validate(); err != nil {
		t.Fatalf("admission question: %v", err)
	}
	// The ambiguous band classifies through the decider.
	fake := NewFakeDecider(0.9)
	ans := MustAsk(t, fake, context.Background(), AdmissionQuestion("a", "b"))
	if ans.Choice == "" {
		t.Fatal("admission answer needs a class")
	}
}

func TestStuckJudgeGate(t *testing.T) {
	if _, ok := StuckQuestion(25, "state"); ok {
		t.Fatal("judge before step 30 must not run")
	}
	if _, ok := StuckQuestion(35, "state"); ok {
		t.Fatal("judge off the every-10 grid must not run")
	}
	q, ok := StuckQuestion(30, "state")
	if !ok {
		t.Fatal("judge at step 30 must run")
	}
	if err := q.Validate(); err != nil {
		t.Fatalf("stuck question: %v", err)
	}
	if !CountsStuck(0.9) || CountsStuck(0.89) {
		t.Fatal("stuck counts only at p >= 0.9")
	}
}

func TestNotifyAndRankPorts(t *testing.T) {
	for _, q := range []Question{
		NotifyQuestion("run blocked"),
		SkillScoreQuestion("root-cause-first", "fix a bug"),
		ToolRerankQuestion("code_graph", "who calls f"),
	} {
		if err := q.Validate(); err != nil {
			t.Fatalf("port question invalid: %v", err)
		}
	}
	fake := NewFakeDecider(0.95, 0.8, 0.5)
	th := DefaultThresholds()
	ans := MustAsk(t, fake, context.Background(), NotifyQuestion("run blocked"))
	if Decide(ans.Prob, th) != OutcomeAct {
		t.Fatal("high notify probability should act")
	}
}
