package wireengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Embedded task-scoped surface for exec/eval single-run scope.
//
// In embedded mode there is no daemon, no memory service and no live
// child execution: the eight eval-profile tools that other milestones
// own as stubs (task_db, subagent, ack_directive, history_search,
// pin_observation, leave_continuation_note, search_skills, load_skill)
// would otherwise answer "not wired" to every call. The adapters
// below fill those stub providers with task-scoped in-memory
// behavior so every tool exposed in exec/eval works end to end:
// task_db plans against a MemoryStore, subagent/ack_directive act on
// an embedded orchestrator with a root agent, history/pins/notes keep
// run-local state, and skills resolve from the embedded builtin
// index. Wiring only fills providers, so the core prefix hash never
// moves (Sections 11.6, 13.3).

// SurfaceStores carries the task-scoped state behind the embedded
// adapters. It is built once per embedded catalog and shared by the
// adapters wired onto it.
type SurfaceStores struct {
	Tasks     *tasks.MemoryStore
	Orch      *orchestrator.Orchestrator
	OrchStore *orchestrator.MemoryStore
	Root      types.AgentID
	Scope     types.Scope
	History   *HistoryLog
	Pins      *PinStore
	Notes     *NoteStore
}

// EnsureSurfaceStores fills nil entries with task-scoped in-memory
// defaults and creates the root (master) agent the subagent and
// ack_directive adapters default to.
func EnsureSurfaceStores(runID string) *SurfaceStores {
	s := &SurfaceStores{
		Tasks:   tasks.NewMemoryStore(),
		History: NewHistoryLog(),
		Pins:    NewPinStore(),
		Notes:   NewNoteStore(),
	}
	if runID == "" {
		runID = "embedded"
	}
	s.Scope = types.Scope{Kind: types.ScopeTask, ID: runID}
	mem := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), mem, orchestrator.NoopJournal{})
	s.Orch = orch
	s.OrchStore = mem
	root := types.Agent{
		ID: types.NewAgentID(), Name: "embedded-master",
		Description: "embedded single-run root", Profile: string(agents.ProfileMaster),
		Level: 0, Scope: s.Scope, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = mem.SaveAgent(root)
	s.Root = root.ID
	return s
}

// HistoryLog is the run-local tool history behind history_search in
// embedded single-run scope (no memory service under eval).
type HistoryLog struct {
	mu      sync.Mutex
	entries []HistoryEntry
}

// HistoryEntry is one recorded tool call summary.
type HistoryEntry struct {
	At     time.Time `json:"at"`
	Tool   string    `json:"tool"`
	Args   string    `json:"args"`
	Output string    `json:"output"`
}

// NewHistoryLog returns an empty log.
func NewHistoryLog() *HistoryLog { return &HistoryLog{} }

// Append records one tool call summary.
func (l *HistoryLog) Append(tool, args, output string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	sum := output
	if len(sum) > 500 {
		sum = sum[:500] + "…"
	}
	l.entries = append(l.entries, HistoryEntry{At: time.Now().UTC(), Tool: tool, Args: args, Output: sum})
}

// Search returns up to k entries matching text and/or tool, newest
// last. Empty filters return the tail of the log.
func (l *HistoryLog) Search(text, tool string, k int) []HistoryEntry {
	if l == nil {
		return nil
	}
	if k <= 0 {
		k = 20
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t := strings.ToLower(text)
	var out []HistoryEntry
	for _, e := range l.entries {
		if tool != "" && e.Tool != tool {
			continue
		}
		if t != "" && !strings.Contains(strings.ToLower(e.Tool+" "+e.Args+" "+e.Output), t) {
			continue
		}
		out = append(out, e)
	}
	if len(out) > k {
		out = out[len(out)-k:]
	}
	return out
}

// PinStore keeps pin_observation texts for the run. The context
// masking layer reads them through HistoryLog-adjacent snapshots;
// the store is the ownership seam (Section 11.3.5).
type PinStore struct {
	mu   sync.Mutex
	pins []string
}

// NewPinStore returns an empty store.
func NewPinStore() *PinStore { return &PinStore{} }

// Pin records an observation.
func (p *PinStore) Pin(text string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pins = append(p.pins, text)
	return len(p.pins)
}

// List returns the pinned observations.
func (p *PinStore) List() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pins...)
}

// NoteStore keeps leave_continuation_note notes for the run; the
// latest note seeds the continuation context after restarts.
type NoteStore struct {
	mu    sync.Mutex
	notes []string
}

// NewNoteStore returns an empty store.
func NewNoteStore() *NoteStore { return &NoteStore{} }

// Add records a note.
func (n *NoteStore) Add(note string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notes = append(n.notes, note)
	return len(n.notes)
}

// Latest returns the newest note.
func (n *NoteStore) Latest() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.notes) == 0 {
		return ""
	}
	return n.notes[len(n.notes)-1]
}

// ---- adapters (tools.Tool over the surface stores) ----

// TaskDBAdapter runs task_db batches against the embedded MemoryStore.
type TaskDBAdapter struct {
	Store *tasks.MemoryStore
	Scope types.Scope
}

// Definition implements tools.Tool.
func (t *TaskDBAdapter) Definition() tools.Definition {
	return tools.Definition{
		Name: "task_db", Description: "Grouped transactional operations on projects, tasks, plans and evidence (embedded task scope)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"ops":{"type":"array","minItems":1}},"required":["ops"],"additionalProperties":true}`),
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemKeyed, Cancel: types.CancelAfterCompletion,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "work", Tags: []string{"task"}, Keywords: []string{"task", "plan", "project", "evidence"},
	}
}

// taskDBOpAliases maps common model guesses to real task_db op
// names (observed live: create_requirement, create_criterion).
var taskDBOpAliases = map[string]string{
	"create_requirement": "add_requirement",
	"create_criterion":   "add_criterion",
	"create_check":       "add_check",
	"create_plan_item":   "add_plan_item",
	"create_milestone":   "add_milestone",
	"create_dependency":  "add_dependency",
	"create_decision":    "add_decision",
	"create_assumption":  "add_assumption",
	"list":               "query",
	"search":             "query",
	"get":                "query",
}

// Invoke implements tools.Tool.
func (t *TaskDBAdapter) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	args := tools.NormalizeArgs("task_db", inv.Arguments, tools.DefaultSurfaceTuning())
	var body struct {
		Ops []map[string]any `json:"ops"`
	}
	if err := json.Unmarshal(args, &body); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if len(body.Ops) == 0 {
		return tools.Result{Status: tools.StatusInputError, Output: "ops must not be empty: pass {\"ops\": [{\"op\": \"query\", \"entity\": \"criteria\"}]} to start"}, nil
	}
	calls := make([]tasks.OpCall, 0, len(body.Ops))
	for _, m := range body.Ops {
		op, _ := m["op"].(string)
		op = strings.ToLower(strings.TrimSpace(op))
		if aliased, ok := taskDBOpAliases[op]; ok {
			op = aliased
		}
		if op == "" {
			return tools.Result{Status: tools.StatusInputError, Output: "every op needs an \"op\" name (query, create_task, add_requirement, add_criterion, record_evidence, ...)"}, nil
		}
		params := map[string]any{}
		for k, v := range m {
			if k != "op" {
				params[k] = v
			}
		}
		calls = append(calls, tasks.OpCall{Op: op, Params: params})
	}
	store := t.Store
	if store == nil {
		store = tasks.NewMemoryStore()
	}
	results, err := tasks.ApplyBatch(store, t.Scope, calls)
	if err != nil {
		out := err.Error()
		if strings.Contains(out, "unknown op") {
			out += " (valid ops: query, create_task, update_task, create_plan, add_plan_item, update_plan_item, add_dependency, add_milestone, add_requirement, update_requirement, add_criterion, update_criterion, add_check, record_evidence, add_assumption, add_decision, append_log)"
		}
		return tools.Result{Status: tools.StatusInputError, Output: out}, nil
	}
	raw, _ := json.Marshal(map[string]any{"results": results})
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, nil
}

// ResourceKeys implements tools.Tool.
func (t *TaskDBAdapter) ResourceKeys(args json.RawMessage) ([]string, error) {
	return []string{"taskdb:" + t.Scope.String()}, nil
}

// SubagentAdapter delegates to the orchestrator subagent tool with
// the embedded root as the default caller.
type SubagentAdapter struct {
	Inner     *orchestrator.SubagentTool
	Orch      *orchestrator.Orchestrator
	OrchStore *orchestrator.MemoryStore
	Root      types.AgentID
}

// Definition implements tools.Tool.
func (t *SubagentAdapter) Definition() tools.Definition {
	inner := t.inner()
	return inner.Definition()
}

func (t *SubagentAdapter) inner() *orchestrator.SubagentTool {
	if t.Inner != nil {
		return t.Inner
	}
	return &orchestrator.SubagentTool{Orch: t.Orch}
}

// Invoke implements tools.Tool.
func (t *SubagentAdapter) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	args := tools.NormalizeArgs("subagent", inv.Arguments, tools.DefaultSurfaceTuning())
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if m == nil {
		m = map[string]any{}
	}
	if _, ok := m["caller"]; !ok {
		m["caller"] = string(t.Root)
	} else if s, _ := m["caller"].(string); s == "" {
		m["caller"] = string(t.Root)
	} else if t.OrchStore != nil {
		if _, err := t.OrchStore.GetAgent(types.AgentID(s)); err != nil {
			m["caller"] = string(t.Root)
		}
	}
	if _, ok := m["action"]; !ok && !tools.FrictionRepairEnabled() {
		return tools.Result{Status: tools.StatusInputError, Output: subagentUsage()}, nil
	}
	raw, _ := json.Marshal(m)
	res, err := t.inner().Invoke(ctx, tools.Invocation{Tool: "subagent", Arguments: raw, IdempotencyKey: inv.IdempotencyKey})
	if err != nil {
		return res, err
	}
	if res.Status == tools.StatusOK {
		res.Output = withScopeNote(res.Output)
	}
	return res, nil
}

func subagentUsage() string {
	return "action is required: {\"action\": \"spawn|status|message|redirect|pause|resume|interrupt|cancel\", ...}. " +
		"Spawn example: {\"action\":\"spawn\",\"card\":{\"goal\":\"...\",\"deliverable\":\"...\"}}. " +
		"Caller defaults to this run; prefer doing the work directly in single-run scope."
}

func withScopeNote(out string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return out
	}
	m["scope_note"] = "embedded single-run scope: the child run is recorded; continue the work in this run"
	raw, err := json.Marshal(m)
	if err != nil {
		return out
	}
	return string(raw)
}

// ResourceKeys implements tools.Tool.
func (t *SubagentAdapter) ResourceKeys(args json.RawMessage) ([]string, error) {
	return t.inner().ResourceKeys(args)
}

// AckAdapter acknowledges directives on the embedded orchestrator.
type AckAdapter struct {
	Orch *orchestrator.Orchestrator
	Root types.AgentID
}

// Definition implements tools.Tool.
func (t *AckAdapter) Definition() tools.Definition {
	return tools.Definition{
		Name: "ack_directive", Description: "Acknowledge a directive and explain how it applies (embedded: acks pending root directives, else records the note)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"directive":{"type":"string"},"note":{"type":"string"}},"additionalProperties":true}`),
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemIdempotent, Cancel: types.CancelImmediate,
		Timeout: 30_000_000_000, OutputBudget: tools.DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "conversation", Tags: []string{"directive"}, Keywords: []string{"ack", "directive"},
	}
}

// Invoke implements tools.Tool.
func (t *AckAdapter) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	args := tools.NormalizeArgs("ack_directive", inv.Arguments, tools.DefaultSurfaceTuning())
	var a struct {
		Directive string `json:"directive"`
		Note      string `json:"note"`
	}
	_ = json.Unmarshal(args, &a)
	if t.Orch == nil {
		return tools.Result{Status: tools.StatusOK, Output: "acked (no orchestrator in scope): " + a.Note}, nil
	}
	pending := t.Orch.PendingDirectives(t.Root)
	if a.Directive == "" {
		if len(pending) == 0 {
			out := "no pending directives (single-run scope)"
			if a.Note != "" {
				out += "; noted: " + a.Note
			}
			return tools.Result{Status: tools.StatusOK, Output: out}, nil
		}
		acked := 0
		for _, msg := range pending {
			if err := t.Orch.AckDirective("", msg.ID, firstNonEmpty(a.Note, "ack all"), 0); err == nil {
				acked++
			}
		}
		return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("acked %d pending directive(s)", acked)}, nil
	}
	if err := t.Orch.AckDirective("", types.MessageID(a.Directive), firstNonEmpty(a.Note, "acked"), 0); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: fmt.Sprintf("ack %q: %v", a.Directive, err)}, nil
	}
	return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("acked %s: %s", a.Directive, a.Note)}, nil
}

// ResourceKeys implements tools.Tool.
func (t *AckAdapter) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// HistoryAdapter searches the run-local tool history.
type HistoryAdapter struct {
	Log *HistoryLog
}

// Definition implements tools.Tool.
func (t *HistoryAdapter) Definition() tools.Definition {
	return tools.Definition{
		Name: "history_search", Description: "Search this run's tool calls by text, tool name or state (embedded run-local history)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"tool":{"type":"string"},"k":{"type":"integer"}},"additionalProperties":true}`),
		Effect:      types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate,
		Timeout: 30_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "memory", Tags: []string{"history"}, Keywords: []string{"history", "search", "actions"},
	}
}

// Invoke implements tools.Tool.
func (t *HistoryAdapter) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	args := tools.NormalizeArgs("history_search", inv.Arguments, tools.DefaultSurfaceTuning())
	var a struct {
		Text  string `json:"text"`
		Query string `json:"query"`
		Tool  string `json:"tool"`
		K     int    `json:"k"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Text == "" {
		a.Text = a.Query
	}
	hits := t.Log.Search(a.Text, a.Tool, a.K)
	if len(hits) == 0 {
		return tools.Result{Status: tools.StatusOK, Output: "no history entries yet in this run"}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d entr(ies):\n", len(hits))
	for _, e := range hits {
		fmt.Fprintf(&b, "- [%s] %s args=%s out=%s\n", e.At.Format(time.RFC3339), e.Tool, trunc(e.Args, 200), trunc(e.Output, 200))
	}
	return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
}

// ResourceKeys implements tools.Tool.
func (t *HistoryAdapter) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// PinAdapter records pin_observation texts for the run.
type PinAdapter struct {
	Pins *PinStore
}

// Definition implements tools.Tool.
func (t *PinAdapter) Definition() tools.Definition {
	return tools.Definition{
		Name: "pin_observation", Description: "Mark an observation important so masking never hides it (embedded: kept for the run)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"observation":{"type":"string"}},"additionalProperties":true}`),
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemIdempotent, Cancel: types.CancelImmediate,
		Timeout: 30_000_000_000, OutputBudget: tools.DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "context", Tags: []string{"context"}, Keywords: []string{"pin", "observation", "mask"},
	}
}

// Invoke implements tools.Tool.
func (t *PinAdapter) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	args := tools.NormalizeArgs("pin_observation", inv.Arguments, tools.DefaultSurfaceTuning())
	var m map[string]any
	_ = json.Unmarshal(args, &m)
	text := firstNonEmpty(asStr(m["observation"]), asStr(m["id"]), asStr(m["text"]))
	if strings.TrimSpace(text) == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "observation is required: pin_observation {\"observation\": \"<text that must survive masking>\"}"}, nil
	}
	store := t.Pins
	if store == nil {
		store = NewPinStore()
	}
	n := store.Pin(text)
	return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("pinned observation %d (kept for the run)", n)}, nil
}

// ResourceKeys implements tools.Tool.
func (t *PinAdapter) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

// NoteAdapter records leave_continuation_note notes for the run.
type NoteAdapter struct {
	Notes *NoteStore
}

// Definition implements tools.Tool.
func (t *NoteAdapter) Definition() tools.Definition {
	return tools.Definition{
		Name: "leave_continuation_note", Description: "Save a brief next-step note for the next call (embedded: kept for the run)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"note":{"type":"string"}},"additionalProperties":true}`),
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemIdempotent, Cancel: types.CancelAfterCompletion,
		Timeout: 30_000_000_000, OutputBudget: tools.DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "conversation", Tags: []string{"note"}, Keywords: []string{"note", "continuation", "context"},
	}
}

// Invoke implements tools.Tool.
func (t *NoteAdapter) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	args := tools.NormalizeArgs("leave_continuation_note", inv.Arguments, tools.DefaultSurfaceTuning())
	var m map[string]any
	_ = json.Unmarshal(args, &m)
	note := firstNonEmpty(asStr(m["note"]), asStr(m["text"]))
	if strings.TrimSpace(note) == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "note is required: leave_continuation_note {\"note\": \"<next step>\"}"}, nil
	}
	store := t.Notes
	if store == nil {
		store = NewNoteStore()
	}
	n := store.Add(note)
	return tools.Result{Status: tools.StatusOK, Output: fmt.Sprintf("noted (%d in run); latest: %s", n, trunc(note, 200))}, nil
}

// ResourceKeys implements tools.Tool.
func (t *NoteAdapter) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// WireEmbeddedSurface fills the task-scoped stub providers from
// stores: task_db, subagent, ack_directive, history_search,
// pin_observation and leave_continuation_note. Missing registry
// entries are registered; the core prefix never changes because the
// definitions stay the stubs'. Skills are wired separately through
// WireSkills with a builtin default (see BuildEmbedded).
func WireEmbeddedSurface(reg *tools.MapRegistry, stores *SurfaceStores) {
	if stores == nil {
		stores = EnsureSurfaceStores("")
	}
	if stores.Tasks == nil {
		stores.Tasks = tasks.NewMemoryStore()
	}
	if stores.Orch == nil || stores.OrchStore == nil {
		built := EnsureSurfaceStores("")
		stores.Orch = built.Orch
		stores.OrchStore = built.OrchStore
		stores.Root = built.Root
	}
	if stores.History == nil {
		stores.History = NewHistoryLog()
	}
	if stores.Pins == nil {
		stores.Pins = NewPinStore()
	}
	if stores.Notes == nil {
		stores.Notes = NewNoteStore()
	}
	adapters := []tools.Tool{
		&TaskDBAdapter{Store: stores.Tasks, Scope: stores.Scope},
		&SubagentAdapter{Inner: &orchestrator.SubagentTool{Orch: stores.Orch}, Orch: stores.Orch, OrchStore: stores.OrchStore, Root: stores.Root},
		&AckAdapter{Orch: stores.Orch, Root: stores.Root},
		&HistoryAdapter{Log: stores.History},
		&PinAdapter{Pins: stores.Pins},
		&NoteAdapter{Notes: stores.Notes},
	}
	for _, real := range adapters {
		existing, err := reg.Get(real.Definition().Name)
		if err != nil {
			_ = reg.Register(real)
			continue
		}
		if stub, ok := existing.(*tools.StubTool); ok {
			provider := real
			stub.Provider = func(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
				return provider.Invoke(ctx, inv)
			}
		}
	}
}

// DefaultSkillsIndex returns the embedded builtin skill index used
// when EmbeddedOptions.Skills is nil: eval and exec runs always have
// skills available instead of "not wired" errors.
func DefaultSkillsIndex() *skills.Index {
	ix := skills.NewBuiltinIndex()
	_ = ix.Scan()
	return ix
}

// SurfaceToolNames lists the stub providers WireEmbeddedSurface fills.
func SurfaceToolNames() []string {
	return []string{"task_db", "subagent", "ack_directive", "history_search", "pin_observation", "leave_continuation_note"}
}

// SortedKeys renders a deterministic key list for tests.
func SortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
