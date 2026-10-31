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

var getToolOutputSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"ref":    map[string]any{"type": "string"},
		"offset": map[string]any{"type": "integer", "minimum": 0},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100000},
	},
	"required":             []string{"ref"},
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

// GetToolOutputTool implements get_tool_output over the OutputStore.
type GetToolOutputTool struct {
	Outputs OutputStore
	History FileHistoryStore
}

func (t *GetToolOutputTool) store() OutputStore {
	if t.Outputs != nil {
		return t.Outputs
	}
	return NewMemoryOutputStore()
}

func (t *GetToolOutputTool) Definition() Definition {
	return Definition{
		Name: "get_tool_output", Description: "Retrieve the full output of a truncated or masked tool call",
		InputSchema: getToolOutputSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 10_000_000_000, OutputBudget: DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "control", Tags: []string{"output"}, Keywords: []string{"output", "full", "truncated", "blob"},
	}
}

func (t *GetToolOutputTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Ref    string `json:"ref"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	full, ok := t.store().Get(a.Ref)
	if !ok && t.History != nil {
		if b, ok2 := t.History.Blob(a.Ref); ok2 {
			full, ok = string(b), true
		}
	}
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

func (t *GetToolOutputTool) ResourceKeys(args json.RawMessage) ([]string, error) { return nil, nil }

// SnapshotProvider supplies the status snapshot; the orchestrator owns
// the data (Section 15.6). The tool only renders it deterministically.
type SnapshotProvider interface {
	Snapshot() map[string]any
}

// StatusTool implements status as a stub rendering the provider data.
type StatusTool struct {
	Provider SnapshotProvider
	Sessions *SessionManager
}

func (t *StatusTool) Definition() Definition {
	return Definition{
		Name: "status", Description: "Snapshot of jobs and agents: states, current tool, live output, budget",
		InputSchema: statusSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 10_000_000_000, OutputBudget: DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "control", Tags: []string{"status"}, Keywords: []string{"status", "jobs", "agents", "progress"},
	}
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
			if s.Tail != "" {
				fmt.Fprintf(&b, "  tail: %s\n", lastLine(s.Tail))
			}
		}
	}
	if len(keys) == 0 && (t.Sessions == nil || len(t.Sessions.Summaries("")) == 0) {
		b.WriteString("no runs; orchestrator provider not wired (H8)\n")
	}
	return Result{Status: StatusOK, Output: b.String()}, nil
}

func (t *StatusTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
