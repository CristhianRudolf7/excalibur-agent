package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

var clockSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"now", "convert", "add"}},
		"zone":   map[string]any{"type": "string"},
		"time":   map[string]any{"type": "string"},
		"from":   map[string]any{"type": "string"},
		"to":     map[string]any{"type": "string"},
		"add":    map[string]any{"type": "string"},
		"format": map[string]any{"type": "string"},
	},
	"additionalProperties": false,
})

var statusSchema = schemaFor(map[string]any{
	"type":                 "object",
	"properties":           map[string]any{},
	"additionalProperties": false,
})

// ClockTool implements clock: deterministic time without model math.
type ClockTool struct {
	Now func() time.Time
}

func (t *ClockTool) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *ClockTool) Definition() Definition {
	return Definition{
		Name: "clock", Description: "Current time in any zone, conversions and date arithmetic",
		InputSchema: clockSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 10_000_000_000, OutputBudget: 4096, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "control", Tags: []string{"time"}, Keywords: []string{"clock", "time", "date", "timezone"},
	}
}

func (t *ClockTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Action string `json:"action"`
		Zone   string `json:"zone"`
		Time   string `json:"time"`
		From   string `json:"from"`
		To     string `json:"to"`
		Add    string `json:"add"`
		Format string `json:"format"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if a.Action == "" {
		a.Action = "now"
	}
	zone := a.Zone
	if zone == "" {
		zone = "UTC"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Result{Status: StatusInputError, Output: fmt.Sprintf("bad zone %q: %v", zone, err)}, nil
	}
	switch a.Action {
	case "now":
		n := t.now().In(loc)
		return Result{Status: StatusOK, Output: n.Format(time.RFC3339) + " " + loc.String()}, nil
	case "convert":
		if a.Time == "" {
			return Result{Status: StatusInputError, Output: "time is required for convert"}, nil
		}
		from := a.From
		if from == "" {
			from = "UTC"
		}
		fromLoc, err := time.LoadLocation(from)
		if err != nil {
			return Result{Status: StatusInputError, Output: fmt.Sprintf("bad from zone %q: %v", from, err)}, nil
		}
		parsed, err := parseTime(a.Time, fromLoc)
		if err != nil {
			return Result{Status: StatusInputError, Output: fmt.Sprintf("bad time %q: %v", a.Time, err)}, nil
		}
		to := a.To
		if to == "" {
			to = zone
		}
		toLoc, err := time.LoadLocation(to)
		if err != nil {
			return Result{Status: StatusInputError, Output: fmt.Sprintf("bad to zone %q: %v", to, err)}, nil
		}
		return Result{Status: StatusOK, Output: parsed.In(toLoc).Format(time.RFC3339) + " " + toLoc.String()}, nil
	case "add":
		if a.Time == "" || a.Add == "" {
			return Result{Status: StatusInputError, Output: "time and add are required for add"}, nil
		}
		parsed, err := parseTime(a.Time, loc)
		if err != nil {
			return Result{Status: StatusInputError, Output: fmt.Sprintf("bad time %q: %v", a.Time, err)}, nil
		}
		d, err := time.ParseDuration(a.Add)
		if err != nil {
			return Result{Status: StatusInputError, Output: fmt.Sprintf("bad duration %q: %v (want Go duration like 26h30m)", a.Add, err)}, nil
		}
		return Result{Status: StatusOK, Output: parsed.Add(d).In(loc).Format(time.RFC3339) + " " + loc.String()}, nil
	default:
		return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown action %q", a.Action)}, nil
	}
}

func (t *ClockTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func parseTime(s string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if tm, err := time.ParseInLocation(layout, s, loc); err == nil {
			return tm, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time (want RFC3339 or 2006-01-02[ 15:04:05])")
}

// getToolOutputSchemaLegacy is the schema before EXCALIBUR_TOOL_OUTPUT_REFS.
var getToolOutputSchemaLegacy = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"ref":    map[string]any{"type": "string"},
		"offset": map[string]any{"type": "integer", "minimum": 0},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100000},
	},
	"required":             []string{"ref"},
	"additionalProperties": false,
})

// getToolOutputSchema accepts a ref, or the list mode. ref is no longer
// required because {"list": true} stands on its own.
var getToolOutputSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"ref":    map[string]any{"type": "string", "description": "A blob-sha256 ref printed in a truncated result, a session id sess-N, or a call id call-N"},
		"offset": map[string]any{"type": "integer", "minimum": 0},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100000},
		"list":   map[string]any{"type": "boolean", "description": "true lists recent stored outputs and terminal sessions with their refs"},
	},
	"additionalProperties": false,
})

// outputListLimit bounds the get_tool_output list to the newest entries.
const outputListLimit = 20

// getToolOutputUsage names every valid form of the ref argument.
const getToolOutputUsage = `get_tool_output needs one of: {"ref": "blob-sha256:..."} (a ref printed in a truncated result), {"ref": "sess-N"} (a terminal session's full log, paged with offset and limit), {"ref": "call-N"} (an output call id from the list), or {"list": true} to list recent refs`

// callSummary labels the call that produced an output: the command for
// exec, otherwise the compact arguments.
func callSummary(name string, args json.RawMessage) string {
	if name == "exec" {
		var a struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(args, &a) == nil && strings.TrimSpace(a.Command) != "" {
			return clipText(a.Command, 120)
		}
	}
	return clipText(strings.TrimSpace(string(args)), 120)
}

// GetToolOutputTool implements get_tool_output over the OutputStore.
type GetToolOutputTool struct {
	Sessions *SessionManager
	Outputs  OutputStore
	History  FileHistoryStore
}

func (t *GetToolOutputTool) store() OutputStore {
	if t.Outputs != nil {
		return t.Outputs
	}
	return NewMemoryOutputStore()
}

func (t *GetToolOutputTool) Definition() Definition {
	schema := getToolOutputSchemaLegacy
	if OutputRefsEnabled() {
		schema = getToolOutputSchema
	}
	return Definition{
		Name: "get_tool_output", Description: "Retrieve stored tool output or a terminal session by ref (blob ref, sess-N or call-N); supports byte offset and limit; {\"list\": true} lists recent refs",
		InputSchema: schema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 10_000_000_000, OutputBudget: DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "control", Tags: []string{"output"}, Keywords: []string{"output", "full", "truncated", "blob", "session", "list"},
	}
}

func (t *GetToolOutputTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Ref    string `json:"ref"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
		List   bool   `json:"list"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if a.Offset < 0 {
		return Result{Status: StatusInputError, Output: "offset must be nonnegative"}, nil
	}
	refs := OutputRefsEnabled()
	if refs && a.List {
		return Result{Status: StatusOK, Output: t.listing()}, nil
	}
	if refs && strings.TrimSpace(a.Ref) == "" {
		return Result{Status: StatusInputError, Output: getToolOutputUsage}, nil
	}
	full, ok := t.resolve(a.Ref, refs)
	if !ok {
		return Result{Status: StatusInputError, Output: RetrievalHintForRef(a.Ref)}, nil
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 8000
	}
	if a.Offset > len(full) {
		a.Offset = len(full)
	}
	end := a.Offset + limit
	if end > len(full) {
		end = len(full)
	}
	out := full[a.Offset:end]
	more := ""
	if end < len(full) {
		more = fmt.Sprintf("\n…[%d of %d bytes shown; next_offset=%d]…", end-a.Offset, len(full), end)
	}
	return Result{Status: StatusOK, Output: fmt.Sprintf("bytes %d..%d of %d\n%s%s", a.Offset, end, len(full), out, more)}, nil
}

// resolve finds the full text behind a ref: a stored blob ref, a call
// id, a terminal session id, or a history blob, in that order. Call ids
// and session ids need EXCALIBUR_TOOL_OUTPUT_REFS; sessions also
// resolve under the older FrictionRepair switch.
func (t *GetToolOutputTool) resolve(ref string, refs bool) (string, bool) {
	store := t.store()
	if full, ok := store.Get(ref); ok {
		return full, true
	}
	if refs {
		if resolver, ok := store.(interface {
			ResolveCall(call string) (string, bool)
		}); ok {
			if blob, ok := resolver.ResolveCall(ref); ok {
				if full, ok := store.Get(blob); ok {
					return full, true
				}
			}
		}
	}
	if t.Sessions != nil && (refs || FrictionRepairEnabled()) {
		if _, exists := t.Sessions.Get(SessionID(ref)); exists {
			if full, _, _, err := t.Sessions.Read(SessionID(ref), 0); err == nil {
				return full, true
			}
		}
	}
	if t.History != nil {
		if b, ok := t.History.Blob(ref); ok {
			return string(b), true
		}
	}
	return "", false
}

// listing renders the list mode: the newest stored outputs with their
// blob ref, call id, size and producing call, then the terminal
// sessions, each with its full-log size and state.
func (t *GetToolOutputTool) listing() string {
	var b strings.Builder
	b.WriteString("recent stored outputs, newest first (fetch with get_tool_output {\"ref\": \"<ref or call id>\", \"offset\": 0, \"limit\": 8000}):\n")
	shown := 0
	if lister, ok := t.store().(interface {
		Records(limit int) []OutputRecord
	}); ok {
		for _, r := range lister.Records(outputListLimit) {
			fmt.Fprintf(&b, "- %s  %s  %d bytes  %s %s\n", r.Ref, r.Call, r.Bytes, r.Tool, r.Summary)
			shown++
		}
	}
	if shown == 0 {
		b.WriteString("- none yet\n")
	}
	if t.Sessions != nil {
		sums := t.Sessions.Summaries("")
		if len(sums) > 0 {
			b.WriteString("terminal sessions, newest first (their full log; offset pages through it):\n")
		}
		for i := len(sums) - 1; i >= 0 && i >= len(sums)-outputListLimit; i-- {
			s := sums[i]
			size := 0
			if sess, ok := t.Sessions.Get(s.ID); ok {
				size = len(sess.SnapshotFull())
			}
			state := "exited"
			if s.Alive {
				state = "alive"
			}
			fmt.Fprintf(&b, "- %s  %d bytes  %s  %s\n", s.ID, size, state, clipText(s.Cmd, 120))
		}
	}
	return b.String()
}

func (t *GetToolOutputTool) ResourceKeys(args json.RawMessage) ([]string, error) { return nil, nil }

// SnapshotProvider supplies the status snapshot; the orchestrator owns
// the data (Section 15.6). The tool only renders it deterministically.
type SnapshotProvider interface {
	Snapshot() map[string]any
}

// StatusTool implements status as a stub rendering the provider data.
type StatusTool struct {
	// Provisioner names missing dependencies in session tails
	// (ReportOnly); status never installs.
	Provisioner *Provisioner
	Provider    SnapshotProvider
	Sessions    *SessionManager
}

func (t *StatusTool) Definition() Definition {
	def := Definition{
		Name: "status", Description: "Snapshot of jobs and agents: states, current tool, live output, budget",
		InputSchema: statusSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 10_000_000_000, OutputBudget: DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "control", Tags: []string{"status"}, Keywords: []string{"status", "jobs", "agents", "progress"},
	}
	// Status only names missing dependencies (ReportOnly) and never
	// installs, so it stays read-only whatever the provisioning switch says.
	return def
}

func (t *StatusTool) Invoke(_ context.Context, _ Invocation) (Result, error) {
	data := map[string]any{}
	if t.Provider != nil {
		data = t.Provider.Snapshot()
	}
	var b strings.Builder
	b.WriteString("status snapshot\n")
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		raw, _ := json.Marshal(data[k])
		fmt.Fprintf(&b, "- %s: %s\n", k, string(raw))
	}
	if t.Sessions != nil {
		sums := t.Sessions.Summaries("")
		fmt.Fprintf(&b, "sessions: %d\n", len(sums))
		for _, s := range sums {
			fmt.Fprintf(&b, "- %s alive=%v cmd=%q\n", s.ID, s.Alive, s.Cmd)
			if session, ok := t.Sessions.Get(s.ID); ok {
				if prov := (&ExecTool{Provisioner: t.Provisioner}).provisioner(); prov != nil {
					b.WriteString(prov.ReportOnly(session.Cmd, s.Tail))
				}
			}
			for _, line := range lastLines(s.Tail, 3) {
				fmt.Fprintf(&b, "  tail: %s\n", line)
			}
		}
	}
	if len(keys) == 0 && (t.Sessions == nil || len(t.Sessions.Summaries("")) == 0) {
		b.WriteString("no runs or sessions yet\n")
	}
	return Result{Status: StatusOK, Output: b.String()}, nil
}

func (t *StatusTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// lastLines returns up to n trailing non-blank lines of s.
func lastLines(s string, n int) []string {
	var out []string
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			out = append([]string{lines[i]}, out...)
		}
	}
	return out
}
