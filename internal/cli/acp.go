package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ACPProtocolVersion is the Agent Client Protocol version spoken by
// this adapter (Section 3.5: ACP lets editors use Excalibur as an
// agent). The adapter implements the session subset Harbor's generic
// acp agent needs: initialize, session/new, session/prompt and
// session/cancel over stdio. Richer routes (permission modes, session
// update streaming, Harbor usage conventions) stay future work and
// are recorded in docs/decisions.
const ACPProtocolVersion = "0.1.0"

// ACPServer adapts Excalibur to editors and to Harbor's generic acp
// route: `excalibur acp --profile eval` runs the instruction with the
// eval profile inside the trial container, like the installed route
// but without a dedicated adapter.
type ACPServer struct {
	Stdin  io.Reader
	Stdout io.Writer

	// Profile selects the execution profile (eval for benchmarks).
	Profile ProfileName
	// ProjectDir scopes non-eval runs.
	ProjectDir string

	mu       sync.Mutex
	sessions map[string]string
	next     int
}

// Serve runs the newline-delimited JSON-RPC loop until EOF or context
// cancellation.
func (s *ACPServer) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]string{}
	}
	s.mu.Unlock()
	in := bufio.NewScanner(s.Stdin)
	in.Buffer(make([]byte, 1024*1024), 1024*1024)
	out := bufio.NewWriter(s.Stdout)
	defer out.Flush()
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if !in.Scan() {
			return nil
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		id, hasID := msg["id"]
		method, _ := msg["method"].(string)
		if method == "" || !hasID {
			continue
		}
		params, _ := msg["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
		}
		result, rpcErr := s.dispatch(ctx, method, params)
		resp := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		buf, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s\n", buf); err != nil {
			return err
		}
		out.Flush()
	}
}

func (s *ACPServer) dispatch(ctx context.Context, method string, params map[string]any) (any, map[string]any) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": ACPProtocolVersion,
			"agentCapabilities": map[string]any{
				"promptCapabilities": map[string]any{"text": true},
			},
			"agentInfo": map[string]any{"name": "excalibur", "version": Version},
		}, nil
	case "session/new":
		s.mu.Lock()
		s.next++
		id := fmt.Sprintf("ses_%d", s.next)
		cwd, _ := params["cwd"].(string)
		s.sessions[id] = cwd
		s.mu.Unlock()
		return map[string]any{"sessionId": id}, nil
	case "session/prompt":
		return s.prompt(ctx, params)
	case "session/cancel":
		return map[string]any{}, nil
	default:
		return nil, map[string]any{"code": -32601, "message": "unknown method " + method}
	}
}

func (s *ACPServer) prompt(ctx context.Context, params map[string]any) (any, map[string]any) {
	sessionID, _ := params["sessionId"].(string)
	s.mu.Lock()
	_, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return nil, map[string]any{"code": -32602, "message": "unknown session " + sessionID}
	}
	instruction := acpText(params["prompt"])
	if instruction == "" {
		return nil, map[string]any{"code": -32602, "message": "prompt is required"}
	}
	profile := s.Profile
	if profile == "" {
		profile = ProfileEval
	}
	outcome, err := RunExec(ctx, ExecOptions{
		Request:    instruction,
		ProjectDir: s.ProjectDir,
		Profile:    profile,
	})
	if err != nil {
		if code := ExitCode(err); code != 0 {
			return nil, map[string]any{"code": -32000, "message": err.Error()}
		}
		return nil, map[string]any{"code": -32603, "message": err.Error()}
	}
	return map[string]any{
		"stopReason": "end_turn",
		"content":    []any{map[string]any{"type": "text", "text": outcome.Summary}},
		"state":      string(outcome.State),
		"exitCode":   outcome.ExitCode,
		"taskId":     string(outcome.TaskID),
		"runId":      string(outcome.RunID),
	}, nil
}

// acpText flattens ACP prompt blocks to plain instruction text.
func acpText(prompt any) string {
	blocks, ok := prompt.([]any)
	if !ok {
		if s, ok := prompt.(string); ok {
			return s
		}
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		m, ok := blk.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := m["text"].(string); ok {
			b.WriteString(t)
		} else if t, ok := m["content"].(string); ok {
			b.WriteString(t)
		}
	}
	return b.String()
}

// Ensure the task-state import stays referenced while the ACP route
// reports Section 6.5 states only.
var _ = types.RunCompleted
