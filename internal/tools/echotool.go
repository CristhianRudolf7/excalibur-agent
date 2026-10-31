package tools

import (
	"context"
	"encoding/json"
)

// EchoTool is a deterministic test double for extension fixtures
// (builtin.json in example extensions, ext test bindings): it
// validates like any catalog tool and returns a fixed output.
type EchoTool struct {
	def    Definition
	output string
}

// NewEchoTool returns an echo tool with the given definition.
func NewEchoTool(def Definition, output string) *EchoTool {
	return &EchoTool{def: def, output: output}
}

// Definition implements Tool.
func (t *EchoTool) Definition() Definition { return t.def }

// Invoke implements Tool.
func (t *EchoTool) Invoke(_ context.Context, _ Invocation) (Result, error) {
	return Result{Status: StatusOK, Output: t.output}, nil
}

// ResourceKeys implements Tool.
func (t *EchoTool) ResourceKeys(_ json.RawMessage) ([]string, error) {
	return []string{"echo:" + t.def.Name}, nil
}
