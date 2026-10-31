package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ToolDeps bundles the collaborators of the Section 13.7 and 16.4
// deferred tools. All fields are optional; tools report a clear error
// when the collaborator they need is missing.
type ToolDeps struct {
	History   *History
	Snapshots *SnapshotManager
	Worktrees *WorktreeManager
	Queue     *IntegrationQueue
	Registry  *Registry
	Modes     *ModeStore
	RunID     string
}

// RegisterWorkspaceTools registers the five H7 deferred tools through
// the shared Registry interface: no privileged path (Section 14.12).
// Names, effects and cancel modes follow Section 13.1.
func RegisterWorkspaceTools(reg tools.Registry, deps *ToolDeps) error {
	if deps == nil {
		deps = &ToolDeps{}
	}
	all := []tools.Tool{
		&FileHistoryTool{Deps: deps},
		&RevertEditTool{Deps: deps},
		&WorkspaceSnapshotTool{Deps: deps},
		&WorktreeTool{Deps: deps},
		&IntegrationQueueTool{Deps: deps},
	}
	for _, t := range all {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// WorkspaceToolNames lists the five tools of this package.
func WorkspaceToolNames() []string {
	return []string{"file_history", "revert_edit", "workspace_snapshot", "worktree", "integration_queue"}
}

func workspaceSchema(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

// FileHistoryTool implements file_history: list the recorded versions
// of a file oldest first (Section 13.7).
type FileHistoryTool struct{ Deps *ToolDeps }

func (t *FileHistoryTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "file_history", Description: "List the recorded edit versions of a file with run, tool call and time",
		InputSchema: workspaceSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]any{"type": "string", "minLength": 1},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			},
			"required": []string{"path"}, "additionalProperties": false,
		}),
		Effect: types.EffectReadOnly, Idempotency: types.IdemPure,
		Timeout: 30 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Cancel:    types.CancelImmediate,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "files", Tags: []string{"file", "history"}, Keywords: []string{"history", "file", "versions", "edits"},
	}
}

func (t *FileHistoryTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.Path) == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "path is required"}, nil
	}
	if t.Deps == nil || t.Deps.History == nil {
		return tools.Result{Status: tools.StatusToolError, Output: "file_history: no history wired"}, nil
	}
	vers := t.Deps.History.Versions(a.Path)
	limit := a.Limit
	if limit <= 0 || limit > len(vers) {
		limit = len(vers)
	}
	if limit > 0 {
		vers = vers[len(vers)-limit:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d version(s) of %s:\n", len(vers), a.Path)
	for _, v := range vers {
		fmt.Fprintf(&b, "- %s run=%s tool=%s at=%s pre=%s post=%s\n",
			v.ID, v.RunID, v.ToolCall, v.At.UTC().Format(time.RFC3339), v.Preimage, v.Postimage)
	}
	return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
}

func (t *FileHistoryTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"file:" + a.Path}, nil
}

// RevertEditTool implements revert_edit: restore one edit by id with
// a three-way merge when the file changed afterwards (Section 13.7).
type RevertEditTool struct{ Deps *ToolDeps }

func (t *RevertEditTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "revert_edit", Description: "Restore one recorded edit by id, merging when the file changed afterwards",
		InputSchema: workspaceSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"edit_id": map[string]any{"type": "string", "minLength": 1},
			},
			"required": []string{"edit_id"}, "additionalProperties": false,
		}),
		Effect: types.EffectWriteLocal, Idempotency: types.IdemKeyed,
		Timeout: 60 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Cancel:    types.CancelAfterCompletion,
		Inverse:   "edit_file",
		Probe:     "file content equals the edit preimage or the clean merge",
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "files", Tags: []string{"file", "revert"}, Keywords: []string{"revert", "edit", "undo", "history"},
	}
}

func (t *RevertEditTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		EditID string `json:"edit_id"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.EditID) == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "edit_id is required"}, nil
	}
	if t.Deps == nil || t.Deps.History == nil {
		return tools.Result{Status: tools.StatusToolError, Output: "revert_edit: no history wired"}, nil
	}
	runID := ""
	if t.Deps != nil {
		runID = t.Deps.RunID
	}
	newID, err := t.Deps.History.Revert(a.EditID, runID, "revert_edit")
	if err != nil {
		if c, ok := err.(*ConflictError); ok {
			return tools.Result{Status: tools.StatusToolError, Output: c.Error()}, nil
		}
		return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
	}
	return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("reverted %s as %s", a.EditID, newID)}, nil
}

func (t *RevertEditTool) ResourceKeys(args json.RawMessage) ([]string, error) { return nil, nil }

// WorkspaceSnapshotTool implements workspace_snapshot: take, list,
// diff and restore shadow images of a project tree (Section 13.7).
type WorkspaceSnapshotTool struct{ Deps *ToolDeps }

func (t *WorkspaceSnapshotTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "workspace_snapshot", Description: "Take, list, diff and restore shadow snapshots of a project tree",
		InputSchema: workspaceSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":      map[string]any{"type": "string", "enum": []string{"take", "list", "diff", "restore"}},
				"project":     map[string]any{"type": "string"},
				"project_dir": map[string]any{"type": "string"},
				"snapshot_id": map[string]any{"type": "string"},
				"paths":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"prune_added": map[string]any{"type": "boolean"},
				"step":        map[string]any{"type": "integer", "minimum": 0},
			},
			"required": []string{"action"}, "additionalProperties": false,
		}),
		Effect: types.EffectWriteLocal, Idempotency: types.IdemKeyed,
		Timeout: 120 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Cancel:    types.CancelAfterCompletion,
		Inverse:   "workspace_snapshot restore",
		Probe:     "snapshot manifest exists and diff is empty after restore",
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "files", Tags: []string{"snapshot", "workspace"}, Keywords: []string{"snapshot", "shadow", "restore", "workspace", "undo"},
	}
}

func (t *WorkspaceSnapshotTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Action     string   `json:"action"`
		Project    string   `json:"project"`
		ProjectDir string   `json:"project_dir"`
		SnapshotID string   `json:"snapshot_id"`
		Paths      []string `json:"paths"`
		PruneAdded bool     `json:"prune_added"`
		Step       int      `json:"step"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if t.Deps == nil || t.Deps.Snapshots == nil {
		return tools.Result{Status: tools.StatusToolError, Output: "workspace_snapshot: no snapshot store wired"}, nil
	}
	sm := t.Deps.Snapshots
	switch a.Action {
	case "take":
		if a.ProjectDir == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir is required for take"}, nil
		}
		snap, err := sm.Take(a.Project, a.ProjectDir, t.Deps.RunID, a.Step)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("snapshot %s: %d file(s), %d skipped", snap.ID, len(snap.Files), len(snap.Skipped))}, nil
	case "list":
		snaps := sm.List(a.Project)
		var b strings.Builder
		fmt.Fprintf(&b, "%d snapshot(s):\n", len(snaps))
		for _, s := range snaps {
			fmt.Fprintf(&b, "- %s step=%d run=%s at=%s files=%d skipped=%d\n",
				s.ID, s.Step, s.RunID, s.At.UTC().Format(time.RFC3339), len(s.Files), len(s.Skipped))
		}
		return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
	case "diff":
		if a.ProjectDir == "" || a.SnapshotID == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir and snapshot_id are required for diff"}, nil
		}
		changed, added, deleted, err := sm.Diff(a.ProjectDir, a.SnapshotID)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "changed: %s\nadded: %s\ndeleted: %s",
			strings.Join(changed, ", "), strings.Join(added, ", "), strings.Join(deleted, ", "))
		return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
	case "restore":
		if a.ProjectDir == "" || a.SnapshotID == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir and snapshot_id are required for restore"}, nil
		}
		restored, err := sm.Restore(a.ProjectDir, a.SnapshotID, RestoreOptions{Paths: a.Paths, PruneAdded: a.PruneAdded})
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		sort.Strings(restored)
		return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("restored %d file(s): %s", len(restored), strings.Join(restored, ", "))}, nil
	default:
		return tools.Result{Status: tools.StatusInputError, Output: fmt.Sprintf("unknown action %q: want take, list, diff or restore", a.Action)}, nil
	}
}

func (t *WorkspaceSnapshotTool) ResourceKeys(args json.RawMessage) ([]string, error) { return nil, nil }

// WorktreeTool implements worktree: create integration branches and
// per-child worktrees outside the project root, and list them
// (Section 16.4).
type WorktreeTool struct{ Deps *ToolDeps }

func (t *WorktreeTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "worktree", Description: "Create integration branches and per-child worktrees outside the project root",
		InputSchema: workspaceSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":       map[string]any{"type": "string", "enum": []string{"create_integration", "create_child", "list", "remove_child"}},
				"project_dir":  map[string]any{"type": "string"},
				"project":      map[string]any{"type": "string"},
				"task":         map[string]any{"type": "string"},
				"child":        map[string]any{"type": "string"},
				"integration":  map[string]any{"type": "string"},
				"confirm_user": map[string]any{"type": "boolean"},
			},
			"required": []string{"action"}, "additionalProperties": false,
		}),
		Effect: types.EffectExec, Idempotency: types.IdemKeyed,
		Timeout: 120 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Cancel:    types.CancelCooperative,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "workspace", Tags: []string{"worktree", "git"}, Keywords: []string{"worktree", "branch", "parallel", "integration"},
	}
}

func (t *WorktreeTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Action      string `json:"action"`
		ProjectDir  string `json:"project_dir"`
		Project     string `json:"project"`
		Task        string `json:"task"`
		Child       string `json:"child"`
		Integration string `json:"integration"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if t.Deps == nil || t.Deps.Worktrees == nil {
		return tools.Result{Status: tools.StatusToolError, Output: "worktree: no worktree manager wired"}, nil
	}
	m := t.Deps.Worktrees
	switch a.Action {
	case "create_integration":
		if a.ProjectDir == "" || a.Task == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir and task are required"}, nil
		}
		branch, err := m.CreateIntegration(a.ProjectDir, a.Task)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: "integration branch " + branch}, nil
	case "create_child":
		if a.ProjectDir == "" || a.Task == "" || a.Child == "" || a.Integration == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir, task, child and integration are required"}, nil
		}
		if m.HasSubmodules(a.ProjectDir) {
			return tools.Result{Status: tools.StatusToolError, Output: "project has submodules; using shared tree (Section 16.4)"}, nil
		}
		branch, path, err := m.CreateChild(a.ProjectDir, a.Project, a.Task, a.Child, a.Integration)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("child branch %s at %s", branch, path)}, nil
	case "list":
		if a.ProjectDir == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir is required"}, nil
		}
		paths, err := m.ListWorktrees(a.ProjectDir)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: "worktrees:\n" + strings.Join(paths, "\n")}, nil
	case "remove_child":
		if a.ProjectDir == "" || a.Child == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir and child are required"}, nil
		}
		if err := m.RemoveChild(a.ProjectDir, a.Project, a.Child); err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: "removed worktree of " + a.Child}, nil
	default:
		return tools.Result{Status: tools.StatusInputError, Output: fmt.Sprintf("unknown action %q", a.Action)}, nil
	}
}

func (t *WorktreeTool) ResourceKeys(args json.RawMessage) ([]string, error) { return nil, nil }

// IntegrationQueueTool implements integration_queue: enqueue child
// branches, process them one by one (rebase, resolve, impacted
// tests, fast-forward or return), report status, and propose the
// integration branch to the user without merging (Section 16.4).
type IntegrationQueueTool struct{ Deps *ToolDeps }

func (t *IntegrationQueueTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "integration_queue", Description: "Enqueue child branches and integrate them one by one onto the integration branch",
		InputSchema: workspaceSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":      map[string]any{"type": "string", "enum": []string{"enqueue", "process", "process_all", "status", "propose"}},
				"branch":      map[string]any{"type": "string"},
				"child":       map[string]any{"type": "string"},
				"project_dir": map[string]any{"type": "string"},
				"task":        map[string]any{"type": "string"},
				"integration": map[string]any{"type": "string"},
			},
			"required": []string{"action"}, "additionalProperties": false,
		}),
		Effect: types.EffectExec, Idempotency: types.IdemKeyed,
		Timeout: 300 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Cancel:    types.CancelCooperative,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "workspace", Tags: []string{"integration", "queue"}, Keywords: []string{"integration", "queue", "rebase", "merge"},
	}
}

func (t *IntegrationQueueTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Action      string `json:"action"`
		Branch      string `json:"branch"`
		Child       string `json:"child"`
		ProjectDir  string `json:"project_dir"`
		Task        string `json:"task"`
		Integration string `json:"integration"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if t.Deps == nil || t.Deps.Queue == nil {
		return tools.Result{Status: tools.StatusToolError, Output: "integration_queue: no queue wired"}, nil
	}
	q := t.Deps.Queue
	switch a.Action {
	case "enqueue":
		if a.Branch == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "branch is required"}, nil
		}
		q.Enqueue(a.Child, a.Branch)
		return tools.Result{Status: tools.StatusOK, Output: "enqueued " + a.Branch}, nil
	case "process":
		more, rep, err := q.ProcessNext(ctx)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		if !more {
			return tools.Result{Status: tools.StatusOK, Output: "queue empty"}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("%s: %s\n%s", rep.Branch, rep.Status, rep.Detail)}, nil
	case "process_all":
		reps, err := q.ProcessAll(ctx)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d processed:\n", len(reps))
		for _, r := range reps {
			fmt.Fprintf(&b, "- %s: %s\n", r.Branch, r.Status)
		}
		return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
	case "status":
		var b strings.Builder
		for _, it := range q.Status() {
			fmt.Fprintf(&b, "- %s (%s): %s\n", it.Branch, it.Status, it.Report)
		}
		if b.Len() == 0 {
			return tools.Result{Status: tools.StatusOK, Output: "queue empty"}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
	case "propose":
		if t.Deps.Worktrees == nil || a.ProjectDir == "" || a.Task == "" || a.Integration == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "project_dir, task and integration are required for propose"}, nil
		}
		p, err := t.Deps.Worktrees.Propose(a.ProjectDir, a.Task, a.Integration)
		if err != nil {
			return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
		}
		return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf(
			"propose %s at %s (%d files; merging needs user confirmation)",
			p.IntegrationBranch, p.HeadCommit, len(p.Files))}, nil
	default:
		return tools.Result{Status: tools.StatusInputError, Output: fmt.Sprintf("unknown action %q", a.Action)}, nil
	}
}

func (t *IntegrationQueueTool) ResourceKeys(args json.RawMessage) ([]string, error) { return nil, nil }
