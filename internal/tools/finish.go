package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

var finishSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"summary":  map[string]any{"type": "string", "description": "what was done"},
		"evidence": map[string]any{"type": "string", "description": "commands run and their results proving each requirement"},
		"status":   map[string]any{"type": "string", "enum": []string{"completed", "blocked"}, "description": "completed (default) or blocked when genuinely stuck"},
		"reason":   map[string]any{"type": "string", "description": "concrete reason when blocked"},
	},
	"required":             []string{"summary", "evidence"},
	"additionalProperties": false,
})

// FinishTool implements the explicit finish control (agent-quality,
// Section 7.3 closing). It is a harness control tool, not a benchmark
// tool: the definition lives in the core prefix so the model always
// sees it in non-interactive profiles, but the frozen eval profile
// table (internal/eval) intentionally does not list it, so the
// profile hash never moves. The engine intercepts `finish` calls for
// the refusal/confirmation/blocked logic; Invoke only validates the
// argument shape so direct and call_tool invocations fail fast with
// guidance.
type FinishTool struct{}

func (t *FinishTool) Definition() Definition {
	return Definition{
		Name: "finish",
		Description: "End the run explicitly. Call ONLY when everything is verified: summary says what was done, " +
			"evidence lists the commands run and their results proving each requirement. " +
			"Use status blocked with a concrete reason only when genuinely stuck (missing data, file or permission). " +
			"Never stop with a text-only answer: always call finish with evidence. " +
			"Services the instruction requires must be left running (background:true sessions survive the run; declare keep_running in evidence); " +
			"stop temporary helpers with exec_io close. Killing or stopping a process the task requires is a failure, never a blocked reason.",
		InputSchema: finishSchema, Effect: "exec", Idempotency: "non_idempotent",
		Cancel: "immediate", Timeout: 10 * time.Second, OutputBudget: DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "control", Tags: []string{"finish", "control"}, Keywords: []string{"finish", "done", "complete", "submit"},
	}
}

// FinishArgs is the validated shape shared with the engine.
type FinishArgs struct {
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
}

// ParseFinishArgs validates one finish invocation.
func ParseFinishArgs(raw json.RawMessage) (FinishArgs, string) {
	var a FinishArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return FinishArgs{}, "finish refused: arguments must be a JSON object with summary and evidence"
	}
	a.Summary = strings.TrimSpace(a.Summary)
	a.Evidence = strings.TrimSpace(a.Evidence)
	a.Status = strings.ToLower(strings.TrimSpace(a.Status))
	a.Reason = strings.TrimSpace(a.Reason)
	if a.Status == "" {
		a.Status = "completed"
	}
	if a.Status != "completed" && a.Status != "blocked" {
		return FinishArgs{}, "finish refused: status must be \"completed\" or \"blocked\""
	}
	if a.Summary == "" {
		return FinishArgs{}, "finish refused: summary is required: say what was done"
	}
	if a.Status == "completed" && a.Evidence == "" {
		return FinishArgs{}, "finish refused: evidence is required: list the commands run and their results proving each requirement"
	}
	if a.Status == "blocked" && a.Reason == "" {
		return FinishArgs{}, "finish refused: reason is required when status is blocked: name the concrete missing data, file or permission"
	}
	return a, ""
}

func (t *FinishTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	if _, reason := ParseFinishArgs(inv.Arguments); reason != "" {
		return Result{Status: StatusInputError, Output: reason}, nil
	}
	return Result{Status: StatusOK, Output: "finish recorded by the harness"}, nil
}

func (t *FinishTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }
