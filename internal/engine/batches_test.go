// Parallel batches (Section 7.4): read/write/exclusive grouping,
// per-resource serialization and request-order results.
package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func batchCalls() []providers.ToolCall {
	return []providers.ToolCall{
		toolCall("r1", "read_a", `{}`),
		toolCall("r2", "read_b", `{}`),
		toolCall("w1", "write_f1", `{"path":"/f1"}`),
		toolCall("w2", "write_f1", `{"path":"/f1"}`),
		toolCall("w3", "write_f2", `{"path":"/f2"}`),
		toolCall("g1", "git_op", `{}`),
	}
}

func TestPlanBatchesGroupsByEffectAndResource(t *testing.T) {
	reg := realRegistry(t)
	exec := tools.NewExecutor(reg)
	e := New(Deps{Tools: NewRegistryRunner(reg, exec), Gate: passingGate()})
	run := testRun(Profile{})
	batches, err := e.PlanBatches(context.Background(), run, batchCalls())
	if err != nil {
		t.Fatal(err)
	}
	names := func(b []providers.ToolCall) []string {
		var out []string
		for _, c := range b {
			out = append(out, c.Name)
		}
		return out
	}
	if len(batches) != 4 {
		t.Fatalf("batches = %v, want 4", batches)
	}
	if got := names(batches[0]); len(got) != 2 || got[0] != "read_a" || got[1] != "read_b" {
		t.Fatalf("batch0 = %v, want parallel reads", got)
	}
	if got := names(batches[1]); len(got) != 2 || got[0] != "write_f1" || got[1] != "write_f2" {
		t.Fatalf("batch1 = %v, want independent writes in parallel", got)
	}
	if got := names(batches[2]); len(got) != 1 || got[0] != "write_f1" {
		t.Fatalf("batch2 = %v, want the conflicting write serialized", got)
	}
	if got := names(batches[3]); len(got) != 1 || got[0] != "git_op" {
		t.Fatalf("batch3 = %v, want the exclusive alone", got)
	}
}

func TestBatchResultsKeepRequestOrder(t *testing.T) {
	reg := realRegistry(t)
	exec := tools.NewExecutor(reg)
	e := New(Deps{Tools: NewRegistryRunner(reg, exec), Gate: passingGate()})
	run := testRun(Profile{})
	e.AttachJournal(run.ID, journal.NewMemStore())
	ctx := context.Background()
	_ = e.Admit(ctx, run)
	_ = e.Start(ctx, run)
	batches, err := e.PlanBatches(ctx, run, batchCalls())
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, b := range batches {
		res := e.runParallel(ctx, run, b)
		if len(res) != len(b) {
			t.Fatalf("results %d != batch %d", len(res), len(b))
		}
		for i, tc := range b {
			order = append(order, tc.Name)
			if res[i].Status != tools.StatusOK {
				t.Fatalf("%s: %+v", tc.Name, res[i])
			}
		}
	}
	want := []string{"read_a", "read_b", "write_f1", "write_f2", "write_f1", "git_op"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// funcTool adapts a closure to tools.Tool.
type funcTool struct {
	def tools.Definition
	fn  func() tools.Result
}

func (f *funcTool) Definition() tools.Definition { return f.def }
func (f *funcTool) Invoke(context.Context, tools.Invocation) (tools.Result, error) {
	return f.fn(), nil
}
func (f *funcTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// TestParallelBatchOverlaps proves two reads run concurrently: the
// second tool starts before the first finishes.
func TestParallelBatchOverlaps(t *testing.T) {
	reg := tools.NewMapRegistry()
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var starts []string
	for _, name := range []string{"slow_a", "slow_b"} {
		name := name
		st := &funcTool{
			def: tools.Definition{Name: name, Description: "blocking",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Effect:      types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate},
			fn: func() tools.Result {
				mu.Lock()
				starts = append(starts, name)
				n := len(starts)
				mu.Unlock()
				if n == 2 {
					once.Do(func() { close(release) })
				}
				select {
				case <-release:
				case <-time.After(5 * time.Second):
					return tools.Result{Status: tools.StatusToolError, Output: "no overlap"}
				}
				return tools.Result{Status: tools.StatusOK, Output: name + " done"}
			},
		}
		if err := reg.Register(st); err != nil {
			t.Fatal(err)
		}
	}
	exec := tools.NewExecutor(reg)
	e := New(Deps{Tools: NewRegistryRunner(reg, exec), Gate: passingGate()})
	run := testRun(Profile{})
	e.AttachJournal(run.ID, journal.NewMemStore())
	ctx := context.Background()
	_ = e.Admit(ctx, run)
	_ = e.Start(ctx, run)
	calls := []providers.ToolCall{toolCall("1", "slow_a", `{}`), toolCall("2", "slow_b", `{}`)}
	batches, err := e.PlanBatches(ctx, run, calls)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 {
		t.Fatalf("reads should share one batch: %v", batches)
	}
	res := e.runParallel(ctx, run, batches[0])
	for _, r := range res {
		if r.Status != tools.StatusOK {
			t.Fatalf("result: %+v", r)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 2 {
		t.Fatalf("starts = %v", starts)
	}
}
