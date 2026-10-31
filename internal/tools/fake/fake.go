package fake

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// ScriptedTool is a scripted tools.Tool. Calls consume Results in order;
// when they run out, a StatusOK empty result is returned.
type ScriptedTool struct {
	Def     tools.Definition
	Results []tools.Result
	Err     error
	Keys    []string

	mu    sync.Mutex
	Calls []tools.Invocation
}

// New returns a ScriptedTool with a minimal valid definition.
func New(name string) *ScriptedTool {
	return &ScriptedTool{
		Def: tools.Definition{
			Name:        name,
			Description: "scripted test tool",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Effect:      "read_only",
			Idempotency: "pure",
			Cancel:      "immediate",
		},
	}
}

// Definition implements tools.Tool.
func (t *ScriptedTool) Definition() tools.Definition { return t.Def }

// Invoke implements tools.Tool.
func (t *ScriptedTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Calls = append(t.Calls, inv)
	if t.Err != nil {
		return tools.Result{}, t.Err
	}
	if len(t.Results) == 0 {
		return tools.Result{Status: tools.StatusOK}, nil
	}
	res := t.Results[0]
	t.Results = t.Results[1:]
	return res, nil
}

// ResourceKeys implements tools.Tool.
func (t *ScriptedTool) ResourceKeys(_ json.RawMessage) ([]string, error) {
	return append([]string(nil), t.Keys...), nil
}

// Recorded returns a copy of the received invocations.
func (t *ScriptedTool) Recorded() []tools.Invocation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]tools.Invocation(nil), t.Calls...)
}
