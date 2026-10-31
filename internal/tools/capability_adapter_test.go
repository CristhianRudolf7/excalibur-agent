package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities/fake"
)

func fakeCapability() *fake.ScriptedCapability {
	m := capabilities.Manifest{
		ID: "demo", Version: "1.0.0",
		Operations: []capabilities.Operation{
			{
				Name: "demo_run", Description: "Run a demo black-box operation",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`),
				Effect:      "read_only", Idempotency: "pure", Cancel: "immediate",
			},
		},
	}
	return fake.New(m)
}

func TestCapabilityAdapterStates(t *testing.T) {
	f := fakeCapability()
	f.Results["demo_run"] = []capabilities.TaskResult{
		{TaskID: "w1", State: capabilities.StateWorking},
	}
	f.Next["w1"] = []capabilities.TaskResult{
		{TaskID: "w1", State: capabilities.StateInputRequired, Need: "login"},
		{TaskID: "w1", State: capabilities.StateCompleted, Payload: json.RawMessage(`{"ok":true}`)},
	}
	adapted, err := ToolsFromCapability(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapted) != 1 {
		t.Fatalf("want 1 tool, got %d", len(adapted))
	}
	// First poll progression: working -> input_required surfaces the need.
	f2 := fakeCapability()
	f2.Results["demo_run"] = []capabilities.TaskResult{
		{TaskID: "w1", State: capabilities.StateWorking},
	}
	f2.Next["w1"] = []capabilities.TaskResult{
		{TaskID: "w1", State: capabilities.StateInputRequired, Need: "captcha"},
	}
	adapted2, _ := ToolsFromCapability(f2)
	res, _ := adapted2[0].Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"q":"x"}`)})
	if res.Status != StatusOK || !strings.Contains(res.Output, "input_required") {
		t.Fatalf("input_required must surface: %+v", res)
	}
	// Failed with input class maps to input_error.
	f3 := fakeCapability()
	f3.Results["demo_run"] = []capabilities.TaskResult{
		{TaskID: "f1", State: capabilities.StateFailed, ErrClass: capabilities.ErrInput, ErrorText: "bad q"},
	}
	adapted3, _ := ToolsFromCapability(f3)
	res3, _ := adapted3[0].Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"q":"x"}`)})
	if res3.Status != StatusInputError {
		t.Fatalf("input failure must be input_error: %+v", res3)
	}
	// Cancelled maps to canceled; restart-safe poll resumes by task id.
	f4 := fakeCapability()
	f4.Results["demo_run"] = []capabilities.TaskResult{
		{TaskID: "c1", State: capabilities.StateWorking},
	}
	f4.Next["c1"] = []capabilities.TaskResult{
		{TaskID: "c1", State: capabilities.StateCancelled},
	}
	adapted4, _ := ToolsFromCapability(f4)
	res4, _ := adapted4[0].Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"q":"x"}`)})
	if res4.Status != StatusCanceled {
		t.Fatalf("cancelled must map to canceled: %+v", res4)
	}
}

func TestCapabilityIdempotentResend(t *testing.T) {
	f := fakeCapability()
	f.Results["demo_run"] = []capabilities.TaskResult{
		{TaskID: "t1", State: capabilities.StateCompleted, Payload: json.RawMessage(`{}`)},
		{TaskID: "t1", State: capabilities.StateCompleted, Payload: json.RawMessage(`{}`)},
	}
	adapted, _ := ToolsFromCapability(f)
	if _, err := adapted[0].Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"q":"a"}`), IdempotencyKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapted[0].Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"q":"a"}`), IdempotencyKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	if got := f.EffectCount("demo_run"); got != 1 {
		t.Fatalf("same key must count one effect, got %d", got)
	}
}
