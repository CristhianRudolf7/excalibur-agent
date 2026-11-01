package engine

import (
	"context"
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
