package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
)

// TestMemoryJudgeBands covers the Table 2.1 admission row: the
// calibrated judge classifies the ambiguous pair, abstains to new at
// the bottom and falls back to structure on defer or failure.
func TestMemoryJudgeBands(t *testing.T) {
	ctx := context.Background()
	cand := memory.Candidate{Statement: "Ana promised the report by Friday"}
	old := memory.Memory{Statement: "Ana promised the report by Friday"}
	judge := NewMemoryJudge(models.NewFakeDecider(0.99))
	class, err := judge.Classify(ctx, cand, old)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if class != "duplicate" {
		t.Fatalf("want duplicate at p=0.99, have %q", class)
	}
	// Abstention band: low probability takes the alternative (new).
	low := NewMemoryJudge(models.NewFakeDecider(0.1))
	classLow, err := low.Classify(ctx, cand, old)
	if err != nil {
		t.Fatalf("classify low: %v", err)
	}
	if classLow != "new" {
		t.Fatalf("abstention should take the alternative, have %q", classLow)
	}
	// Nil decider: deterministic default still answers.
	nilJudge := NewMemoryJudge(nil)
	if _, err := nilJudge.Classify(ctx, cand, old); err != nil {
		t.Fatalf("nil judge: %v", err)
	}
}

// TestStuckAndNotifyJudges covers the Table 2.1 loop and notify rows.
func TestStuckAndNotifyJudges(t *testing.T) {
	ctx := context.Background()
	// Before step 30 the judge never fires.
	if JudgeStuck(ctx, models.NewFakeDecider(0.99), 10, "working") {
		t.Fatal("stuck judge fired before step 30")
	}
	if !JudgeStuck(ctx, models.NewFakeDecider(0.95), 30, "same error 3 times") {
		t.Fatal("stuck judge did not fire at step 30 with p=0.95")
	}
	if JudgeStuck(ctx, models.NewFakeDecider(0.5), 40, "same error") {
		t.Fatal("stuck judge fired below 0.9")
	}
	if JudgeStuck(ctx, nil, 30, "state") {
		t.Fatal("nil decider stuck")
	}
	if !JudgeNotify(ctx, models.NewFakeDecider(0.95), "build failed") {
		t.Fatal("notify judge did not fire on high probability")
	}
	if JudgeNotify(ctx, nil, "build failed") {
		t.Fatal("nil decider notified")
	}
}

// TestRerankToolsOrder covers the Section 13.3 rerank row: higher
// pertinence sorts first through the real ports questions.
func TestRerankToolsOrder(t *testing.T) {
	ctx := context.Background()
	dec := models.NewFakeDecider(0.9)
	got := RerankTools(ctx, dec, "convert docx to pdf", []string{"file_convert", "clock"})
	if len(got) != 2 {
		t.Fatalf("rerank dropped tools: %v", got)
	}
	if got2 := RerankTools(ctx, nil, "q", []string{"a", "b"}); len(got2) != 2 {
		t.Fatalf("nil rerank changed the set: %v", got2)
	}
}

type intakeChoiceRecorder struct {
	questions []models.Question
	answer    models.Answer
	err       error
}

func (r *intakeChoiceRecorder) Ask(_ context.Context, q models.Question) (models.Answer, error) {
	r.questions = append(r.questions, q)
	return r.answer, r.err
}

func TestIntakeSingleQuestion(t *testing.T) {
	for _, scopes := range [][]string{nil, {"general"}, {"general", "project:p1"}} {
		t.Run(fmt.Sprint(scopes), func(t *testing.T) {
			t.Setenv("EXCALIBUR_INTAKE_SINGLE_QUESTION", "true")
			dec := &intakeChoiceRecorder{answer: models.Answer{Choice: "quick_action", Prob: .95}}
			d := NewModelDecider(dec, scopes)
			pre := master.Preanalyze("pause R3", nil, time.Now(), time.UTC)
			tri := d.Classify(context.Background(), "pause R3", pre)
			want := 1
			if len(scopes) > 1 {
				want = 2
			}
			if len(dec.questions) != want || dec.questions[0].Prompt != "request type of the message" {
				t.Fatalf("questions=%+v, want %d", dec.questions, want)
			}
			if tri.Confidence != .95 || len(tri.DurableFrag) != 0 || tri.NeedsClarify {
				t.Fatalf("triage=%+v", tri)
			}
		})
	}
}

func TestIntakeSingleQuestionSwitch(t *testing.T) {
	for _, value := range []string{"", "true", "false", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("EXCALIBUR_INTAKE_SINGLE_QUESTION", value)
			dec := &intakeChoiceRecorder{answer: models.Answer{Choice: "conversation", Prob: .95}}
			tri := NewModelDecider(dec, nil).Classify(context.Background(), "Hello", master.Preanalysis{})
			want := 1
			if value == "false" {
				want = 4
			}
			if len(dec.questions) != want {
				t.Fatalf("got %d questions, want %d", len(dec.questions), want)
			}
			if value != "false" && (tri.NeedsClarify || len(tri.DurableFrag) != 0) {
				t.Fatalf("unused model metadata populated: %+v", tri)
			}
		})
	}
}

func TestIntakeClarificationUsesDeterministicSignals(t *testing.T) {
	t.Setenv("EXCALIBUR_INTAKE_SINGLE_QUESTION", "true")
	for _, text := range []string{"pause", "cancel R3"} {
		dec := &intakeChoiceRecorder{answer: models.Answer{Choice: "quick_action", Prob: .95}}
		pre := master.Preanalyze(text, nil, time.Now(), time.UTC)
		tri := NewModelDecider(dec, nil).Classify(context.Background(), text, pre)
		if !tri.NeedsClarify || len(dec.questions) != 1 {
			t.Fatalf("%s: %+v, questions=%d", text, tri, len(dec.questions))
		}
	}
}

func TestIntakeDeciderErrorUsesDeterministicTriage(t *testing.T) {
	dec := &intakeChoiceRecorder{err: errors.New("unavailable")}
	pre := master.Preanalysis{ControlIntent: "pause", ControlTarget: "R3"}
	tri := NewModelDecider(dec, nil).Classify(context.Background(), "pause R3", pre)
	if tri.Type != master.TriageStatusQuery || tri.Confidence != 1 || len(dec.questions) != 1 {
		t.Fatalf("triage=%+v, questions=%d", tri, len(dec.questions))
	}
}

func TestIntakeQuickActionRetainsItsType(t *testing.T) {
	dec := &intakeChoiceRecorder{answer: models.Answer{Choice: "quick_action", Prob: .99}}
	tri := NewModelDecider(dec, nil).Classify(context.Background(), "baja la pinza 5 cm", master.Preanalysis{})
	if tri.Type != master.TriageQuickAction {
		t.Fatalf("quick action became %s", tri.Type)
	}
}
