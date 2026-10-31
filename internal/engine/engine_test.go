// Engine test seams: scripted model, tools and context.
package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	toolfake "github.com/CristhianRudolf7/excalibur-agent/internal/tools/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// fakeEndpoint serves scripted model responses and records requests.
type fakeEndpoint struct {
	mu        sync.Mutex
	responses []providers.ChatResponse
	requests  []providers.ChatRequest
	roles     []types.ModelRole
	err       error
}

func (f *fakeEndpoint) Chat(_ context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	f.roles = append(f.roles, role)
	if f.err != nil {
		return providers.ChatResponse{}, models.CallMeta{}, f.err
	}
	if len(f.responses) == 0 {
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "done"}}, models.CallMeta{}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, models.CallMeta{Provider: "fake", Model: "m"}, nil
}

func (f *fakeEndpoint) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func toolResp(calls ...providers.ToolCall) providers.ChatResponse {
	return providers.ChatResponse{Message: providers.Message{
		Role: providers.RoleAssistant, Content: "", ToolCalls: calls,
	}}
}

func finalResp(text string) providers.ChatResponse {
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: text}}
}

func toolCall(id, name string, args string) providers.ToolCall {
	return providers.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
}

// fakeRunner serves definitions and scripted results.
type fakeRunner struct {
	mu      sync.Mutex
	defs    map[string]tools.Definition
	results map[string][]tools.Result
	calls   []providers.ToolCall
	effects int
}

func scriptedRunner() *fakeRunner {
	return &fakeRunner{defs: map[string]tools.Definition{}, results: map[string][]tools.Result{}}
}

func (f *fakeRunner) add(name string, effect types.EffectClass, idem types.IdempotencyClass, results ...tools.Result) {
	f.defs[name] = tools.Definition{
		Name: name, Description: "scripted " + name,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Effect:      effect, Idempotency: idem, Cancel: types.CancelImmediate,
	}
	f.results[name] = append(f.results[name], results...)
}

func (f *fakeRunner) Execute(_ context.Context, name string, args json.RawMessage, _ tools.ExecuteOptions) (tools.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, providers.ToolCall{Name: name, Arguments: args})
	f.effects++
	if len(f.results[name]) > 0 {
		res := f.results[name][0]
		f.results[name] = f.results[name][1:]
		return res, nil
	}
	return tools.Result{Status: tools.StatusOK, Output: name + " ok"}, nil
}

func (f *fakeRunner) Definition(name string) (tools.Definition, error) {
	d, ok := f.defs[name]
	if !ok {
		return tools.Definition{}, &missingToolError{name: name}
	}
	return d, nil
}

func (f *fakeRunner) ncalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type missingToolError struct{ name string }

func (e *missingToolError) Error() string { return "unknown tool " + e.name }

// fakeBuilder returns one canned request shape per test.
type fakeBuilder struct {
	mu       sync.Mutex
	compiled ctxcompiler.Compiled
	closing  []bool
	requests []providers.ChatRequest
	tools    []providers.ToolDefinition
}

func (b *fakeBuilder) BuildRequest(_ context.Context, _ *Run, closing bool) (providers.ChatRequest, ctxcompiler.Compiled, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closing = append(b.closing, closing)
	choice := ""
	if closing {
		choice = "none"
	}
	req := providers.ChatRequest{Tools: b.tools, ToolChoice: choice}
	b.requests = append(b.requests, req)
	return req, b.compiled, nil
}

// testEngine wires scripted deps with an in-memory journal.
func testEngine(model *fakeEndpoint, runner *fakeRunner, gate GateChecker) (*Engine, *journal.MemStore) {
	js := journal.NewMemStore()
	e := New(Deps{Models: model, Context: &fakeBuilder{}, Tools: runner, Gate: gate})
	return e, js
}

func testRun(profile Profile) *Run {
	if profile.Role == "" {
		profile.Role = types.RoleExecutor
	}
	return NewRun("run_test", "task_test", profile)
}

func passingGate() GateChecker {
	return GateFunc(func(*Run) tasks.GateReport { return tasks.GateReport{OK: true} })
}

func startRun(t *testing.T, e *Engine, run *Run, js *journal.MemStore) {
	t.Helper()
	e.AttachJournal(run.ID, js)
	ctx := context.Background()
	if err := e.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
}

func mustState(t *testing.T, run *Run, want types.RunState) {
	t.Helper()
	if got := run.State(); got != want {
		t.Fatalf("state = %s, want %s", got, want)
	}
}

// realRegistry builds a catalog with scripted tools carrying real
// effect classes and resource keys for batch tests.
func realRegistry(t *testing.T) *tools.MapRegistry {
	t.Helper()
	reg := tools.NewMapRegistry()
	add := func(name string, effect types.EffectClass, idem types.IdempotencyClass, keys []string, out string) {
		st := toolfake.New(name)
		st.Def.Effect = effect
		st.Def.Idempotency = idem
		st.Keys = keys
		st.Results = []tools.Result{{Status: tools.StatusOK, Output: out}}
		if err := reg.Register(st); err != nil {
			t.Fatal(err)
		}
	}
	add("read_a", types.EffectReadOnly, types.IdemPure, nil, "a")
	add("read_b", types.EffectReadOnly, types.IdemPure, nil, "b")
	add("write_f1", types.EffectWriteLocal, types.IdemIdempotent, []string{"file:/f1"}, "w1")
	add("write_f2", types.EffectWriteLocal, types.IdemIdempotent, []string{"file:/f2"}, "w2")
	add("git_op", types.EffectExec, types.IdemNonIdempotent, nil, "git")
	return reg
}
