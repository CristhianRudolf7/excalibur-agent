// End-to-end: a simple task and an interactive program, resolved
// through a scripted fake provider but executed by the real journal,
// compiler, gateway, tools and gate (Sections 7.3, 7.7, 26.1-26.2).
package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// e2eWorld wires the real packages: gateway over a scripted fake
// provider, the real tool pipeline with the real file tools, the
// real context compiler and the real deterministic gate.
type e2eWorld struct {
	engine  *Engine
	run     *Run
	js      *journal.MemStore
	fake    *providerfake.FakeProvider
	gateway *models.Gateway
}

func newE2E(t *testing.T, scripts []providers.ChatResponse, gate GateChecker) *e2eWorld {
	t.Helper()
	fake := providerfake.New("e2e")
	fake.Responses = scripts
	gw := models.NewGateway(models.GatewayConfig{
		Roles: map[types.ModelRole]models.RoleEntry{
			types.RoleExecutor: {Primary: models.ModelRef{Provider: "e2e", Model: "e2e-model"}},
		},
		Prices:       map[string]models.Price{"e2e-model": {Input: 1, Output: 4}},
		PriceVersion: "test",
		MaxAttempts:  2,
		BaseDelay:    1,
	})
	gw.Register(fake)
	reg := tools.NewMapRegistry()
	if err := tools.RegisterBuiltins(reg, tools.DefaultBuiltinDeps()); err != nil {
		t.Fatal(err)
	}
	runner := NewRegistryRunner(reg, tools.NewExecutor(reg))
	snap := func(ctx context.Context, run *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{
			Role: "child", HarnessVersion: "e2e",
			Volatile: ctxcompiler.Volatile{Now: time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)},
		}, nil
	}
	toolDefs := func(run *Run) []providers.ToolDefinition {
		var out []providers.ToolDefinition
		for _, name := range reg.Names() {
			tool, err := reg.Get(name)
			if err != nil {
				continue
			}
			def := tool.Definition()
			out = append(out, providers.ToolDefinition{Name: def.Name, Description: def.Description, Schema: def.InputSchema})
		}
		return out
	}
	builder := &DefaultBuilder{
		Config: ctxcompiler.DefaultConfig(), Counter: ctxcompiler.NewEstimator(0),
		Snapshot: snap, ToolDefs: toolDefs, ReasoningEffort: "low",
	}
	if gate == nil {
		gate = passingGate()
	}
	e := New(Deps{Models: gw, Context: builder, Tools: runner, Gate: gate})
	js := journal.NewMemStore()
	run := NewRun("run_e2e", "task_e2e", Profile{
		Name: "child", Role: types.RoleExecutor, ContextRole: "child",
		Limit: tasks.Limit{MaxSteps: 25},
	})
	e.AttachJournal(run.ID, js)
	return &e2eWorld{engine: e, run: run, js: js, fake: fake, gateway: gw}
}

func writeCall(id, path, content string) providers.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return providers.ToolCall{ID: id, Name: "write_file", Arguments: args}
}

func TestEndToEndSimpleTask(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	w := newE2E(t, []providers.ChatResponse{
		toolResp(writeCall("c1", target, "hello")),
		finalResp("wrote hello.txt"),
	}, nil)
	if err := w.engine.Admit(ctx, w.run); err != nil {
		t.Fatal(err)
	}
	if _, err := w.engine.recorder(w.run.ID).RunStarted(ctx, map[string]string{"task": "write hello"}); err != nil {
		t.Fatal(err)
	}
	if err := w.engine.Start(ctx, w.run); err != nil {
		t.Fatal(err)
	}
	if err := w.engine.Run(ctx, w.run); err != nil {
		t.Fatal(err)
	}
	mustState(t, w.run, types.RunCompleted)
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "hello" {
		t.Fatalf("file = %q, err = %v", data, err)
	}
	entries, err := w.js.List(ctx, w.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[journal.EntryType]int{}
	for _, en := range entries {
		seen[en.Type]++
	}
	for _, want := range []journal.EntryType{
		journal.TypeRunStarted, journal.TypeModelRequest, journal.TypeModelResponse,
		journal.TypeToolIntent, journal.TypeToolResult, journal.TypeStateChange,
	} {
		if seen[want] == 0 {
			t.Fatalf("journal missing %s: %v", want, seen)
		}
	}
	if got := len(w.fake.ChatRequests); got != 2 {
		t.Fatalf("model calls = %d, want 2 (one tool turn, one final)", got)
	}
	rep := w.engine.Report(w.run)
	if rep.State != types.RunCompleted || rep.PlainState != "done" {
		t.Fatalf("report: %+v", rep)
	}
	if rep.Metrics.ToolCalls != 1 {
		t.Fatalf("metrics tool calls = %d", rep.Metrics.ToolCalls)
	}
}

func TestEndToEndInteractiveProgram(t *testing.T) {
	ctx := context.Background()
	w := newE2E(t, []providers.ChatResponse{
		toolResp(providers.ToolCall{ID: "t1", Name: "exec", Arguments: json.RawMessage(`{"command":"scaffold"}`)}),
		toolResp(providers.ToolCall{ID: "t2", Name: "exec", Arguments: json.RawMessage(`{"command":"scaffold","input":"myapp"}`)}),
		finalResp("scaffolded myapp"),
	}, nil)
	// Script the interactive session: the first call prompts, the
	// second consumes the answer. exec would run a real process, so
	// this world swaps the runner for a scripted terminal.
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "project name?"},
		tools.Result{Status: tools.StatusOK, Output: "scaffolded myapp"})
	w.engine.deps.Tools = runner
	if err := w.engine.Admit(ctx, w.run); err != nil {
		t.Fatal(err)
	}
	if err := w.engine.Start(ctx, w.run); err != nil {
		t.Fatal(err)
	}
	if err := w.engine.Run(ctx, w.run); err != nil {
		t.Fatal(err)
	}
	mustState(t, w.run, types.RunCompleted)
	// History pairs every assistant tool-call message with its tool
	// results, so the next model request carries complete
	// function_call/function_call_output pairs (Section 19.2).
	if len(w.run.History) != 4 {
		t.Fatalf("history = %d entries, want 4 (2 tool calls + 2 results)", len(w.run.History))
	}
	if w.run.History[0].Role != providers.RoleAssistant || len(w.run.History[0].ToolCalls) != 1 {
		t.Fatalf("history[0] must be the assistant tool call: %+v", w.run.History[0])
	}
	if w.run.History[1].Content != "project name?" || w.run.History[3].Content != "scaffolded myapp" {
		t.Fatalf("history: %+v", w.run.History)
	}
	if got := len(w.fake.ChatRequests); got != 3 {
		t.Fatalf("model calls = %d, want 3 turns", got)
	}
}
