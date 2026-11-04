package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Daemon-first routing (Section 3.6): when the daemon is active the
// CLI, the MCP server and the ACP adapter pass tasks through its API
// so they stay visible to the master and other clients; otherwise
// they run embedded. Every wrapper below tries the gate first and
// falls back to the embedded file-backed implementation when the
// daemon is down or does not know the task.

// daemonAddr resolves the gate address: env override, then the
// runtime rendezvous file (mirrors the daemon's platform runtime
// dir without importing it so cross-builds stay clean).
func daemonAddr() (string, bool) {
	if addr, ok := daemon.ResolveAddr(cliRuntimeDir()); ok {
		return addr, true
	}
	return "", false
}

//lint:ignore U1000 used by the non-Windows runtime path hooks
func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// gateCall performs one JSON-RPC call against the daemon. Unreachable
// daemons map to daemon.ErrDaemonDown for the embedded fallback.
func gateCall(ctx context.Context, method string, params any) (json.RawMessage, error) {
	addr, ok := daemonAddr()
	if !ok || !gateway.DaemonActive(addr) {
		return nil, daemon.ErrDaemonDown
	}
	c := gateway.Dial(addr, "cli")
	res, err := c.Call(ctx, "cli-1", method, params)
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		return nil, err
	}
	return res, nil
}

// useDaemon reports whether gate routing is available. Callers fall
// back to embedded execution when false.
func useDaemon() (string, bool) {
	addr, ok := daemonAddr()
	if !ok || !gateway.DaemonActive(addr) {
		return "", false
	}
	return addr, true
}

// GateJSON performs one gate call and returns the raw result. It is
// the thin routing the excalibur CLI families use when the daemon is
// running (Sections 3.5, 3.6).
func GateJSON(method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return gateCall(ctx, method, params)
}

// DaemonReachable reports whether gate routing is available.
func DaemonReachable() bool {
	_, ok := useDaemon()
	return ok
}

func isUnknownTarget(err error) bool {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == api.CodeUnknownTarget
	}
	return false
}

// gateStatus maps a daemon task snapshot onto the CLI status shape.
func gateStatus(raw json.RawMessage) (*TaskStatus, error) {
	var snap struct {
		TaskID    types.TaskID    `json:"task_id"`
		Status    types.TaskState `json:"status"`
		RunState  types.RunState  `json:"run_state"`
		LastLine  string          `json:"last_line"`
		Progress  string          `json:"progress"`
		Question  string          `json:"question,omitempty"`
		Project   string          `json:"project_dir,omitempty"`
		Requested string          `json:"requested,omitempty"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &TaskStatus{
		TaskID: snap.TaskID, Short: string(snap.TaskID),
		State: snap.Status, Summary: snap.Progress,
		Question: snap.Question, UpdatedAt: time.Now().UTC(),
	}, nil
}

// tryGateStatus routes status/wait/reply/pause/resume/cancel through
// the daemon. ok=false means the caller runs embedded.
func tryGateStatus(method, projectDir, id, text string, timeoutSecs int64) (st *TaskStatus, ok bool, err error) {
	if _, active := useDaemon(); !active {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var raw json.RawMessage
	switch method {
	case api.MethodTaskStatus:
		raw, err = gateCall(ctx, method, map[string]any{"task_id": id})
	case api.MethodTaskWait:
		raw, err = gateCall(ctx, method, map[string]any{"task_id": id, "timeout_s": timeoutSecs})
	case api.MethodTaskReply:
		raw, err = gateCall(ctx, method, map[string]any{"task_id": id, "text": text})
	case api.MethodTaskPause, api.MethodTaskResume, api.MethodTaskCancel:
		raw, err = gateCall(ctx, method, map[string]any{"task_id": id})
	default:
		return nil, false, nil
	}
	if err != nil {
		if isUnknownTarget(err) {
			return nil, false, nil
		}
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	st, err = gateStatus(raw)
	if err != nil {
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	_ = projectDir
	return st, true, nil
}

// tryGateStart routes task start through the daemon.
func tryGateStart(opts StartOptions) (*StartResult, bool, error) {
	if _, active := useDaemon(); !active {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	params := map[string]any{"request": opts.Request, "project_dir": opts.ProjectDir}
	if opts.Profile != "" {
		params["profile"] = opts.Profile
	}
	if opts.BudgetUSD != nil {
		params["budget_usd"] = *opts.BudgetUSD
	}
	if opts.MaxSteps != nil {
		params["max_steps"] = *opts.MaxSteps
	}
	if opts.TimeoutS != nil {
		params["timeout_s"] = *opts.TimeoutS
	}
	if opts.NoMemory {
		params["no_memory"] = true
	}
	raw, err := gateCall(ctx, api.MethodTaskStart, params)
	if err != nil {
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	var res api.TaskStartResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	return &StartResult{TaskID: res.TaskID, Short: string(res.TaskID), Status: res.Status}, true, nil
}

// tryGateResult routes task result through the daemon.
func tryGateResult(projectDir, id string) (*TaskResult, bool, error) {
	if _, active := useDaemon(); !active {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := gateCall(ctx, api.MethodTaskResult, map[string]any{"task_id": id})
	if err != nil {
		if isUnknownTarget(err) {
			return nil, false, nil
		}
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	var res struct {
		TaskID    types.TaskID `json:"task_id"`
		Done      bool         `json:"done"`
		Summary   string       `json:"summary"`
		RunState  string       `json:"run_state"`
		Question  string       `json:"pending,omitempty"`
		Requested string       `json:"requested,omitempty"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	state := types.TaskInProgress
	if res.Done {
		state = types.TaskCompleted
	}
	if res.Question != "" {
		state = types.TaskBlocked
	}
	_ = projectDir
	return &TaskResult{
		TaskID: res.TaskID, Short: string(res.TaskID), State: state,
		Summary: res.Summary, Reason: res.Question,
	}, true, nil
}

// tryGateList routes task list through the daemon.
func tryGateList(projectDir string) ([]*TaskStatus, bool, error) {
	if _, active := useDaemon(); !active {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := gateCall(ctx, api.MethodTaskList, map[string]any{})
	if err != nil {
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		// The daemon may wrap the list; accept {"tasks": [...]} too.
		var wrapped struct {
			Tasks []json.RawMessage `json:"tasks"`
		}
		if werr := json.Unmarshal(raw, &wrapped); werr != nil {
			return nil, true, &CodedError{Code: ExitError, Err: err}
		}
		list = wrapped.Tasks
	}
	out := make([]*TaskStatus, 0, len(list))
	for _, item := range list {
		st, err := gateStatus(item)
		if err != nil {
			continue
		}
		out = append(out, st)
	}
	_ = projectDir
	return out, true, nil
}

// tryGateLogs routes task logs through the daemon.
func tryGateLogs(projectDir, id string) ([]map[string]any, bool, error) {
	if _, active := useDaemon(); !active {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := gateCall(ctx, api.MethodTaskLogs, map[string]any{"task_id": id})
	if err != nil {
		if isUnknownTarget(err) {
			return nil, false, nil
		}
		return nil, true, &CodedError{Code: ExitError, Err: err}
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, true, &CodedError{Code: ExitError, Err: fmt.Errorf("bad task logs: %w", err)}
	}
	_ = projectDir
	return out, true, nil
}
