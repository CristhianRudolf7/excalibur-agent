// Parallel tool batches (Section 7.4).
//
// Every tool declares its effect class and a ResourceKeys function
// extracting its serialization keys (canonical paths, terminal
// session ids, external accounts). The batch planner sorts one step
// of parallel-requested calls into the smallest batch list honoring
// the rules:
//
//   - read-only calls without resource conflicts run in parallel;
//   - writes serialize per resource: two writes to the same file go
//     in order, writes to different files may run in parallel;
//   - exclusive calls (tree-wide git operations, dependency
//     installs) run alone.
//
// Results return to the model in the requested order. The planner is
// pure and owns no goroutines; execution fans out through safego.Go
// (the bare-go linter forbids raw go statements) and rejoins on a
// WaitGroup, so one slow tool never blocks batch siblings.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ExclusiveTools holds tool names that always run alone: tree-wide
// git operations and dependency installs (7.4). Profiles extend it
// through Engine.Exclusive.
var ExclusiveTools = map[string]bool{
	"exec":        true,
	"apply_patch": true,
}

// prefixHash is the stable cache-prefix identity of a compilation:
// the L0-L3 layer hashes joined and hashed again. Closing mode must
// not change it: definitions stay in the request, only tool_choice
// flips to none (7.3, 11.6).
func prefixHash(c ctxcompiler.Compiled) string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{c.Hashes[0], c.Hashes[1], c.Hashes[2], c.Hashes[3]}, "\n")))
	return hex.EncodeToString(sum[:])
}

// batchCall is one planned call with its effect metadata.
type batchCall struct {
	call providers.ToolCall
	def  tools.Definition
	keys []string
}

// PlanBatches sorts one step of tool calls into execution batches.
func (e *Engine) PlanBatches(_ context.Context, run *Run, calls []providers.ToolCall) ([][]providers.ToolCall, error) {
	planned := make([]batchCall, 0, len(calls))
	for _, tc := range calls {
		def, err := e.deps.Tools.Definition(tc.Name)
		if err != nil {
			return nil, err
		}
		var keys []string
		if tool, gerr := e.toolForKeys(tc.Name); gerr == nil && tool != nil {
			if k, kerr := tool.ResourceKeys(tc.Arguments); kerr == nil {
				keys = k
			}
		}
		planned = append(planned, batchCall{call: tc, def: def, keys: keys})
	}
	var batches [][]batchCall
	exclusive := e.exclusiveSet()
	for _, p := range planned {
		if p.def.Effect == types.EffectExec || exclusive[p.call.Name] {
			batches = append(batches, []batchCall{p})
			continue
		}
		if p.def.Effect == types.EffectReadOnly {
			if n := len(batches); n > 0 && batchReadOnly(batches[n-1]) {
				batches[n-1] = append(batches[n-1], p)
			} else {
				batches = append(batches, []batchCall{p})
			}
			continue
		}
		placed := false
		for i := range batches {
			if batchExclusive(batches[i], exclusive) || batchReadOnly(batches[i]) {
				continue
			}
			if !keysOverlap(batches[i], p.keys) {
				batches[i] = append(batches[i], p)
				placed = true
				break
			}
		}
		if !placed {
			batches = append(batches, []batchCall{p})
		}
	}
	out := make([][]providers.ToolCall, 0, len(batches))
	for _, b := range batches {
		one := make([]providers.ToolCall, 0, len(b))
		for _, p := range b {
			one = append(one, p.call)
		}
		out = append(out, one)
	}
	return out, nil
}

func batchReadOnly(b []batchCall) bool {
	for _, p := range b {
		if p.def.Effect != types.EffectReadOnly {
			return false
		}
	}
	return len(b) > 0
}

func batchExclusive(b []batchCall, exclusive map[string]bool) bool {
	for _, p := range b {
		if p.def.Effect == types.EffectExec || exclusive[p.call.Name] {
			return true
		}
	}
	return false
}

func keysOverlap(batch []batchCall, keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	for _, p := range batch {
		for _, k := range p.keys {
			if set[k] {
				return true
			}
		}
	}
	return false
}

func (e *Engine) exclusiveSet() map[string]bool {
	out := map[string]bool{}
	for k, v := range ExclusiveTools {
		out[k] = v
	}
	return out
}

// keyedTool is the optional interface for ResourceKeys lookup when
// the ToolRunner is not the registry adapter.
type keyedTool interface {
	ResourceKeys(args json.RawMessage) ([]string, error)
}

func (e *Engine) toolForKeys(name string) (keyedTool, error) {
	if a, ok := e.deps.Tools.(*RegistryRunner); ok {
		t, err := a.Registry.Get(name)
		if err != nil {
			return nil, err
		}
		return t, nil
	}
	return nil, fmt.Errorf("engine: no key source for %q", name)
}

// runParallel executes one batch concurrently and returns results in
// request order. Each call is journaled before (intent, with its
// idempotency key) and after (result) the effect: the write-ahead
// rule (17.1). Transient tool failures retry with the same key, so
// repeating never duplicates keyed effects (17.7).
func (e *Engine) runParallel(ctx context.Context, run *Run, batch []providers.ToolCall) []tools.Result {
	results := make([]tools.Result, len(batch))
	var wg sync.WaitGroup
	for i, tc := range batch {
		wg.Add(1)
		i, tc := i, tc
		safego.Go("engine/tool-batch", nil, func() {
			defer wg.Done()
			results[i] = e.runOne(ctx, run, tc)
		})
	}
	done := make(chan struct{})
	safego.Go("engine/tool-join", nil, func() {
		defer close(done)
		wg.Wait()
	})
	select {
	case <-ctx.Done():
		// Cancellation still rejoins: after-completion tools must
		// finish first (13.4), and every started effect needs its
		// result entry for crash recovery.
		<-done
	case <-done:
	}
	return results
}

// runOne journals intent, executes with same-key retries on
// transient failures, and journals the result.
func (e *Engine) runOne(ctx context.Context, run *Run, tc providers.ToolCall) tools.Result {
	return e.runOneWithKey(ctx, run, tc, e.deps.NewKey())
}

func (e *Engine) runOneWithKey(ctx context.Context, run *Run, tc providers.ToolCall, key string) tools.Result {
	if key == "" {
		key = e.deps.NewKey()
	}
	rec := e.recorder(run.ID)
	def, derr := e.deps.Tools.Definition(tc.Name)
	if rec != nil {
		effect, idem := e.effectOf(tc.Name)
		_, _ = rec.ToolIntent(ctx, journal.ToolIntentPayload{
			CallID: tc.ID, Tool: tc.Name, Args: tc.Arguments,
			IdempotencyKey: key, Effect: effect, Idempotency: idem,
		})
	}
	// Snapshot tracking: the single-tool view reports the latest
	// in-flight call; parallel siblings union their keys via the
	// batch wrapper in execBatch. Per-call tracking here keeps the
	// sequential path exact.
	timeout := time.Duration(0)
	cancel := types.CancelImmediate
	keys := []string(nil)
	if derr == nil {
		timeout = def.Timeout
		cancel = def.Cancel
		if tool, gerr := e.toolForKeys(tc.Name); gerr == nil && tool != nil {
			if k, kerr := tool.ResourceKeys(tc.Arguments); kerr == nil {
				keys = k
			}
		}
	}
	run.SetCurrentTool(tc.Name, string(tc.Arguments), keys, timeout, cancel)
	defer run.ClearCurrentTool()
	var res tools.Result
	for attempt := 0; ; attempt++ {
		var err error
		res, err = e.deps.Tools.Execute(ctx, tc.Name, tc.Arguments, tools.ExecuteOptions{IdempotencyKey: key})
		if err != nil {
			res = tools.Result{Status: tools.StatusToolError, Output: err.Error()}
		}
		if ctx.Err() != nil && (res.Status == tools.StatusCanceled || res.Status == tools.StatusTimeout) {
			// An orchestrator interrupt canceled the tool: report
			// it as canceled without retrying; the Run loop
			// decides how to continue from the flags.
			break
		}
		if !res.Transient || attempt >= 2 {
			break
		}
	}
	if rec != nil {
		_, _ = rec.ToolResult(ctx, journal.ToolResultPayload{
			CallID: tc.ID, Tool: tc.Name, Status: string(res.Status),
			IdempotencyKey: key, Transient: res.Transient,
		})
	}
	if tail := liveTailOf(res); tail != "" {
		run.SetLiveTail(tail)
	}
	return res
}

// liveTailOf keeps the last bounded tail of a tool output for the
// status snapshot without retaining the whole blob.
func liveTailOf(res tools.Result) string {
	const keep = 512
	if len(res.Output) <= keep {
		return res.Output
	}
	return res.Output[len(res.Output)-keep:]
}

func (e *Engine) effectOf(name string) (types.EffectClass, types.IdempotencyClass) {
	def, err := e.deps.Tools.Definition(name)
	if err != nil {
		return types.EffectExec, types.IdemNonIdempotent
	}
	return def.Effect, def.Idempotency
}

// RegistryRunner is the production ToolRunner: definitions come from
// the catalog registry and execution from the 13.4 pipeline.
type RegistryRunner struct {
	Registry *tools.MapRegistry
	Exec     *tools.Executor
}

// NewRegistryRunner wires a registry to its pipeline executor.
func NewRegistryRunner(reg *tools.MapRegistry, exec *tools.Executor) *RegistryRunner {
	return &RegistryRunner{Registry: reg, Exec: exec}
}

// Execute implements ToolRunner.
func (r *RegistryRunner) Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	return r.Exec.Execute(ctx, name, args, opts)
}

// Definition implements ToolRunner.
func (r *RegistryRunner) Definition(name string) (tools.Definition, error) {
	t, err := r.Registry.Get(name)
	if err != nil {
		return tools.Definition{}, err
	}
	return t.Definition(), nil
}
