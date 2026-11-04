package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const chatChildWait = 10 * time.Minute

// waitChatStatus holds the coordinator's tool call instead of spending model
// steps polling a child. Scope checks still run through the original tool.
func waitChatStatus(ctx context.Context, timeout time.Duration, read func() (tools.Result, error)) (tools.Result, error) {
	first, err := read()
	if err != nil || first.Status != tools.StatusOK {
		return first, err
	}
	type progress struct {
		State types.RunState `json:"state"`
		Text  string         `json:"last_progress"`
	}
	var baseline progress
	if json.Unmarshal([]byte(first.Output), &baseline) != nil || baseline.State.IsTerminal() || baseline.State == types.RunPaused || baseline.State == types.RunBlocked {
		return first, nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	latest := first
	for {
		select {
		case <-ctx.Done():
			return tools.Result{Status: tools.StatusCanceled, Output: "Child wait cancelled; the child retains its independent lifetime."}, nil
		case <-timer.C:
			return latest, nil
		case <-ticker.C:
			latest, err = read()
			if err != nil || latest.Status != tools.StatusOK {
				return latest, err
			}
			var next progress
			if json.Unmarshal([]byte(latest.Output), &next) != nil || next.State != baseline.State || next.Text != baseline.Text {
				return latest, nil
			}
		}
	}
}

// Only the daemon advertises await. The embedded/eval schema remains frozen.
func chatSubagentDefinition(def tools.Definition) tools.Definition {
	var schema map[string]any
	if json.Unmarshal(def.InputSchema, &schema) != nil {
		return def
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return def
	}
	action, ok := properties["action"].(map[string]any)
	if !ok {
		return def
	}
	values, ok := action["enum"].([]any)
	if !ok {
		return def
	}
	action["enum"] = append(values, "await")
	def.InputSchema, _ = json.Marshal(schema)
	def.Description += " In daemon chat, await waits up to ten minutes for child progress or completion. Coordinators' status on a running child also waits; avoid polling."
	return def
}
