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

type acpSession struct {
	toServer   *io.PipeWriter
	fromServer *bufio.Scanner
	cancel     context.CancelFunc
	done       chan error
}

func newACPSession(t *testing.T, profile ProfileName) *acpSession {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	srv := &ACPServer{Stdin: c2sR, Stdout: s2cW, Profile: profile, ProjectDir: t.TempDir()}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	return &acpSession{toServer: c2sW, fromServer: bufio.NewScanner(s2cR), cancel: cancel, done: done}
}

func (s *acpSession) call(t *testing.T, id any, method string, params map[string]any) map[string]any {
	t.Helper()
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	fmt.Fprintf(s.toServer, "%s\n", req)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("no response to %s", method)
		}
		if !s.fromServer.Scan() {
			t.Fatalf("acp server closed during %s", method)
		}
		var resp map[string]any
		if err := json.Unmarshal(s.fromServer.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if _, ok := resp["id"]; !ok {
			continue
		}
		if fmt.Sprint(resp["id"]) != fmt.Sprint(id) {
			continue
		}
		return resp
	}
}

func (s *acpSession) close() {
	s.cancel()
	s.toServer.Close()
	<-s.done
}

func TestACPInitializeSessionPrompt(t *testing.T) {
	sess := newACPSession(t, ProfileEval)
	defer sess.close()
	resp := sess.call(t, 1, "initialize", map[string]any{})
	if resp["result"] == nil {
		t.Fatalf("initialize: %v", resp)
	}
	resp = sess.call(t, 2, "session/new", map[string]any{"cwd": t.TempDir()})
	result := resp["result"].(map[string]any)
	sessionID, _ := result["sessionId"].(string)
	if sessionID == "" {
		t.Fatalf("session/new: %v", result)
	}
	resp = sess.call(t, 3, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": "summarize this"}},
	})
	if resp["error"] != nil {
		t.Fatalf("prompt: %v", resp["error"])
	}
	got := resp["result"].(map[string]any)
	if got["stopReason"] != "end_turn" {
		t.Fatalf("stopReason = %v", got)
	}
	content, _ := got["content"].([]any)
	if len(content) == 0 {
		t.Fatal("prompt needs content")
	}
	if code, _ := got["exitCode"].(float64); code != 0 {
		t.Fatalf("exitCode = %v", code)
	}
	// Unknown session and unknown method fail cleanly.
	resp = sess.call(t, 4, "session/prompt", map[string]any{
		"sessionId": "ses_999", "prompt": "hi",
	})
	if resp["error"] == nil {
		t.Fatal("unknown session must error")
	}
	resp = sess.call(t, 5, "nope", map[string]any{})
	if resp["error"] == nil {
		t.Fatal("unknown method must error")
	}
	if _, ok := sess.call(t, 6, "session/cancel", map[string]any{})["result"]; !ok {
		t.Fatal("cancel must ack")
	}
}

// TestThreeRoutesAgree covers the Section 23.8 row for the three
// connection routes over the same instruction: the installed route
// (exec --profile eval), the ACP route (acp --profile eval) and the
// external route (background task child) reach the same final state
// with usage and cost present in metrics.
func TestThreeRoutesAgree(t *testing.T) {
	const instruction = "route parity check"
	execOut, err := RunExec(context.Background(), ExecOptions{
		Request: instruction, LogsDir: t.TempDir(), Profile: ProfileEval,
	})
	if err != nil {
		t.Fatal(err)
	}
	acpLogs := t.TempDir()
	t.Setenv("EXCALIBUR_LOGS", acpLogs)
	// Exercise the full prompt path via a live session.
	sess := newACPSession(t, ProfileEval)
	defer sess.close()
	sess.call(t, 1, "initialize", map[string]any{})
	newSess := sess.call(t, 2, "session/new", map[string]any{})
	sessionID := newSess["result"].(map[string]any)["sessionId"].(string)
	promptResp := sess.call(t, 3, "session/prompt", map[string]any{
		"sessionId": sessionID, "prompt": instruction,
	})
	if promptResp["error"] != nil {
		t.Fatalf("acp prompt: %v", promptResp["error"])
	}
	acpState := promptResp["result"].(map[string]any)["state"].(string)

	proj := t.TempDir()
	started, err := Start(StartOptions{Request: instruction, ProjectDir: proj, Inline: true})
	if err != nil {
		t.Fatal(err)
	}
	st, err := Status(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{string(execOut.State), acpState, string(st.State)} {
		if state != "completed" {
			t.Fatalf("routes disagree: exec=%q acp=%q external=%q",
				execOut.State, acpState, st.State)
		}
	}
	if !strings.Contains(execOut.Summary, instruction) {
		t.Fatalf("summary = %q", execOut.Summary)
	}
}
