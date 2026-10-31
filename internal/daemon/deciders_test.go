package daemon

import (
	"context"
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
