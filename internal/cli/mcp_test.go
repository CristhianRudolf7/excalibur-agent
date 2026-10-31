package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// mcpSession drives a Server over in-memory pipes for one test.
type mcpSession struct {
	toServer   *io.PipeWriter
	fromServer *bufio.Scanner
	cancel     context.CancelFunc
	done       chan error
}

func newMCPSession(t *testing.T) *mcpSession {
	t.Helper()
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	srv := &MCPServer{Stdin: clientToServerR, Stdout: serverToClientW}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	return &mcpSession{
		toServer:   clientToServerW,
		fromServer: bufio.NewScanner(serverToClientR),
		cancel:     cancel,
		done:       done,
	}
}

func (s *mcpSession) call(t *testing.T, id any, method string, params map[string]any) map[string]any {
	t.Helper()
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	fmt.Fprintf(s.toServer, "%s\n", req)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("no response to %s", method)
		}
		if !s.fromServer.Scan() {
			t.Fatalf("server closed during %s", method)
		}
		var resp map[string]any
		if err := json.Unmarshal(s.fromServer.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if _, isNote := resp["id"]; !isNote {
			continue
		}
		if fmt.Sprint(resp["id"]) != fmt.Sprint(id) {
			continue
		}
		return resp
	}
}

func (s *mcpSession) close() {
	s.cancel()
	s.toServer.Close()
	<-s.done
}

func TestMCPInitializeAndTools(t *testing.T) {
	sess := newMCPSession(t)
	defer sess.close()
	resp := sess.call(t, 1, "initialize", map[string]any{})
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != MCPProtocolVersion {
		t.Fatalf("protocol = %v", result["protocolVersion"])
	}
	resp = sess.call(t, 2, "tools/list", map[string]any{})
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("tools = %d, want 7", len(tools))
	}
	want := map[string]bool{}
	for _, tool := range tools {
		name := tool.(map[string]any)["name"].(string)
		want[name] = true
	}
	for _, name := range []string{
		"excalibur_delegate", "excalibur_status", "excalibur_wait",
		"excalibur_result", "excalibur_reply", "excalibur_cancel", "excalibur_list",
	} {
		if !want[name] {
			t.Fatalf("missing tool %s", name)
		}
	}
	resp = sess.call(t, 3, "tools/call", map[string]any{"name": "nope", "arguments": map[string]any{}})
	if resp["error"] == nil {
		t.Fatal("unknown tool must error")
	}
}

func mcpCall(t *testing.T, sess *mcpSession, id int, name string, args map[string]any) (string, bool) {
	t.Helper()
	resp := sess.call(t, id, "tools/call", map[string]any{"name": name, "arguments": args})
	if rpcErr, ok := resp["error"]; ok {
		t.Fatalf("tool %s rpc error: %v", name, rpcErr)
	}
	result := resp["result"].(map[string]any)
	contents := result["content"].([]any)
	text := contents[0].(map[string]any)["text"].(string)
	_, isErr := result["isError"]
	return text, isErr
}

// TestMCPDelegateFlow covers the Section 3.7 delegation row over the
// MCP server: delegate a small task in a git repository, wait for it
// and collect the result, with identical reports across modes.
func TestMCPDelegateFlow(t *testing.T) {
	proj := t.TempDir()
	sess := newMCPSession(t)
	defer sess.close()
	text, isErr := mcpCall(t, sess, 1, "excalibur_delegate", map[string]any{
		"instruction": "fix it", "project_dir": proj, "wait": false,
	})
	if isErr {
		t.Fatalf("delegate: %s", text)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(text), &receipt); err != nil {
		t.Fatalf("delegate receipt is not JSON: %s", text)
	}
	taskID, _ := receipt["task_id"].(string)
	if taskID == "" {
		t.Fatalf("receipt = %s", text)
	}
	waitText, _ := mcpCall(t, sess, 2, "excalibur_wait", map[string]any{
		"task_id": taskID, "project_dir": proj, "timeout_s": 30,
	})
	if !strings.Contains(waitText, "completed") {
		t.Fatalf("wait = %s", waitText)
	}
	resText, _ := mcpCall(t, sess, 3, "excalibur_result", map[string]any{
		"task_id": taskID, "project_dir": proj,
	})
	if !strings.Contains(resText, "completed") {
		t.Fatalf("result = %s", resText)
	}
	cliText, _ := mcpCall(t, sess, 4, "excalibur_status", map[string]any{
		"task_id": taskID, "project_dir": proj,
	})
	if !strings.Contains(cliText, "completed") {
		t.Fatalf("status = %s", cliText)
	}
	listText, _ := mcpCall(t, sess, 5, "excalibur_list", map[string]any{"project_dir": proj})
	if !strings.Contains(listText, taskID) {
		t.Fatalf("list = %s", listText)
	}
	cancelText, isCancelErr := mcpCall(t, sess, 6, "excalibur_cancel", map[string]any{
		"task_id": taskID, "project_dir": proj,
	})
	if !isCancelErr || !strings.Contains(cancelText, "already") {
		t.Fatalf("cancel of a terminal task must be a tool error, got %q (isErr=%v)", cancelText, isCancelErr)
	}
}

// TestMCPQuestionReply covers the Section 3.7 row where a delegated
// task needs a datum: wait returns the question, reply answers it,
// and never_ask assumes instead.
func TestMCPQuestionReply(t *testing.T) {
	proj := t.TempDir()
	sess := newMCPSession(t)
	defer sess.close()
	text, _ := mcpCall(t, sess, 1, "excalibur_delegate", map[string]any{
		"instruction": "ship it {{ask: which tag?}}", "project_dir": proj, "wait": false,
	})
	var receipt map[string]any
	if err := json.Unmarshal([]byte(text), &receipt); err != nil {
		t.Fatalf("receipt: %s", text)
	}
	taskID := receipt["task_id"].(string)
	waitText, _ := mcpCall(t, sess, 2, "excalibur_wait", map[string]any{
		"task_id": taskID, "project_dir": proj, "timeout_s": 10,
	})
	if !strings.Contains(waitText, "which tag?") {
		t.Fatalf("wait must return the question: %s", waitText)
	}
	replyText, _ := mcpCall(t, sess, 3, "excalibur_reply", map[string]any{
		"task_id": taskID, "text": "v1", "project_dir": proj,
	})
	if !strings.Contains(replyText, "completed") {
		t.Fatalf("reply must resume to completion: %s", replyText)
	}
	// never_ask assumes and records the assumption.
	text2, _ := mcpCall(t, sess, 4, "excalibur_delegate", map[string]any{
		"instruction": "ship it {{ask: which tag?}}", "project_dir": proj,
		"wait": true, "never_ask": true,
	})
	if !strings.Contains(text2, "completed") || !strings.Contains(text2, "which tag?") {
		t.Fatalf("never_ask must assume and record: %s", text2)
	}
}
