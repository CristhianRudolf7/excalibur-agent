package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestCacheTelemetryIncludesToolPayloadsAndWrites(t *testing.T) {
	dir := t.TempDir()
	rec, err := eval.NewTrialRecorder(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := recordingModelEndpoint{base: errorUsageEndpoint{usage: providers.Usage{Input: 100, CacheRead: 60, CacheWrite: 30}}, run: &ExecRun{Recorder: rec}}
	req := providers.ChatRequest{Messages: []providers.Message{
		{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "one", Name: "exec", Arguments: json.RawMessage(`{"command":"echo one"}`)}}},
		{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "one", Name: "exec", Output: "one"}},
	}}
	for i := 0; i < 3; i++ {
		_, _, _ = m.Chat(context.Background(), types.RoleExecutor, req)
		if i == 0 {
			req.Messages[0].ToolCalls[0].Arguments = json.RawMessage(`{"command":"echo two"}`)
		}
		if i == 1 {
			req.Messages[1].ToolResult.Output = "two (masked observation)"
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "model_calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []eval.ModelCall
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var call eval.ModelCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	if len(calls) != 3 {
		t.Fatal(len(calls))
	}
	for _, key := range []string{"msg_0_assistant", "msg_0_tool_calls", "request_payload"} {
		if calls[0].LayerHashes[key] == calls[1].LayerHashes[key] {
			t.Fatalf("arguments invisible in %s", key)
		}
	}
	for _, key := range []string{"msg_1_tool", "msg_1_tool_result", "request_payload"} {
		if calls[1].LayerHashes[key] == calls[2].LayerHashes[key] {
			t.Fatalf("result invisible in %s", key)
		}
	}
	for _, call := range calls {
		if call.CacheRead != 60 || call.CacheWrite != 30 || call.InputTokens != 100 || call.LayerSizes["msg_0_tool_calls"] == 0 || call.LayerSizes["msg_1_tool_result"] == 0 {
			t.Fatalf("incomplete accounting %+v", call)
		}
	}
}
