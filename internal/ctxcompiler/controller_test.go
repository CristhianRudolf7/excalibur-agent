package ctxcompiler_test

import (
	"context"
	"strings"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Long synthetic run: history grows past the full-window trigger,
// masking fires first (batch toward target), then compaction fires
// (state-only, no model call) and the recompiled request stays under
// the model's max input. L0-L2 never move, so the provider prefix
// cache survives except at the batch/compaction boundary.
func TestControllerLongRunCompactsUnderMaxInput(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	cfg.ModelLimits = map[string]ctxcompiler.ModelLimits{
		"test-small": {ContextWindow: 10000, MaxInput: 8000, MaxOutput: 1000},
	}
	cfg.CompactionReserveTokens = 500
	cfg.MaskTriggerTokens = 2000
	cfg.MaskTargetTokens = 1000
	cfg.LiteralObservations = 2
	cfg.PostCompactionLiteralTokens = 1500
	counter := ctxcompiler.NewEstimator(4)
	ctrl := ctxcompiler.NewController(cfg, counter, "test-small")
	if got := ctrl.Threshold(); got != 8000-500-800 {
		t.Fatalf("small-model threshold = %d, want %d", got, 8000-500-800)
	}
	// Build a long activity: 40 tool results, ~400 chars (~100
	// tokens) each, plus assistant call stubs. Total well past both
	// the masking trigger and the compaction trigger.
	var activity []ctxcompiler.ActivityEvent
	seq := int64(1)
	for i := 0; i < 40; i++ {
		callID := strings.Repeat("c", 8) + string(rune('a'+i%26)) + string(rune('0'+i/26))
		body := strings.Repeat("x", 400)
		activity = append(activity, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleAssistant, Content: "do step",
			ToolCalls: []providers.ToolCall{{ID: callID, Name: "exec", Arguments: []byte(`{}`)}},
		})
		seq++
		activity = append(activity, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleTool, Content: body,
			ToolResult: &providers.ToolResult{CallID: callID, Name: "exec", Output: body},
		})
		seq++
	}
	snap := ctxcompiler.Snapshot{
		Role: "child", HarnessVersion: "test",
		Task: ctxcompiler.TaskView{OriginalRequest: "synthetic long run"},
	}
	compiled, err := ctxcompiler.Compile(cfg, snapWithActivity(snap, activity), counter)
	if err != nil {
		t.Fatal(err)
	}
	prefixBefore := [3]string{
		compiled.LayerHash(ctxcompiler.LayerCore),
		compiled.LayerHash(ctxcompiler.LayerIdentity),
		compiled.LayerHash(ctxcompiler.LayerScope),
	}
	// Masking must fire: 40 unmasked observations at ~100 tokens
	// each dwarf the 2K trigger.
	maskOut := ctrl.ApplyMask(activity, nil, false)
	if !maskOut.Applied {
		t.Fatal("masking must fire on the synthetic history")
	}
	if maskOut.Remaining > cfg.MaskTargetTokens+500 {
		t.Fatalf("masking must drop toward target, left %d", maskOut.Remaining)
	}
	// Newest observations stay literal.
	maskedCount := 0
	for _, e := range activity {
		if e.ToolResult != nil && strings.Contains(e.ToolResult.Output, "get_tool_output") {
			maskedCount++
		}
	}
	if maskedCount == 0 {
		t.Fatal("masked markers must appear in activity")
	}
	// Compaction must fire on the masked total: the estimate is the
	// full compiled size, and the output stays under max input.
	maskedSnap := snapWithActivity(snap, activity)
	maskedSnap.CompactionBlock = ctrl.Block()
	compiledMasked, err := ctxcompiler.Compile(cfg, maskedSnap, counter)
	if err != nil {
		t.Fatal(err)
	}
	if !ctrl.NeedsCompaction(compiledMasked.TotalTokens + 6000) {
		t.Fatalf("synthetic total %d must trip the %d trigger", compiledMasked.TotalTokens+6000, ctrl.Threshold())
	}
	state := ctxcompiler.StateFromTask(snap.Task)
	outcome, kept, err := ctrl.ApplyCompact(context.Background(), state, activity, compiledMasked.TotalTokens+6000)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Applied {
		t.Fatal("compaction must fire past the trigger")
	}
	finalSnap := snapWithActivity(snap, kept)
	finalSnap.CompactionBlock = outcome.Block
	final, err := ctxcompiler.Compile(cfg, finalSnap, counter)
	if err != nil {
		t.Fatal(err)
	}
	lim := ctxcompiler.LimitsForModel("test-small", cfg)
	if final.TotalTokens >= lim.MaxInput {
		t.Fatalf("compacted total %d must stay under max input %d", final.TotalTokens, lim.MaxInput)
	}
	// Cache prefix stable: L0-L2 hashes unchanged across
	// masking+compaction (only L3 gains the block and L4 shrinks).
	for i, l := range []ctxcompiler.Layer{ctxcompiler.LayerCore, ctxcompiler.LayerIdentity, ctxcompiler.LayerScope} {
		if got := final.LayerHash(l); got != prefixBefore[i] {
			t.Fatalf("L%d must stay stable across control, changed", int(l))
		}
	}
	if !strings.Contains(outcome.Block, "Compaction") || outcome.KeptEvents == 0 {
		t.Fatalf("compaction block must render with a kept tail: %+v", outcome)
	}
}

func snapWithActivity(base ctxcompiler.Snapshot, activity []ctxcompiler.ActivityEvent) ctxcompiler.Snapshot {
	base.Activity = append([]ctxcompiler.ActivityEvent(nil), activity...)
	return base
}
