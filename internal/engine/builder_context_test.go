package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// Embedded BuildRequest invokes masking and compaction (owner
// decision 0051): the threshold helpers were previously test-only.
// L0-L2 stay stable so the provider prefix cache never churns
// except at the batch/compaction boundary.
func TestBuilderInvokesMaskAndCompact(t *testing.T) {
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

	run := NewRun("run_ctx_1", "task_ctx_1", Profile{
		Name: "child", ContextRole: "child", Limit: tasks.Limit{MaxSteps: 50},
	})
	// Long tool history: 40 results at ~100 tokens each.
	seq := run.nextHistorySeq()
	for i := 0; i < 40; i++ {
		callID := "tc_ctx_" + itoa(i)
		body := strings.Repeat("y", 400)
		run.mu.Lock()
		run.History = append(run.History, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleTool, Content: body,
			ToolResult: &providers.ToolResult{CallID: callID, Name: "exec", Output: body},
		})
		run.mu.Unlock()
		seq++
	}
	var masks, compacts int
	b := &DefaultBuilder{
		Config:  cfg,
		Counter: counter,
		Snapshot: func(ctx context.Context, r *Run) (ctxcompiler.Snapshot, error) {
			return ctxcompiler.Snapshot{
				Role: "child", HarnessVersion: "test",
				Task:     ctxcompiler.TaskView{OriginalRequest: "builder context control"},
				Volatile: ctxcompiler.Volatile{Now: time.Now().UTC()},
			}, nil
		},
		ModelID: "test-small",
		OnMask: func(batch, dropped int, note string) {
			masks++
		},
		OnCompact: func(from, to, kept int, note string) {
			compacts++
		},
	}
	req, compiled, err := b.BuildRequest(context.Background(), run, false)
	if err != nil {
		t.Fatal(err)
	}
	if masks == 0 {
		t.Fatal("BuildRequest must invoke masking on the long history")
	}
	if compacts == 0 {
		// The synthetic history is large but the compiled total may
		// still sit below the trigger once masked; force a second
		// pass with an artificially small limit to prove the
		// compaction seam fires and stays under max input.
		small := cfg
		small.ModelLimits = map[string]ctxcompiler.ModelLimits{
			"test-small": {ContextWindow: 4000, MaxInput: 3000, MaxOutput: 500},
		}
		b.Config = small
		_, compiled2, err := b.BuildRequest(context.Background(), run, false)
		if err != nil {
			t.Fatal(err)
		}
		if compacts == 0 {
			t.Fatalf("BuildRequest must invoke compaction past the trigger (total %d)", compiled2.TotalTokens)
		}
		lim := ctxcompiler.LimitsForModel("test-small", small)
		if compiled2.TotalTokens >= lim.MaxInput {
			t.Fatalf("compacted total %d must stay under max input %d", compiled2.TotalTokens, lim.MaxInput)
		}
	}
	lim := ctxcompiler.LimitsForModel("test-small", cfg)
	if compiled.TotalTokens >= lim.MaxInput {
		t.Fatalf("built request %d must stay under max input %d", compiled.TotalTokens, lim.MaxInput)
	}
	if len(req.Messages) == 0 {
		t.Fatal("request must carry messages")
	}
	// Prefix stability is covered at the compiler level (L0-L2
	// hashes); here we assert the run persisted control state.
	if run.MaskBatches == 0 {
		t.Fatal("run must record masking batches")
	}
}

// Switches disable each mechanism for ablations.
func TestBuilderContextSwitchesOff(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	counter := ctxcompiler.NewEstimator(4)
	run := NewRun("run_ctx_2", "task_ctx_2", Profile{
		Name: "child", ContextRole: "child", Limit: tasks.Limit{MaxSteps: 5},
	})
	for i := 0; i < 30; i++ {
		body := strings.Repeat("z", 400)
		run.mu.Lock()
		seq := run.nextHistorySeqLocked()
		run.History = append(run.History, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleTool, Content: body,
			ToolResult: &providers.ToolResult{CallID: "off_" + itoa(i), Name: "exec", Output: body},
		})
		run.mu.Unlock()
	}
	off := false
	b := &DefaultBuilder{
		Config:  cfg,
		Counter: counter,
		Snapshot: func(ctx context.Context, r *Run) (ctxcompiler.Snapshot, error) {
			return ctxcompiler.Snapshot{Role: "child", HarnessVersion: "test"}, nil
		},
		EnableMask:    &off,
		EnableCompact: &off,
	}
	var masks, compacts int
	b.OnMask = func(int, int, string) { masks++ }
	b.OnCompact = func(int, int, int, string) { compacts++ }
	if _, _, err := b.BuildRequest(context.Background(), run, false); err != nil {
		t.Fatal(err)
	}
	if masks != 0 || compacts != 0 {
		t.Fatalf("disabled control must not fire: masks=%d compacts=%d", masks, compacts)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestSnapshotOwnedMaskedPrefixNeverReturnsToLiteral(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	cfg.MaskTriggerTokens, cfg.MaskTargetTokens, cfg.LiteralObservations = 900, 300, 1
	run := testRun(Profile{})
	var snapshot []ctxcompiler.ActivityEvent
	appendResult := func(events []ctxcompiler.ActivityEvent, id string, seq int64) []ctxcompiler.ActivityEvent {
		call := providers.ToolCall{ID: id, Name: "exec", Arguments: []byte(`{"command":"make check"}`)}
		body := strings.Repeat("x", 400)
		return append(events, ctxcompiler.ActivityEvent{Seq: seq, Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{call}}, ctxcompiler.ActivityEvent{Seq: seq + 1, Role: providers.RoleTool, Content: body, ToolResult: &providers.ToolResult{CallID: id, Name: "exec", Output: body}})
	}
	for i := 0; i < 3; i++ {
		snapshot = appendResult(snapshot, "snapshot-"+itoa(i), int64(i*2))
	}
	for i := 0; i < 10; i++ {
		run.History = appendResult(run.History, "history-"+itoa(i), int64(6+i*2))
	}
	no := false
	b := &DefaultBuilder{Config: cfg, Counter: ctxcompiler.NewEstimator(4), EnableCompact: &no, Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{Activity: snapshot}, nil
	}}
	first, _, err := b.BuildRequest(context.Background(), run, false)
	if err != nil {
		t.Fatal(err)
	}
	batches := run.MaskBatches
	if batches == 0 {
		t.Fatal("masking not exercised")
	}
	for turn := 0; turn < 5; turn++ {
		next, _, err := b.BuildRequest(context.Background(), run, false)
		if err != nil {
			t.Fatal(err)
		}
		old, _ := json.Marshal(first.Messages[:len(first.Messages)-1])
		current, _ := json.Marshal(next.Messages[:len(next.Messages)-1])
		if !bytes.Equal(old, current) {
			t.Fatalf("request %d restored snapshot literals or rewrote marker", turn)
		}
		if run.MaskBatches != batches {
			t.Fatal("unchanged snapshots re-entered the literal pool")
		}
	}
	if snapshot[1].Mask != nil || snapshot[1].ToolResult.Output != strings.Repeat("x", 400) {
		t.Fatal("snapshot source was mutated")
	}
}

func TestMaskKeepsOriginalActivityForAuditOnly(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	cfg.MaskTriggerTokens = 100
	cfg.MaskTargetTokens = 50
	cfg.LiteralObservations = 1
	full := strings.Repeat("compiler unit output\n", 80)
	r := testRun(Profile{})
	r.History = []ctxcompiler.ActivityEvent{{Seq: 1, Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "original-output", Name: "exec", Arguments: []byte(`{}`)}}}, {Seq: 2, Role: providers.RoleTool, Content: full, ToolResult: &providers.ToolResult{CallID: "original-output", Name: "exec", Output: full}}}
	r.History = append(r.History, ctxcompiler.ActivityEvent{Seq: 3, Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "recent", Name: "exec", Arguments: []byte(`{}`)}}}, ctxcompiler.ActivityEvent{Seq: 4, Role: providers.RoleTool, Content: "recent", ToolResult: &providers.ToolResult{CallID: "recent", Name: "exec", Output: "recent"}})
	no := false
	b := &DefaultBuilder{Config: cfg, EnableCompact: &no, Snapshot: testSnapshotFunc()}
	req, _, err := b.BuildRequest(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.History[1].Content != full {
		t.Fatal("masking destroyed original audit text")
	}
	for _, message := range req.Messages {
		if message.Role == providers.RoleTool && (message.Content == full || message.ToolResult.Output == full) {
			t.Fatal("full observation leaked into model request")
		}
	}
}
