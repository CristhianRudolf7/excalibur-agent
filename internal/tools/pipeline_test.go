package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type recordJournal struct {
	intents int
	results int
}

func (j *recordJournal) RecordIntent(_ context.Context, _ Invocation) error { j.intents++; return nil }
func (j *recordJournal) RecordResult(_ context.Context, _ Invocation, _ Result) error {
	j.results++
	return nil
}

func scriptedRegistry(def Definition, fn func(ctx context.Context, inv Invocation) (Result, error)) *MapRegistry {
	r := NewMapRegistry()
	_ = r.Register(&funcTool{def: def, fn: fn})
	return r
}

type funcTool struct {
	def Definition
	fn  func(ctx context.Context, inv Invocation) (Result, error)
}

func (t *funcTool) Definition() Definition { return t.def }
func (t *funcTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	return t.fn(ctx, inv)
}
func (t *funcTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func echoDef() Definition {
	return Definition{
		Name: "echo_test", Description: "echo", InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
		Effect: types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate,
		Timeout: time.Second, OutputBudget: 100,
	}
}

func TestRepairMinorJSON(t *testing.T) {
	raw := json.RawMessage(`{"msg": "hi",}`)
	fixed := RepairJSON(raw)
	if !json.Valid(fixed) {
		t.Fatalf("trailing comma not repaired: %s", fixed)
	}
	var v map[string]string
	if err := json.Unmarshal(fixed, &v); err != nil || v["msg"] != "hi" {
		t.Fatalf("bad repair: %s %v", fixed, err)
	}
}

func TestMajorJSONError(t *testing.T) {
	reg := scriptedRegistry(echoDef(), func(_ context.Context, _ Invocation) (Result, error) {
		return Result{Status: StatusOK, Output: "unreachable"}, nil
	})
	ex := NewExecutor(reg)
	res, err := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg": }`), ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusInputError {
		t.Fatalf("want input_error, got %+v", res)
	}
	if !strings.Contains(res.Output, "msg") {
		t.Fatalf("error should carry schema part, got %q", res.Output)
	}
}

func TestPipelineValidationAndJournal(t *testing.T) {
	j := &recordJournal{}
	reg := scriptedRegistry(echoDef(), func(_ context.Context, inv Invocation) (Result, error) {
		var a map[string]string
		_ = json.Unmarshal(inv.Arguments, &a)
		return Result{Status: StatusOK, Output: a["msg"]}, nil
	})
	ex := NewExecutor(reg)
	ex.Journal = j
	// Minor error repaired, then validated and run.
	res, _ := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg": "hi",}`), ExecuteOptions{})
	if res.Status != StatusOK || res.Output != "hi" {
		t.Fatalf("got %+v", res)
	}
	if j.intents != 1 || j.results != 1 {
		t.Fatalf("journal hooks: %+v", j)
	}
	// Missing required field.
	res2, _ := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{}`), ExecuteOptions{})
	if res2.Status != StatusInputError {
		t.Fatalf("want input_error, got %+v", res2)
	}
}

func TestPipelineTimeout(t *testing.T) {
	def := echoDef()
	def.Timeout = 50 * time.Millisecond
	reg := scriptedRegistry(def, func(ctx context.Context, _ Invocation) (Result, error) {
		select {
		case <-ctx.Done():
			return Result{Status: StatusOK, Output: "late"}, nil
		case <-time.After(5 * time.Second):
			return Result{Status: StatusOK, Output: "late"}, nil
		}
	})
	ex := NewExecutor(reg)
	res, _ := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg":"x"}`), ExecuteOptions{})
	if res.Status != StatusTimeout {
		t.Fatalf("want timeout, got %+v", res)
	}
}

func TestPipelineCancelModes(t *testing.T) {
	// after_completion detaches from parent cancel but honors timeout.
	def := echoDef()
	def.Cancel = types.CancelAfterCompletion
	def.Timeout = 2 * time.Second
	reg := scriptedRegistry(def, func(ctx context.Context, _ Invocation) (Result, error) {
		select {
		case <-ctx.Done():
			return Result{Status: StatusOK, Output: "canceled"}, nil
		case <-time.After(100 * time.Millisecond):
			return Result{Status: StatusOK, Output: "finished"}, nil
		}
	})
	ex := NewExecutor(reg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, _ := ex.Execute(ctx, "echo_test", json.RawMessage(`{"msg":"x"}`), ExecuteOptions{})
	if res.Status != StatusOK || res.Output != "finished" {
		t.Fatalf("after_completion must finish, got %+v", res)
	}

	// immediate propagates cancel.
	def2 := echoDef()
	def2.Cancel = types.CancelImmediate
	reg2 := scriptedRegistry(def2, func(ctx context.Context, _ Invocation) (Result, error) {
		<-ctx.Done()
		return Result{Status: StatusOK, Output: "late"}, nil
	})
	ex2 := NewExecutor(reg2)
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	res2, _ := ex2.Execute(ctx2, "echo_test", json.RawMessage(`{"msg":"x"}`), ExecuteOptions{})
	if res2.Status != StatusCanceled {
		t.Fatalf("immediate must cancel, got %+v", res2)
	}
}

func TestTruncationAndGetOutput(t *testing.T) {
	def := echoDef()
	def.OutputBudget = 100
	reg := scriptedRegistry(def, func(_ context.Context, _ Invocation) (Result, error) {
		return Result{Status: StatusOK, Output: strings.Repeat("a", 1000)}, nil
	})
	ex := NewExecutor(reg)
	res, _ := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg":"x"}`), ExecuteOptions{})
	if !res.Truncated || res.BlobRef == "" {
		t.Fatalf("want truncation with blob ref, got %+v", res)
	}
	if !strings.Contains(res.Output, "truncated") || !strings.Contains(res.Output, res.BlobRef) {
		t.Fatalf("marker must carry ref: %q", res.Output)
	}
	get := &GetToolOutputTool{Outputs: ex.Outputs}
	// Use the ref directly through the store-backed tool.
	raw, _ := json.Marshal(map[string]string{"ref": res.BlobRef})
	got2, _ := get.Invoke(context.Background(), Invocation{Arguments: raw})
	if !strings.Contains(got2.Output, strings.Repeat("a", 100)) {
		t.Fatalf("get_tool_output must return full output, got %q", got2.Output)
	}
}

func TestRedactionBeforeTruncation(t *testing.T) {
	def := echoDef()
	reg := scriptedRegistry(def, func(_ context.Context, _ Invocation) (Result, error) {
		return Result{Status: StatusOK, Output: "token SECRET-123 here"}, nil
	})
	ex := NewExecutor(reg)
	ex.Redact = func(s string) string { return strings.ReplaceAll(s, "SECRET-123", "[redacted]") }
	res, _ := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg":"x"}`), ExecuteOptions{})
	if strings.Contains(res.Output, "SECRET-123") {
		t.Fatalf("redactor must run before normalization: %q", res.Output)
	}
}

func TestRecoveryClasses(t *testing.T) {
	if RecoveryFor(types.EffectReadOnly) != RecoverRerun {
		t.Fatal("read_only reruns")
	}
	if RecoveryFor(types.EffectWriteLocal) != RecoverCheckProbe {
		t.Fatal("write_local checks probe")
	}
	if RecoveryFor(types.EffectExec) != RecoverUncertain {
		t.Fatal("exec is uncertain")
	}
	if RecoveryForIdempotency(types.EffectExec, types.IdemKeyed) != RecoverResendKey {
		t.Fatal("keyed resends with key")
	}
	if RecoveryForIdempotency(types.EffectReadOnly, types.IdemNonIdempotent) != RecoverRerun {
		t.Fatal("pure/readonly rerun")
	}
}

func TestKeyedIdempotencyKeyGenerated(t *testing.T) {
	var gotKey string
	def := echoDef()
	def.Idempotency = types.IdemKeyed
	reg := scriptedRegistry(def, func(_ context.Context, inv Invocation) (Result, error) {
		gotKey = inv.IdempotencyKey
		return Result{Status: StatusOK, Output: "ok"}, nil
	})
	ex := NewExecutor(reg)
	ex.NewID = func() string { return "fixed-key" }
	_, _ = ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg":"x"}`), ExecuteOptions{})
	if gotKey != "fixed-key" {
		t.Fatalf("keyed tools need a key, got %q", gotKey)
	}
}

func TestAtomicToolContextPreservesRunIdentity(t *testing.T) {
	ex := NewExecutor(NewMapRegistry())
	parent, stop := context.WithCancel(WithSourceRun(context.Background(), "run_atomic"))
	stop()
	ctx, cancel := ex.contextForCancel(parent, types.CancelAfterCompletion, time.Second)
	defer cancel()
	if ctx.Err() != nil || ExecutingRunID(ctx, "") != "run_atomic" {
		t.Fatalf("atomic context lost execution identity or inherited cancellation")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("atomic timeout missing")
	}
}
