// Journal tests: append ordering, write-ahead helpers, model-leg
// replay and the 17.2 pending-effect classification.
package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testEntries() []Entry {
	intent := func(call, tool string, idem types.IdempotencyClass) Entry {
		return Entry{Type: TypeToolIntent, Payload: MarshalPayload(ToolIntentPayload{
			CallID: call, Tool: tool, Idempotency: idem,
		})}
	}
	result := func(call string) Entry {
		return Entry{Type: TypeToolResult, Payload: MarshalPayload(ToolResultPayload{CallID: call})}
	}
	return []Entry{
		{Type: TypeRunStarted},
		{Type: TypeModelRequest, Payload: MarshalPayload(ModelRequestPayload{Hash: "h1"})},
		{Type: TypeModelResponse, Payload: MarshalPayload(ModelResponsePayload{Hash: "h1"})},
		intent("c1", "read_file", types.IdemPure),
		result("c1"),
		intent("c2", "exec", types.IdemNonIdempotent),
		intent("c3", "write_file", types.IdemIdempotent),
	}
}

func TestMemStoreAssignsSequences(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	rec := &Recorder{Store: m, RunID: "run_1"}
	for i := 0; i < 3; i++ {
		e, err := rec.ModelRequest(ctx, "h")
		if err != nil {
			t.Fatal(err)
		}
		if e.Seq != uint64(i) {
			t.Fatalf("seq = %d, want %d", e.Seq, i)
		}
	}
	entries, err := m.List(ctx, "run_1")
	if err != nil || len(entries) != 3 {
		t.Fatalf("list: %v %d", err, len(entries))
	}
}

func TestPendingIntentsSkipsCompleted(t *testing.T) {
	pendings := PendingIntents(testEntries())
	if len(pendings) != 2 {
		t.Fatalf("pendings = %d, want 2 (c2, c3)", len(pendings))
	}
	if pendings[0].Intent.CallID != "c2" || pendings[1].Intent.CallID != "c3" {
		t.Fatalf("pendings: %+v", pendings)
	}
}

func TestModelLegReplay(t *testing.T) {
	rep := ScanModelLeg(testEntries())
	if rep.Requests != 1 || rep.Responses != 1 || !rep.HasResponse {
		t.Fatalf("replay: %+v", rep)
	}
	trailing := append(testEntries(),
		Entry{Type: TypeModelRequest, Payload: MarshalPayload(ModelRequestPayload{Hash: "h2"})})
	rep = ScanModelLeg(trailing)
	if rep.HasResponse || rep.LastRequestHash != "h2" {
		t.Fatalf("trailing request needs a fresh call: %+v", rep)
	}
}

type probeFunc func(ctx context.Context, tool string, args json.RawMessage) (bool, error)

func (f probeFunc) Probe(ctx context.Context, tool string, args json.RawMessage) (bool, error) {
	return f(ctx, tool, args)
}

func TestResolvePendingClasses(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		intent ToolIntentPayload
		probe  Prober
		want   PendingAction
	}{
		{"pure reruns", ToolIntentPayload{CallID: "a", Idempotency: types.IdemPure}, nil, ActionRerun},
		{"idempotent without probe reruns", ToolIntentPayload{CallID: "b", Idempotency: types.IdemIdempotent}, nil, ActionRerun},
		{"idempotent probe true done", ToolIntentPayload{CallID: "c", Idempotency: types.IdemIdempotent},
			probeFunc(func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }), ActionDone},
		{"idempotent probe false reruns", ToolIntentPayload{CallID: "d", Idempotency: types.IdemIdempotent},
			probeFunc(func(context.Context, string, json.RawMessage) (bool, error) { return false, nil }), ActionRerun},
		{"keyed resends same key", ToolIntentPayload{CallID: "e", Idempotency: types.IdemKeyed, IdempotencyKey: "k"}, nil, ActionRerun},
		{"non-idempotent without probe uncertain", ToolIntentPayload{CallID: "f", Idempotency: types.IdemNonIdempotent}, nil, ActionUncertain},
		{"non-idempotent probe true done", ToolIntentPayload{CallID: "g", Idempotency: types.IdemNonIdempotent},
			probeFunc(func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }), ActionDone},
		{"non-idempotent probe false uncertain", ToolIntentPayload{CallID: "h", Idempotency: types.IdemNonIdempotent},
			probeFunc(func(context.Context, string, json.RawMessage) (bool, error) { return false, nil }), ActionUncertain},
		{"non-idempotent probe error uncertain", ToolIntentPayload{CallID: "i", Idempotency: types.IdemNonIdempotent},
			probeFunc(func(context.Context, string, json.RawMessage) (bool, error) { return false, fmt.Errorf("boom") }), ActionUncertain},
	}
	for _, c := range cases {
		d := ResolvePending(ctx, PendingIntent{Intent: c.intent}, c.probe)
		if d.Action != c.want {
			t.Errorf("%s: action = %s, want %s (%s)", c.name, d.Action, c.want, d.Reason)
		}
	}
}

func TestShouldCheckpointEveryTwenty(t *testing.T) {
	for _, s := range []int{20, 40, 60} {
		if !ShouldCheckpoint(s) {
			t.Errorf("step %d should checkpoint", s)
		}
	}
	for _, s := range []int{0, 1, 19, 21} {
		if ShouldCheckpoint(s) {
			t.Errorf("step %d should not checkpoint", s)
		}
	}
}

func TestHashRequestStable(t *testing.T) {
	a := HashRequest("abc")
	b := HashRequest("abc")
	c := HashRequest("abd")
	if a != b || a == c {
		t.Fatal("request hash must be stable and sensitive")
	}
}
