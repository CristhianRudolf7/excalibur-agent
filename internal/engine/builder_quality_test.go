package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func testSnapshotFunc() SnapshotFunc {
	return func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{
			Role: "child", HarnessVersion: "test",
			Task: ctxcompiler.TaskView{OriginalRequest: "do it"},
		}, nil
	}
}

// The Muse family variant: a muse-* model id selects the Muse
// working-method note and the adapted exec/finish tool descriptions,
// while a GPT id keeps byte-identical definitions.
func TestBuilderMuseFamilyWiring(t *testing.T) {
	ctx := context.Background()
	run := testRun(Profile{})
	defs := func(context.Context, *Run) []providers.ToolDefinition {
		return []providers.ToolDefinition{
			{Name: "exec", Description: "run a command"},
			{Name: "read_file", Description: "read a file"},
			{Name: "finish", Description: "end the run"},
		}
	}
	newReq := func(model string, q *QualityConfig) (providers.ChatRequest, string) {
		b := &DefaultBuilder{
			Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
			Snapshot: testSnapshotFunc(), ToolDefs: func(*Run) []providers.ToolDefinition {
				return defs(ctx, nil)
			}, ReasoningEffort: "low", ModelID: model, Quality: q,
		}
		req, compiled, err := b.BuildRequest(ctx, run, false)
		if err != nil {
			t.Fatal(err)
		}
		return req, compiled.LayerText(ctxcompiler.LayerCore)
	}
	byName := func(req providers.ChatRequest) map[string]string {
		out := map[string]string{}
		for _, d := range req.Tools {
			out[d.Name] = d.Description
		}
		return out
	}
	museReq, museL0 := newReq("muse-spark-1.3", nil)
	if !strings.Contains(museL0, "Finish only via the finish tool") {
		t.Error("Muse L0 must carry the Muse family note")
	}
	museDefs := byName(museReq)
	if museDefs["exec"] == "run a command" || !strings.Contains(museDefs["exec"], "call order") {
		t.Errorf("Muse exec definition must carry the API discipline, got %q", museDefs["exec"])
	}
	if museDefs["finish"] == "end the run" || !strings.Contains(museDefs["finish"], "ONLY through this tool") {
		t.Errorf("Muse finish definition must carry the finish-only rule, got %q", museDefs["finish"])
	}
	if museDefs["read_file"] != "read a file" {
		t.Errorf("Muse must leave other tool definitions unchanged, got %q", museDefs["read_file"])
	}
	// GPT stays byte-identical with adaptation on.
	gptReq, gptL0 := newReq("gpt-6-luna", nil)
	if strings.Contains(gptL0, "Finish only via the finish tool") {
		t.Error("GPT L0 must not carry the Muse note")
	}
	for name, want := range map[string]string{"exec": "run a command", "read_file": "read a file", "finish": "end the run"} {
		if got := byName(gptReq)[name]; got != want {
			t.Errorf("GPT tool %q changed to %q", name, got)
		}
	}
	// Adapt-off restores identical definitions for Muse.
	off := QualityConfig{WorkingMethod: true, FamilyAdapt: false, WorkingNotes: true,
		LoopNudges: true, StuckRecovery: true, RepeatBlock: true, FinishGuard: true,
		MaxGuardTrips: 2, MaxNotes: 24}
	plainReq, plainL0 := newReq("muse-spark-1.3", &off)
	for name, want := range map[string]string{"exec": "run a command", "read_file": "read a file", "finish": "end the run"} {
		if got := byName(plainReq)[name]; got != want {
			t.Errorf("adapt-off Muse tool %q changed to %q", name, got)
		}
	}
	if strings.Contains(plainL0, "Finish only via the finish tool") {
		t.Error("adapt-off Muse L0 must not carry the Muse note")
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
