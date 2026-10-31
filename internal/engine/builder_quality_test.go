package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

func testSnapshotFunc() SnapshotFunc {
	return func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{
			Role: "child", HarnessVersion: "test",
			Task: ctxcompiler.TaskView{OriginalRequest: "do it"},
		}, nil
	}
}

// The builder renders the working method, the family variant and the
// pinned notes; every piece switches off independently.
func TestBuilderQualityWiring(t *testing.T) {
	ctx := context.Background()
	run := testRun(Profile{})
	run.addFact("did read_file main.go", 24)
	b := &DefaultBuilder{
		Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
		Snapshot: testSnapshotFunc(), ReasoningEffort: "low", ModelID: "qwen3-max",
	}
	req, compiled, err := b.BuildRequest(ctx, run, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) == 0 {
		t.Fatal("empty request")
	}
	l0 := compiled.LayerText(ctxcompiler.LayerCore)
	if !strings.Contains(l0, "working method:") {
		t.Error("L0 must carry the working method by default")
	}
	if !strings.Contains(l0, "work in English") {
		t.Error("L0 must carry the Qwen family variant for ModelID qwen3-max")
	}
	l3 := compiled.LayerText(ctxcompiler.LayerTask)
	if !strings.Contains(l3, "did read_file main.go") {
		t.Error("L3 must carry the pinned working notes")
	}
	if req.ReasoningEffort != "low" {
		t.Errorf("reasoning effort must pass through untouched, got %q", req.ReasoningEffort)
	}
	// Ablation: everything off restores the legacy bytes.
	off := QualityConfig{}
	plain := &DefaultBuilder{
		Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
		Snapshot: testSnapshotFunc(), ReasoningEffort: "low", ModelID: "qwen3-max",
		Quality: &off,
	}
	_, legacy, err := plain.BuildRequest(ctx, run, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(legacy.LayerText(ctxcompiler.LayerCore), "working method:") {
		t.Error("WorkingMethod=false must drop the method block")
	}
	if strings.Contains(legacy.LayerText(ctxcompiler.LayerTask), "working notes:") {
		t.Error("WorkingNotes=false must drop the notes section")
	}
	// A snapshot-level model id wins over the builder default.
	both := &DefaultBuilder{
		Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
		Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
			snap, _ := testSnapshotFunc()(context.Background(), nil)
			snap.ModelID = "gpt-6-luna"
			return snap, nil
		},
		ReasoningEffort: "low", ModelID: "qwen3-max",
	}
	_, gptCompiled, err := both.BuildRequest(ctx, run, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gptCompiled.LayerText(ctxcompiler.LayerCore), "batch independent read-only calls") {
		t.Error("snapshot ModelID must win over the builder default")
	}
}
