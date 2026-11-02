package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// CodeDiagramToolName projects the real code graph into visual artifacts.
const CodeDiagramToolName = "code_diagram"

// CodeDiagramTool shares the renderer used by diagram_render. RunID scopes its
// output directory to the run; an empty ID denotes a standalone invocation.
type CodeDiagramTool struct {
	Media *tools.MediaDeps
	RunID string
}

type diagramArgs struct {
	scope        string
	Path         string `json:"path"`
	Level        string `json:"level"`
	Focus        string `json:"focus"`
	Format       string `json:"format"`
	MaxNodes     int    `json:"max_nodes"`
	Direction    string `json:"direction"`
	IncludeTests bool   `json:"include_tests"`
	Output       string `json:"output"`
	Depth        int    `json:"depth"`
}

func (t *CodeDiagramTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        CodeDiagramToolName,
		Description: "Create repository diagrams from the real code graph: overview, module, symbols or call flow. Saves source and rendered artifacts; splits large overviews into module details. Use Graphviz for large graphs. No model calls or LSP required.",
		InputSchema: schemaFor(map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"path", "level"},
			"properties": map[string]any{
				"path":          map[string]any{"type": "string", "minLength": 1},
				"level":         map[string]any{"type": "string", "enum": []string{"overview", "module", "symbols", "flow"}},
				"focus":         map[string]any{"type": "string", "description": "Module directory/import path, symbol name, or entry function (flow). Use a symbol ID to disambiguate."},
				"format":        map[string]any{"type": "string", "enum": []string{"mermaid", "graphviz"}, "default": "mermaid"},
				"max_nodes":     map[string]any{"type": "integer", "minimum": 2, "maximum": 200, "default": 40},
				"direction":     map[string]any{"type": "string", "enum": []string{"TB", "BT", "LR", "RL"}, "default": "LR"},
				"include_tests": map[string]any{"type": "boolean", "default": false},
				"output":        map[string]any{"type": "string", "enum": []string{"svg", "png", "pdf"}, "default": "svg"},
				"depth":         map[string]any{"type": "integer", "minimum": 1, "maximum": 10, "default": 3},
			}}),
		Effect: types.EffectWriteLocal, Idempotency: types.IdemNonIdempotent,
		Cancel: types.CancelCooperative, Timeout: 60 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "code", Tags: []string{"code", "diagram"}, Keywords: []string{"repository", "codebase", "diagram", "architecture", "flow", "visualize"},
	}
}

func parseDiagramArgs(raw json.RawMessage) (diagramArgs, error) {
	a := diagramArgs{Format: "mermaid", MaxNodes: 40, Direction: "LR", Output: "svg", Depth: 3}
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, err
	}
	if a.Path == "" {
		return a, fmt.Errorf("path is required")
	}
	switch a.Level {
	case "overview", "module", "symbols", "flow":
	default:
		return a, fmt.Errorf("level must be overview, module, symbols or flow")
	}
	if a.Level == "flow" && a.Focus == "" {
		return a, fmt.Errorf("flow requires an entry function in focus")
	}
	if a.Format != "mermaid" && a.Format != "graphviz" {
		return a, fmt.Errorf("format must be mermaid or graphviz")
	}
	if a.Output != "svg" && a.Output != "png" && a.Output != "pdf" {
		return a, fmt.Errorf("output must be svg, png or pdf")
	}
	if a.MaxNodes < 2 || a.MaxNodes > 200 {
		return a, fmt.Errorf("max_nodes must be between 2 and 200")
	}
	if a.Depth < 1 || a.Depth > 10 {
		return a, fmt.Errorf("depth must be between 1 and 10")
	}
	switch a.Direction {
	case "TB", "BT", "LR", "RL":
	default:
		return a, fmt.Errorf("direction must be TB, BT, LR or RL")
	}
	return a, nil
}

func (t *CodeDiagramTool) ResourceKeys(raw json.RawMessage) ([]string, error) {
	a, err := parseDiagramArgs(raw)
	if err != nil {
		return nil, err
	}
	return []string{"project:" + a.Path}, nil
}

func (t *CodeDiagramTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	a, err := parseDiagramArgs(inv.Arguments)
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	return t.render(ctx, a)
}
