package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func scriptedWriteThenDone(target, content string) []providers.ChatResponse {
	args, _ := json.Marshal(map[string]string{"path": target, "content": content})
	return []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "write_file", Arguments: args}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "wrote " + target}},
	}
}

// TestExecEnginePathEndToEnd covers the embedded eval profile using
// all of the wiring through excalibur exec with a scripted fake
// provider: the real engine loop, the real tool pipeline with leases
// and shared history, deferred tools through search_tools/call_tool,
// vault redaction in outputs, and the Appendix F artifacts.
func TestExecEnginePathEndToEnd(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	fake := providerfake.New("e2e-embedded")
	fake.Responses = scriptedWriteThenDone(target, "hello embedded")
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write hello", LogsDir: logs, Profile: ProfileEval,
		Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted || out.ExitCode != ExitOK {
		t.Fatalf("state = %q/%d", out.State, out.ExitCode)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "hello embedded" {
		t.Fatalf("file = %q, err = %v", data, err)
	}
	if len(fake.ChatRequests) != 2 {
		t.Fatalf("model calls = %d, want 2 (one tool turn, one final)", len(fake.ChatRequests))
	}
	// The tool definitions ride the prefix in fixed order so the
	// input cache is stable across steps.
	first, second := fake.ChatRequests[0], fake.ChatRequests[1]
	if len(first.Tools) == 0 || len(second.Tools) == 0 {
		t.Fatal("both model calls must carry the tool prefix")
	}
	if first.Tools[0].Name != second.Tools[0].Name {
		t.Fatal("tool prefix order must be stable across calls")
	}
	if err := telemetry.ValidateFile(out.ATIFPath); err != nil {
		t.Fatalf("ATIF invalid: %v", err)
	}
	for _, name := range []string{"profile.json", "result.json", "events.jsonl", "metrics.json"} {
		if _, err := os.Stat(filepath.Join(out.Dir, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}
}

// TestExecEnginePathDeferredCallTool covers deferred reachability in
// the embedded run: the scripted model searches for a deferred tool
// and invokes it through call_tool without breaking the cache prefix.
func TestExecEnginePathDeferredCallTool(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "note.txt")
	writeArgs, _ := json.Marshal(map[string]string{"path": target, "content": "v1"})
	searchArgs, _ := json.Marshal(map[string]string{"query": "file versions history"})
	callArgs, _ := json.Marshal(map[string]any{
		"name": "file_history", "arguments": map[string]any{"path": target},
	})
	fake := providerfake.New("e2e-deferred")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "write_file", Arguments: writeArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{
				{ID: "c2", Name: "search_tools", Arguments: searchArgs},
			}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{
				{ID: "c3", Name: "call_tool", Arguments: callArgs},
			}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "done with history"}},
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write and list history", LogsDir: logs, Profile: ProfileEval,
		Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if len(fake.ChatRequests) != 4 {
		t.Fatalf("model calls = %d, want 4", len(fake.ChatRequests))
	}
	raw, err := os.ReadFile(out.ATIFPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "file_history") {
		t.Fatalf("ATIF must show the deferred tool result, got %s", raw)
	}
}
