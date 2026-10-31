package fake

import (
	"encoding/json"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

func testManifest() capabilities.Manifest {
	return capabilities.Manifest{
		ID: "demo", Version: "1.0.0",
		Operations: []capabilities.Operation{{
			Name: "run", Description: "Run",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Effect:      "exec", Idempotency: "keyed", Cancel: "cooperative",
		}},
	}
}

func TestIdempotencySingleEffect(t *testing.T) {
	f := New(testManifest())
	f.Results["run"] = []capabilities.TaskResult{
		{TaskID: "t1", State: capabilities.StateCompleted, Payload: json.RawMessage(`{"n":1}`)},
		{TaskID: "t1", State: capabilities.StateCompleted, Payload: json.RawMessage(`{"n":1}`)},
	}
	if _, err := f.Invoke("run", json.RawMessage(`{}`), "key-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Invoke("run", json.RawMessage(`{}`), "key-1"); err != nil {
		t.Fatal(err)
	}
	if got := f.EffectCount("run"); got != 1 {
		t.Fatalf("same key must count one effect, got %d", got)
	}
	if got := f.CallCount("run"); got != 2 {
		t.Fatalf("calls: %d", got)
	}
}

func TestDurableProgressionAndRestart(t *testing.T) {
	f := New(testManifest())
	f.Results["run"] = []capabilities.TaskResult{
		{TaskID: "durable-1", State: capabilities.StateWorking},
	}
	f.Next["durable-1"] = []capabilities.TaskResult{
		{TaskID: "durable-1", State: capabilities.StateInputRequired, Need: "login"},
		{TaskID: "durable-1", State: capabilities.StateCompleted, Payload: json.RawMessage(`{"ok":true}`)},
	}
	first, err := f.Invoke("run", json.RawMessage(`{}`), "k")
	if err != nil {
		t.Fatal(err)
	}
	if first.State != capabilities.StateWorking {
		t.Fatalf("want working, got %s", first.State)
	}
	// Restart-safe: the task id persists and polling resumes.
	need, err := f.Poll("durable-1")
	if err != nil {
		t.Fatal(err)
	}
	if need.State != capabilities.StateInputRequired {
		t.Fatalf("want input_required, got %s", need.State)
	}
	done, err := f.Update("durable-1", json.RawMessage(`{"login":"u"}`))
	if err != nil {
		t.Fatal(err)
	}
	if done.State != capabilities.StateCompleted {
		t.Fatalf("want completed, got %s", done.State)
	}
}

func TestCooperativeCancelResume(t *testing.T) {
	f := New(testManifest())
	f.Results["run"] = []capabilities.TaskResult{
		{TaskID: "c1", State: capabilities.StateWorking},
	}
	f.Next["c1"] = []capabilities.TaskResult{
		{TaskID: "c1", State: capabilities.StateWorking},
		{TaskID: "c1", State: capabilities.StateCancelled},
	}
	if _, err := f.Invoke("run", json.RawMessage(`{}`), "k"); err != nil {
		t.Fatal(err)
	}
	// Cancel is cooperative: the harness re-polls the final state.
	mid, err := f.Cancel("c1")
	if err != nil {
		t.Fatal(err)
	}
	if mid.State != capabilities.StateWorking {
		t.Fatalf("cooperative cancel may not take effect at once: %s", mid.State)
	}
	final, err := f.Poll("c1")
	if err != nil {
		t.Fatal(err)
	}
	if final.State != capabilities.StateCancelled {
		t.Fatalf("want cancelled, got %s", final.State)
	}
}
