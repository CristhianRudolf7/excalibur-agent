package tools

import "testing"

func TestExecAndPollOwnTypedSessionReceipts(t *testing.T) {
	tool := execTestTool(t)
	defer closeSessions(tool.Sessions)
	result := callTool(t, tool, map[string]any{"command": "printf 'mentioned sess-999 alive: true'", "timeout_s": 2})
	if len(result.Sessions) != 1 || result.Sessions[0].ID == "sess-999" || result.Sessions[0].Alive || result.Sessions[0].ExitCode == nil || *result.Sessions[0].ExitCode != 0 {
		t.Fatalf("bad process receipt: %+v", result.Sessions)
	}
	id := result.Sessions[0].ID
	poll := callTool(t, &ExecIOTool{Sessions: tool.Sessions}, map[string]any{"action": "read", "session": id})
	if len(poll.Sessions) != 1 || poll.Sessions[0].ID != id || poll.Sessions[0].Alive {
		t.Fatalf("bad poll receipt: %+v", poll.Sessions)
	}
	listed := callTool(t, &ExecIOTool{Sessions: tool.Sessions}, map[string]any{"action": "list", "owner": "unknown-owner"})
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != id {
		t.Fatal("receipt list differs from rendered owner fallback")
	}
	t.Setenv("EXCALIBUR_TYPED_SESSION_EVIDENCE", "0")
	result = callTool(t, tool, map[string]any{"command": "true", "timeout_s": 2})
	if result.Sessions != nil {
		t.Fatal("switch ignored")
	}
}
