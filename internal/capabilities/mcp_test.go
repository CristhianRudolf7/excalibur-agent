package capabilities

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mcpTool(name string, ann *mcp.ToolAnnotations, meta map[string]any) *mcp.Tool {
	t := &mcp.Tool{Name: name, Description: "tool " + name, InputSchema: map[string]any{"type": "object"}, Annotations: ann}
	if meta != nil {
		t.Meta = mcp.Meta{"excalibur/": meta}
	}
	return t
}

func TestExcaliburMetaExplicitWins(t *testing.T) {
	ann := &mcp.ToolAnnotations{ReadOnlyHint: true}
	meta := map[string]any{"effect": "write_external", "idempotency": "keyed", "cancel": "cooperative", "fast": true, "group": "browser"}
	got := ExcaliburMeta(mcpTool("act", ann, meta))
	if got.Effect != "write_external" || got.Idempotency != "keyed" || got.Cancel != "cooperative" || !got.Fast || got.Group != "browser" {
		t.Fatalf("explicit _meta must win: %+v", got)
	}
}

func TestExcaliburMetaDerivationConservative(t *testing.T) {
	readOnly := ExcaliburMeta(mcpTool("r", &mcp.ToolAnnotations{ReadOnlyHint: true}, nil))
	if readOnly.Effect != "read_only" || readOnly.Idempotency != "pure" || readOnly.Cancel != "immediate" {
		t.Fatalf("readOnlyHint must map to read_only/pure/immediate: %+v", readOnly)
	}
	idem := ExcaliburMeta(mcpTool("w", &mcp.ToolAnnotations{IdempotentHint: true}, nil))
	if idem.Effect != "write_local" || idem.Idempotency != "idempotent" {
		t.Fatalf("idempotentHint must map conservatively: %+v", idem)
	}
	unknown := ExcaliburMeta(mcpTool("x", nil, nil))
	if unknown.Effect != "write_external" || unknown.Idempotency != "non_idempotent" || unknown.Cancel != "cooperative" {
		t.Fatalf("missing hints must be most conservative: %+v", unknown)
	}
}

func TestManifestFromMCPToolsNaming(t *testing.T) {
	tools := []*mcp.Tool{
		mcpTool("get_issue", nil, nil),
		mcpTool("A Very Long Tool Name That Surely Exceeds The Sixty Four Character Provider Limit XYZ", nil, nil),
	}
	m := ManifestFromMCPTools("GitHub", tools)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(m.Operations) != 2 {
		t.Fatalf("want 2 ops, got %d", len(m.Operations))
	}
	found := false
	for _, op := range m.Operations {
		if op.Name == "mcp__github__get_issue" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing mcp__github__get_issue in %+v", m.Operations)
	}
	for _, op := range m.Operations {
		if len(op.Name) > 64 {
			t.Fatalf("name %q exceeds 64 chars", op.Name)
		}
	}
	// Deterministic order regardless of server listing order.
	rev := []*mcp.Tool{tools[1], tools[0]}
	m2 := ManifestFromMCPTools("GitHub", rev)
	if m.Operations[0].Name != m2.Operations[0].Name || m.Operations[1].Name != m2.Operations[1].Name {
		t.Fatal("manifest order must be deterministic")
	}
}

func TestTaskResultFromMCP(t *testing.T) {
	ok := TaskResultFromMCP("t1", &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}})
	if ok.State != StateCompleted {
		t.Fatalf("want completed, got %q", ok.State)
	}
	var payload map[string]any
	if err := json.Unmarshal(ok.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	failed := TaskResultFromMCP("t2", &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "boom"}}})
	if failed.State != StateFailed || failed.ErrorText != "boom" {
		t.Fatalf("want failed with text: %+v", failed)
	}
	working := TaskResultFromMCP("t3", &mcp.CallToolResult{Meta: mcp.Meta{"io.modelcontextprotocol/task": map[string]any{"taskId": "task-9"}}})
	if working.State != StateWorking || working.TaskID != "task-9" {
		t.Fatalf("task handle must yield working: %+v", working)
	}
}
