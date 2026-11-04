package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

const codingTextLimit = 8192
const codingPathLimit = 512
const codingFileLimit = 50

// codingActivity belongs only to the daemon's interactive runners. It observes
// recorded versions, never the working tree, so other runs cannot contaminate diffs.
type codingActivity struct {
	bus                                         *telemetry.Bus
	history                                     *workspace.History
	redact                                      func(string) string
	conversation, run, parent, turn, root, name string
	steps                                       []string
	started                                     time.Time
	calls                                       atomic.Uint64
}

func (a *assistantTurns) codingActivity(conv, run, parent, turn, root, name string, steps []string) *codingActivity {
	redact := vault.NewRedactor().Redact
	if a.services.engineOptions.Vault != nil {
		redact = a.services.engineOptions.Vault.Redact
	}
	c := &codingActivity{bus: a.backend.Bus, history: a.services.History, redact: redact, conversation: conv, run: run, parent: parent, turn: turn, root: root, name: name, started: time.Now()}
	for _, step := range steps[:min(len(steps), 20)] {
		value, _ := c.text(step, 512, false)
		c.steps = append(c.steps, value)
	}
	c.name, _ = c.text(name, 128, false)
	return c
}

// Redact before clipping: a credential straddling a byte boundary must never leak.
func (c *codingActivity) text(s string, limit int, tail bool) (string, bool) {
	s = c.redact(s)
	if len(s) <= limit {
		return s, false
	}
	if tail {
		s = s[len(s)-limit:]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[1:]
		}
	} else {
		s = s[:limit]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
	}
	return s, true
}
func (c *codingActivity) publish(kind types.EventType, payload any) {
	if c == nil || c.bus == nil {
		return
	}
	id := types.RunID(c.run)
	_, _ = c.bus.Publish(kind, &id, nil, payload)
}
func (c *codingActivity) progress(state types.RunState) {
	if c == nil {
		return
	}
	done := 0
	if state == types.RunCompleted {
		done = len(c.steps)
	}
	c.publish(types.EventRunProgress, api.RunProgressEvent{RunID: types.RunID(c.run), ConversationID: c.conversation, ParentRunID: c.parent, TurnID: c.turn, Name: c.name, Steps: c.steps, State: state, PlanDone: done, PlanTotal: len(c.steps), ElapsedMS: time.Since(c.started).Milliseconds()})
}
func (c *codingActivity) call(name string, args json.RawMessage, opts tools.ExecuteOptions) api.ToolCallEvent {
	id := opts.CallID
	if id == "" {
		id = fmt.Sprintf("activity-%d", c.calls.Add(1))
	}
	var input struct {
		Path    string `json:"path"`
		Command string `json:"command"`
		Session string `json:"session"`
	}
	_ = json.Unmarshal(args, &input)
	path, _ := c.text(input.Path, codingPathLimit, false)
	id, _ = c.text(id, 128, false)
	session, _ := c.text(input.Session, 128, false)
	name, _ = c.text(name, 128, false)
	command, clipped := c.text(input.Command, 2048, false)
	kind := "tool"
	switch name {
	case "read_file", "glob", "grep":
		kind = "read"
	case "write_file", "edit_file", "apply_patch", "revert_edit":
		kind = "edit"
	case "exec", "exec_io":
		kind = "command"
	}
	return api.ToolCallEvent{RunID: types.RunID(c.run), ConversationID: c.conversation, ParentRunID: c.parent, TurnID: c.turn, CallID: id, Tool: name, Kind: kind, Path: path, Command: command, SessionID: session, Status: "running", Truncated: clipped}
}

// ResourceKeys resolves the tool's actual paths (including multi-file patches).
// Version IDs sampled before execution exclude older and concurrent writes.
func (c *codingActivity) versions(reg *tools.MapRegistry, name string, args json.RawMessage) map[string]map[string]bool {
	paths := map[string]map[string]bool{}
	if c.history == nil {
		return paths
	}
	t, err := reg.Get(name)
	if err != nil {
		return paths
	}
	keys, _ := t.ResourceKeys(args)
	if name == "revert_edit" {
		var input struct {
			EditID string `json:"edit_id"`
		}
		_ = json.Unmarshal(args, &input)
		if v, ok := c.history.Get(input.EditID); ok {
			keys = append(keys, "file:"+v.Path)
		}
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, "file:") || len(paths) >= codingFileLimit {
			continue
		}
		path := strings.TrimPrefix(key, "file:")
		ids := map[string]bool{}
		for _, v := range c.history.Versions(path) {
			ids[v.ID] = true
		}
		paths[path] = ids
	}
	return paths
}
func (c *codingActivity) edits(call api.ToolCallEvent, before map[string]map[string]bool) {
	paths := make([]string, 0, len(before))
	for path := range before {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		ids := before[path]
		for _, v := range c.history.Versions(path) {
			if ids[v.ID] || v.RunID != c.run {
				continue
			}
			pre, _ := c.history.Blob(v.Preimage)
			post, _ := c.history.Blob(v.Postimage)
			// Redact images before inserting diff prefixes, which would otherwise
			// split multiline literal secrets and prevent exact matching.
			if workspace.IsText(pre) && workspace.IsText(post) {
				pre = []byte(c.redact(string(pre)))
				post = []byte(c.redact(string(post)))
			}
			h := workspace.NewHistory()
			h.RecordFileWithID(v.ID, path, pre, post, c.run, call.CallID, v.At, v.Created, v.Deleted)
			for _, change := range h.Changes([]string{c.run}, []string{c.root}) {
				var diff strings.Builder
				for _, hunk := range change.Hunks {
					fmt.Fprintf(&diff, "@@ -%d,%d +%d,%d @@\n", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
					for _, line := range hunk.Lines {
						diff.WriteString(line)
						diff.WriteByte('\n')
					}
				}
				text, clipped := c.text(diff.String(), codingTextLimit, false)
				p, _ := c.text(filepath.ToSlash(change.Path), codingPathLimit, false)
				c.publish(types.EventFileEdited, api.FileEditEvent{RunID: types.RunID(c.run), ConversationID: c.conversation, ParentRunID: c.parent, TurnID: c.turn, CallID: call.CallID, EditID: v.ID, Path: p, Diff: text, Added: change.Added, Removed: change.Removed, Truncated: clipped || change.Truncated, Binary: change.Binary})
			}
		}
	}
}
func (r *daemonToolRunner) executeActivity(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (res tools.Result, err error) {
	c := r.coding
	if c == nil {
		return r.ex.Execute(ctx, name, args, opts)
	}
	call := c.call(name, args, opts)
	before := c.versions(r.reg, name, args)
	c.publish(types.EventToolCalled, call)
	started := time.Now()
	defer func() {
		call.Status = "completed"
		if err != nil || res.Status != tools.StatusOK {
			call.Status = "failed"
		}
		output := res.Output
		if err != nil {
			output += "\n" + err.Error()
		}
		var clipped bool
		call.Output, clipped = c.text(output, codingTextLimit, true)
		call.Truncated = call.Truncated || clipped || res.Truncated
		call.ExitCode = res.ExitCode
		if len(res.Sessions) == 1 {
			call.SessionID, _ = c.text(res.Sessions[0].ID, 128, false)
			if res.Sessions[0].Alive && call.Status == "completed" {
				call.Status = "running"
			}
		}
		call.ElapsedMS = time.Since(started).Milliseconds()
		c.publish(types.EventToolCompleted, call)
		c.edits(call, before)
	}()
	return r.ex.Execute(ctx, name, args, opts)
}
