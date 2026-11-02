package tools

import (
	"context"
	"encoding/json"
)

func InputRestoreEnabled() bool { return switchDefaultOn("EXCALIBUR_INPUT_RESTORE") }

// RestoreInputTool exposes manifest recovery without exposing private state.
type RestoreInputTool struct {
	Restore func(path, destination string) error
}

func (t *RestoreInputTool) Definition() Definition {
	return Definition{Name: "restore_input", Description: "Restore a preserved original task input by its original path; optional destination makes a disposable copy inside the task tree. Only manifest inputs are available; hashes are verified.", InputSchema: schemaFor(map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "destination": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}), Effect: "write_local", Idempotency: "non_idempotent", Cancel: "graceful", Timeout: 30000000000, OutputBudget: DefaultOutputBudget, Platforms: []string{"linux"}, Profiles: []string{"eval", "delegate", "default"}}
}
func (t *RestoreInputTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Path        string `json:"path"`
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if !InputRestoreEnabled() || t.Restore == nil || a.Path == "" {
		return Result{Status: StatusInputError, Output: "input restore unavailable or path missing"}, nil
	}
	if err := t.Restore(a.Path, a.Destination); err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	return Result{Status: StatusOK, Output: "Preserved original restored; SHA-256 verified."}, nil
}
func (t *RestoreInputTool) ResourceKeys(json.RawMessage) ([]string, error) {
	return []string{"input-restore"}, nil
}
