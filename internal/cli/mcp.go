package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// MCPProtocolVersion is the MCP specification version this server
// speaks (Section 3.5: the official Go SDK pins 2026-07-28; the
// stdio wire here implements that version's tools/list and
// tools/call surface without the extra dependency).
const MCPProtocolVersion = "2026-07-28"

// MCPServer is the Section 3.6 delegation server over stdio: a few
// tools with short descriptions (P8) so any MCP-capable agent can
// delegate a task, follow its progress and collect the result
// without knowing Excalibur internals.
type MCPServer struct {
	Stdin  io.Reader
	Stdout io.Writer
}

// Serve runs the newline-delimited JSON-RPC loop until EOF or
// context cancellation.
func (s *MCPServer) Serve(ctx context.Context) error {
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
		if method == "" {
			continue
		}
		if !hasID {
			// Notification from client (e.g. notifications/initialized).
			continue
		}
		params, _ := msg["params"].(map[string]any)
		result, rpcErr := s.dispatch(ctx, method, params, func(n map[string]any) {
			note, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": "notifications/progress", "params": n,
			})
			fmt.Fprintf(out, "%s\n", note)
			out.Flush()
		})
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

func rpcError(code int, message string) map[string]any {
	return map[string]any{"code": code, "message": message}
}

func (s *MCPServer) dispatch(ctx context.Context, method string, params map[string]any, progress func(map[string]any)) (any, map[string]any) {
	if params == nil {
		params = map[string]any{}
	}
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": MCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}, "logging": map[string]any{}},
			"serverInfo":      map[string]any{"name": "excalibur", "version": Version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools()}, nil
	case "tools/call":
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		token := progressToken(params)
		text, isErr, err := callTool(ctx, name, args, func(p map[string]any) {
			if token != nil {
				p["progressToken"] = token
			}
			progress(p)
		})
		if err != nil {
			return nil, rpcError(-32602, err.Error())
		}
		res := map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
		if isErr {
			res["isError"] = true
		}
		return res, nil
	default:
		return nil, rpcError(-32601, "unknown method "+method)
	}
}

func progressToken(params map[string]any) any {
	meta, _ := params["_meta"].(map[string]any)
	if meta == nil {
		return nil
	}
	return meta["progressToken"]
}

// mcpTools declares the seven delegation tools (Section 3.6) with
// short descriptions (P8).
func mcpTools() []map[string]any {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "number"}
	intg := map[string]any{"type": "integer"}
	boolean := map[string]any{"type": "boolean"}
	obj := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	return []map[string]any{
		{"name": "excalibur_delegate", "description": "Delegate a self-contained task to Excalibur; returns a task id, or the final result with wait:true.",
			"inputSchema": obj(map[string]any{
				"instruction": str, "project_dir": str, "profile": str, "memory": boolean,
				"worktree": boolean, "budget_usd": num, "max_steps": intg, "timeout_s": intg,
				"wait": boolean, "never_ask": boolean, "output_schema": str,
			}, "instruction")},
		{"name": "excalibur_status", "description": "Status snapshot of a delegated task: state, step, plan progress and current tool line.",
			"inputSchema": obj(map[string]any{"task_id": str, "project_dir": str}, "task_id")},
		{"name": "excalibur_wait", "description": "Wait for completion or a pending question (max 600s per call); emits progress notifications.",
			"inputSchema": obj(map[string]any{"task_id": str, "timeout_s": intg, "project_dir": str}, "task_id")},
		{"name": "excalibur_result", "description": "Delivery report of a delegated task: summary, files, branch/diff, criteria, pending.",
			"inputSchema": obj(map[string]any{"task_id": str, "project_dir": str}, "task_id")},
		{"name": "excalibur_reply", "description": "Answer a pending question or add a directive to a delegated task.",
			"inputSchema": obj(map[string]any{"task_id": str, "text": str, "project_dir": str}, "task_id", "text")},
		{"name": "excalibur_cancel", "description": "Cancel a delegated task.",
			"inputSchema": obj(map[string]any{"task_id": str, "project_dir": str}, "task_id")},
		{"name": "excalibur_list", "description": "List delegated tasks, optionally filtered by project.",
			"inputSchema": obj(map[string]any{"project_dir": str})},
	}
}

func mcpString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func mcpFloat(args map[string]any, key string) (float64, bool) {
	switch v := args[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	default:
		return 0, false
	}
}

func mcpInt(args map[string]any, key string) (int64, bool) {
	switch v := args[key].(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

func callTool(ctx context.Context, name string, args map[string]any, progress func(map[string]any)) (string, bool, error) {
	projectDir := mcpString(args, "project_dir")
	taskID := mcpString(args, "task_id")
	switch name {
	case "excalibur_delegate":
		return mcpDelegate(ctx, args, progress)
	case "excalibur_status":
		if taskID == "" {
			return "", true, fmt.Errorf("task_id is required")
		}
		st, err := Status(projectDir, taskID)
		if err != nil {
			return err.Error(), true, nil
		}
		return statusText(st), false, nil
	case "excalibur_wait":
		if taskID == "" {
			return "", true, fmt.Errorf("task_id is required")
		}
		timeout, ok := mcpInt(args, "timeout_s")
		if !ok || timeout <= 0 || timeout > 600 {
			timeout = 600
			if ok && timeout > 600 {
				timeout = 600
			}
		}
		st, err := waitWithProgress(ctx, projectDir, taskID, timeout, progress)
		if err != nil {
			return err.Error(), true, nil
		}
		return statusText(st), false, nil
	case "excalibur_result":
		if taskID == "" {
			return "", true, fmt.Errorf("task_id is required")
		}
		res, err := Result(projectDir, taskID)
		if err != nil {
			return err.Error(), true, nil
		}
		return resultText(res), false, nil
	case "excalibur_reply":
		if taskID == "" {
			return "", true, fmt.Errorf("task_id is required")
		}
		text := mcpString(args, "text")
		if text == "" {
			return "", true, fmt.Errorf("text is required")
		}
		st, err := Reply(projectDir, taskID, text)
		if err != nil {
			return err.Error(), true, nil
		}
		return statusText(st), false, nil
	case "excalibur_cancel":
		if taskID == "" {
			return "", true, fmt.Errorf("task_id is required")
		}
		st, err := Cancel(projectDir, taskID)
		if err != nil {
			return err.Error(), true, nil
		}
		return statusText(st), false, nil
	case "excalibur_list":
		items, err := ListTasks(projectDir)
		if err != nil {
			return err.Error(), true, nil
		}
		var b strings.Builder
		for _, st := range items {
			fmt.Fprintf(&b, "%s %s %s\n", st.Short, st.TaskID, st.State)
		}
		if b.Len() == 0 {
			return "no tasks", false, nil
		}
		return strings.TrimRight(b.String(), "\n"), false, nil
	default:
		return "", true, fmt.Errorf("unknown tool %q", name)
	}
}

func mcpDelegate(ctx context.Context, args map[string]any, progress func(map[string]any)) (string, bool, error) {
	instruction := mcpString(args, "instruction")
	if instruction == "" {
		return "", true, fmt.Errorf("instruction is required")
	}
	so := StartOptions{Request: instruction, ProjectDir: mcpString(args, "project_dir")}
	if p := mcpString(args, "profile"); p != "" {
		so.Profile = p
	} else {
		so.Profile = string(ProfileDelegate)
	}
	if mem, ok := args["memory"].(bool); ok {
		so.NoMemory = !mem
	}
	if wt, ok := args["worktree"].(bool); ok {
		so.Worktree = &wt
	}
	if b, ok := mcpFloat(args, "budget_usd"); ok {
		so.BudgetUSD = &b
	}
	if ms, ok := mcpInt(args, "max_steps"); ok {
		v := int(ms)
		so.MaxSteps = &v
	}
	if to, ok := mcpInt(args, "timeout_s"); ok {
		so.TimeoutS = &to
	}
	if na, ok := args["never_ask"].(bool); ok {
		so.NeverAsk = na
	}
	wait, _ := args["wait"].(bool)
	started, err := Start(so)
	if err != nil {
		return "", true, err
	}
	if !wait {
		out, _ := json.Marshal(map[string]any{
			"task_id": string(started.TaskID), "short": started.Short, "status": string(started.Status),
		})
		return string(out), false, nil
	}
	st, err := waitWithProgress(ctx, mcpString(args, "project_dir"), string(started.TaskID), 600, progress)
	if err != nil {
		return "", true, err
	}
	res, err := Result(mcpString(args, "project_dir"), string(started.TaskID))
	if err != nil {
		return statusText(st), true, err
	}
	return resultText(res), false, nil
}

// waitWithProgress polls the task store while emitting MCP progress
// notifications (step, plan done/total, tool in flight) so the
// delegating agent does not poll in a loop (Section 3.6).
func waitWithProgress(ctx context.Context, projectDir, id string, timeoutSecs int64, progress func(map[string]any)) (*TaskStatus, error) {
	if timeoutSecs <= 0 || timeoutSecs > 600 {
		timeoutSecs = 600
	}
	deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		st, err := Status(projectDir, id)
		if err != nil {
			return nil, err
		}
		if progress != nil {
			progress(map[string]any{
				"progress": ticks, "message": fmt.Sprintf("task %s: %s", st.Short, st.State),
				"state": string(st.State), "task_id": string(st.TaskID),
			})
		}
		ticks++
		if isTerminalStatus(st.State) || st.Question != "" {
			return st, nil
		}
		if time.Now().After(deadline) {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func isTerminalStatus(s types.TaskState) bool {
	switch s {
	case types.TaskCompleted, types.TaskCompletedPending,
		types.TaskFailed, types.TaskCanceled:
		return true
	default:
		return false
	}
}

func statusText(st *TaskStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "task %s (%s): %s", st.Short, st.TaskID, st.State)
	if st.Reason != "" {
		fmt.Fprintf(&b, " reason=%s", st.Reason)
	}
	if st.Question != "" {
		fmt.Fprintf(&b, " question=%q", st.Question)
	}
	if st.Summary != "" {
		fmt.Fprintf(&b, " summary=%q", st.Summary)
	}
	if st.Branch != "" {
		fmt.Fprintf(&b, " branch=%s", st.Branch)
	}
	return b.String()
}

func resultText(res *TaskResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "task %s (%s): %s\n%s", res.Short, res.TaskID, res.State, res.Summary)
	if res.Branch != "" {
		fmt.Fprintf(&b, "\nbranch: %s", res.Branch)
	}
	if res.DiffStat != "" {
		fmt.Fprintf(&b, "\ndiff:\n%s", res.DiffStat)
	}
	if len(res.FilesChanged) > 0 {
		fmt.Fprintf(&b, "\nfiles: %s", strings.Join(res.FilesChanged, ", "))
	}
	if len(res.Assumptions) > 0 {
		fmt.Fprintf(&b, "\nassumptions: %s", strings.Join(res.Assumptions, "; "))
	}
	if res.Reason != "" {
		fmt.Fprintf(&b, "\nreason: %s", res.Reason)
	}
	return b.String()
}
