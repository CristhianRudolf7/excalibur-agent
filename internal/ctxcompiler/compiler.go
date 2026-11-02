package ctxcompiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Layer is one context layer of Section 11.1. L0-L2 are the stable
// prefix; L3 changes on task events via deltas appended to L4 and is
// only re-rendered at compaction; L4 is append-only history; L5 is
// volatile per-call state.
type Layer int

// Context layers in wire order.
const (
	LayerCore     Layer = 0
	LayerIdentity Layer = 1
	LayerScope    Layer = 2
	LayerTask     Layer = 3
	LayerActivity Layer = 4
	LayerVolatile Layer = 5
)

// NumLayers is the layer count.
const NumLayers = 6

// Name returns the short layer name used in diagnostics.
func (l Layer) Name() string {
	switch l {
	case LayerCore:
		return "L0"
	case LayerIdentity:
		return "L1"
	case LayerScope:
		return "L2"
	case LayerTask:
		return "L3"
	case LayerActivity:
		return "L4"
	case LayerVolatile:
		return "L5"
	default:
		return "L?"
	}
}

// HashLayer returns the hex SHA-256 of one rendered layer.
func HashLayer(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Preference is one user preference rendered into L2: the ones fixed
// as always pertinent first, then the recalled ones, each group in
// key order.
type Preference struct {
	Key    string
	Value  string
	Always bool
}

// InstructionView is one active durable instruction rendered
// literally into L2 (project/general scopes) or L3
// (task/conversation scopes). See instructions.go for the registry.
type InstructionView struct {
	Scope string
	ID    string
	Text  string
}

// RequirementView is the compact L3 projection of a requirement:
// code, text and state only, no history (11.1).
type RequirementView struct {
	Code  string
	Text  string
	State string
}

// CriterionView is the compact L3 projection of a criterion: state
// only, never the full evidence output (11.1).
type CriterionView struct {
	Code      string
	Statement string
	State     string
}

// PlanItemView is one micro-plan step with its state.
type PlanItemView struct {
	Code  string
	Title string
	State string
}

// SkillView is one suggested skill with its one-line reason (at most
// Config.SuggestedSkillsMax enter L3).
type SkillView struct {
	Name string
	Why  string
}

// TaskView is the L3 input: the literal original request, the fixed
// instructions, compact requirements/criteria, the micro plan, the
// pinned working notes, retrieved memories, suggested skills and the
// delegation card.
type TaskView struct {
	OriginalRequest   string
	FixedInstructions []string
	Requirements      []RequirementView
	Criteria          []CriterionView
	Plan              []PlanItemView
	// WorkingNotes are pinned key facts (paths, decisions,
	// failures) kept verbatim in L3 so small models do not lose
	// them as L4 grows. Rendered most-recent-last, capped by
	// Config.WorkingNotesMax.
	WorkingNotes    []string
	Memories        []MemoryHit
	SuggestedSkills []SkillView
	Delegation      string
}

// ActivityEvent is one append-only L4 entry: a message, a tool call
// or a tool result. Entries are rendered independently and never
// extended in place, so appending history only appends bytes (11.1
// rules 3-4). Masked observations arrive with their marker already
// in Content; see masking.go.
type ActivityEvent struct {
	// Dependencies is an engine evidence receipt, never projected to the wire.
	Dependencies map[string]string `json:"dependencies,omitempty"`
	// Seq orders the entry; it is history state, not a per-request
	// counter, so it may appear before the last stable mark.
	Seq int64
	// Role is the message role.
	Role providers.Role
	// Content is the literal body.
	Content string
	// ToolCalls carries assistant tool invocations.
	ToolCalls []providers.ToolCall
	// ToolResult carries a tool outcome for RoleTool entries.
	ToolResult *providers.ToolResult
}

// Volatile is the L5 input: everything that may change every call.
// The clock reading lives here and nowhere else (11.1 rule 1).
type Volatile struct {
	// Now is the call time; rendered in UTC RFC 3339.
	Now time.Time
	// ContinuationNote resumes work after a restart or compaction.
	ContinuationNote string
	// BudgetState is the one-line budget snapshot.
	BudgetState string
	// Directives are pending directives.
	Directives []string
	// GateReports are completion-gate reports.
	GateReports []string
	// RecoveryNotices are crash/interruption notices.
	RecoveryNotices []string
	// MasterJobs is the brief work table of the master (6.2).
	MasterJobs []string
}

// Snapshot is the full compiler input: database state plus run
// history projected for one call. Compiling the same Snapshot
// yields exactly the same bytes.
type Snapshot struct {
	// HarnessVersion pins L0 to the harness release.
	HarnessVersion string
	// Role is the caller role (master, child, parent, ...).
	Role string
	// CoreTools are the role core tools, serialized name-ordered.
	CoreTools []CoreTool
	// AgentProfile is the agent (or chosen persona) core.
	AgentProfile string
	// ModelID is the resolved model id for family adaptation
	// (agent quality): it only selects the L0 prompt variant, it is
	// never rendered, so same-family models share the L0 prefix.
	ModelID string
	// SkillCatalog is the pre-grouped L1 catalog text owned by the
	// skill index (12.3); the compiler renders it verbatim.
	SkillCatalog string
	// Preferences holds project (L2) preferences.
	Preferences []Preference
	// ProjectContext is the stable project context: description,
	// conventions, project instructions, build/test commands.
	ProjectContext string
	// Instructions are the active durable instructions for the
	// call scopes, rendered literally.
	Instructions []InstructionView
	// Task is the L3 input.
	Task TaskView
	// CompactionBlock, when non-empty, is the latest compaction
	// block re-rendered with L3 (11.4 step 4).
	CompactionBlock string
	// Activity is the append-only history.
	Activity []ActivityEvent
	// Volatile is the per-call state.
	Volatile Volatile
}

// Compiled is one deterministic compilation: the six rendered
// layers, their hashes and their token costs.
type Compiled struct {
	Layers      [NumLayers]string
	Hashes      [NumLayers]string
	LayerTokens [NumLayers]int
	TotalTokens int
}

// LayerText returns the rendered text of one layer.
func (c Compiled) LayerText(l Layer) string { return c.Layers[int(l)] }

// LayerHash returns the hex hash of one layer.
func (c Compiled) LayerHash(l Layer) string { return c.Hashes[int(l)] }

// Compile renders snap deterministically. The same Snapshot always
// produces the same bytes: lists are sorted, separators are fixed,
// and the clock reading only enters L5.
func Compile(cfg Config, snap Snapshot, counter Counter) (Compiled, error) {
	if err := cfg.Validate(); err != nil {
		return Compiled{}, err
	}
	if counter == nil {
		counter = NewEstimator(0)
	}
	var out Compiled
	out.Layers[LayerCore] = renderCore(cfg, snap)
	out.Layers[LayerIdentity] = renderIdentity(snap)
	out.Layers[LayerScope] = renderScope(snap)
	out.Layers[LayerTask] = renderTask(cfg, snap)
	out.Layers[LayerActivity] = renderActivity(snap)
	out.Layers[LayerVolatile] = renderVolatile(snap)
	for i := 0; i < NumLayers; i++ {
		out.Hashes[i] = HashLayer(out.Layers[i])
		out.LayerTokens[i] = counter.Count(out.Layers[i])
		out.TotalTokens += out.LayerTokens[i]
	}
	return out, nil
}

func renderCore(cfg Config, snap Snapshot) string {
	tools := append([]CoreTool(nil), snap.CoreTools...)
	sort.SliceStable(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	family := DetectFamily(snap.ModelID)
	if cfg.FamilyAdapt {
		for i := range tools {
			tools[i].Description = AdaptToolDescription(family, tools[i].Name, tools[i].Description)
		}
	}
	var b strings.Builder
	b.WriteString("role: " + snap.Role + "\n")
	b.WriteString("harness: " + snap.HarnessVersion + "\n")
	b.WriteString("tools:\n")
	for _, t := range tools {
		b.WriteString("- " + t.Name + ": " + t.Description + "\n")
	}
	// The working method rides the stable L0 prefix so every call
	// of the run carries it without churning the cache (agent
	// quality; Section 19 roles for the family variant).
	if cfg.MethodPrompt {
		b.WriteString("method:\n")
		b.WriteString(MethodBlock(DetectFamily(snap.ModelID), cfg.FamilyAdapt))
	}
	return b.String()
}

func renderIdentity(snap Snapshot) string {
	var b strings.Builder
	b.WriteString("profile: " + snap.AgentProfile + "\n")
	b.WriteString("skills:\n")
	b.WriteString(snap.SkillCatalog)
	if !strings.HasSuffix(snap.SkillCatalog, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func renderScope(snap Snapshot) string {
	always := make([]Preference, 0, len(snap.Preferences))
	recalled := make([]Preference, 0, len(snap.Preferences))
	for _, p := range snap.Preferences {
		if p.Always {
			always = append(always, p)
		} else {
			recalled = append(recalled, p)
		}
	}
	byKey := func(p []Preference) {
		sort.SliceStable(p, func(i, j int) bool { return p[i].Key < p[j].Key })
	}
	byKey(always)
	byKey(recalled)
	var b strings.Builder
	b.WriteString("preferences:\n")
	for _, p := range always {
		b.WriteString("- " + p.Key + "=" + p.Value + "\n")
	}
	for _, p := range recalled {
		b.WriteString("- " + p.Key + "=" + p.Value + " (recalled)\n")
	}
	b.WriteString("project:\n")
	b.WriteString(snap.ProjectContext)
	if !strings.HasSuffix(snap.ProjectContext, "\n") {
		b.WriteString("\n")
	}
	ins := append([]InstructionView(nil), snap.Instructions...)
	sort.SliceStable(ins, func(i, j int) bool {
		if ins[i].Scope != ins[j].Scope {
			return ins[i].Scope < ins[j].Scope
		}
		return ins[i].ID < ins[j].ID
	})
	b.WriteString("instructions:\n")
	for _, in := range ins {
		b.WriteString("[" + in.Scope + "/" + in.ID + "] " + in.Text + "\n")
	}
	return b.String()
}

func renderTask(cfg Config, snap Snapshot) string {
	t := snap.Task
	var b strings.Builder
	b.WriteString("request:\n")
	b.WriteString(t.OriginalRequest)
	if !strings.HasSuffix(t.OriginalRequest, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("fixed instructions:\n")
	for _, f := range t.FixedInstructions {
		b.WriteString("- " + f + "\n")
	}
	reqs := append([]RequirementView(nil), t.Requirements...)
	sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].Code < reqs[j].Code })
	b.WriteString("requirements:\n")
	for _, r := range reqs {
		b.WriteString("- " + r.Code + " [" + r.State + "] " + r.Text + "\n")
	}
	crits := append([]CriterionView(nil), t.Criteria...)
	sort.SliceStable(crits, func(i, j int) bool { return crits[i].Code < crits[j].Code })
	b.WriteString("criteria:\n")
	for _, c := range crits {
		b.WriteString("- " + c.Code + " [" + c.State + "] " + c.Statement + "\n")
	}
	plan := append([]PlanItemView(nil), t.Plan...)
	sort.SliceStable(plan, func(i, j int) bool { return plan[i].Code < plan[j].Code })
	b.WriteString("plan:\n")
	for _, p := range plan {
		b.WriteString("- " + p.Code + " [" + p.State + "] " + p.Title + "\n")
	}
	// Pinned working notes: key facts the engine keeps verbatim so
	// small models do not lose them as L4 grows. Most-recent-last,
	// capped; empty renders nothing so legacy bytes are unchanged.
	if notes := tailNotes(t.WorkingNotes, cfg.WorkingNotesMax); len(notes) > 0 {
		b.WriteString("working notes:\n")
		for _, n := range notes {
			b.WriteString("- " + n + "\n")
		}
	}
	b.WriteString("memories:\n")
	for _, m := range t.Memories {
		b.WriteString("- " + m.Text + "\n")
	}
	b.WriteString("suggested skills:\n")
	for i, s := range t.SuggestedSkills {
		if i >= cfg.SuggestedSkillsMax {
			break
		}
		b.WriteString("- " + s.Name + ": " + s.Why + "\n")
	}
	b.WriteString("delegation:\n")
	b.WriteString(t.Delegation)
	if !strings.HasSuffix(t.Delegation, "\n") {
		b.WriteString("\n")
	}
	if snap.CompactionBlock != "" {
		b.WriteString("compaction:\n")
		b.WriteString(snap.CompactionBlock)
		if !strings.HasSuffix(snap.CompactionBlock, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderActivity(snap Snapshot) string {
	return renderEvents(snap.Activity)
}

func renderEvents(input []ActivityEvent) string {
	events := append([]ActivityEvent(nil), input...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Seq != events[j].Seq {
			return events[i].Seq < events[j].Seq
		}
		return events[i].Content < events[j].Content
	})
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "[seq %d] %s: %s", e.Seq, string(e.Role), e.Content)
		if !strings.HasSuffix(e.Content, "\n") {
			b.WriteString("\n")
		}
		for _, tc := range e.ToolCalls {
			fmt.Fprintf(&b, "[seq %d] calls %s(%s) args %s\n", e.Seq, tc.Name, tc.ID, string(tc.Arguments))
		}
		if e.ToolResult != nil {
			fmt.Fprintf(&b, "[seq %d] tool %s/%s: %s", e.Seq, e.ToolResult.Name, e.ToolResult.CallID, e.ToolResult.Output)
			if !strings.HasSuffix(e.ToolResult.Output, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func renderVolatile(snap Snapshot) string {
	v := snap.Volatile
	now := v.Now.UTC()
	if now.IsZero() {
		now = time.Unix(0, 0).UTC()
	}
	var b strings.Builder
	b.WriteString("now: " + now.Format(time.RFC3339) + "\n")
	if v.ContinuationNote != "" {
		b.WriteString("continuation: " + v.ContinuationNote + "\n")
	}
	if v.BudgetState != "" {
		b.WriteString("budget: " + v.BudgetState + "\n")
	}
	writeList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString(title + ":\n")
		for _, it := range items {
			b.WriteString("- " + it + "\n")
		}
	}
	writeList("directives", v.Directives)
	writeList("gate", v.GateReports)
	writeList("recovery", v.RecoveryNotices)
	writeList("master jobs", v.MasterJobs)
	return b.String()
}

// tailNotes returns the working notes to render: at most max most
// recent entries in original order. Non-positive max disables.
func tailNotes(notes []string, max int) []string {
	if max <= 0 || len(notes) == 0 {
		return nil
	}
	if len(notes) <= max {
		return notes
	}
	return notes[len(notes)-max:]
}

// FormatDelta renders a context delta: task-level changes travel as
// an appended L4 update instead of an L3 rewrite, so the stable
// prefix keeps its bytes (11.1 rule 3). OpenCode and Codex apply the
// same versioned-delta trick.
func FormatDelta(revision int, notes ...string) string {
	return fmt.Sprintf("Context update v%d: %s", revision, strings.Join(notes, "; "))
}

// AppendDelta appends a context delta to the snapshot activity as a
// user entry. seq must extend the history order.
func AppendDelta(snap *Snapshot, seq int64, revision int, notes ...string) {
	snap.Activity = append(snap.Activity, ActivityEvent{
		Seq:     seq,
		Role:    providers.RoleUser,
		Content: FormatDelta(revision, notes...),
	})
}

// ProviderTools converts core tools to provider tool definitions with
// a permissive object schema. The engine replaces the schemas with
// the validated ones from the tool catalog; the compiler only needs
// the stable name-ordered list.
func ProviderTools(tools []CoreTool) []providers.ToolDefinition {
	cp := append([]CoreTool(nil), tools...)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	out := make([]providers.ToolDefinition, 0, len(cp))
	for _, t := range cp {
		out = append(out, providers.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Schema:      []byte(`{"type":"object"}`),
		})
	}
	return out
}

// ToChatRequest projects a compilation onto one provider call.
// Order is tools, then system (L0-L3), then messages, then the
// volatile tail, which is the exact-match prefix order every major
// provider caches on (P4, 11.6). Reasoning effort and tool choice
// travel fixed per role so they never break the prefix (11.6).
func (c Compiled) ToChatRequest(model string, tools []providers.ToolDefinition, maxTokens int, reasoningEffort, toolChoice string) providers.ChatRequest {
	msgs := make([]providers.Message, 0, 4+len(tools))
	msgs = append(msgs,
		providers.Message{Role: providers.RoleSystem, Content: c.Layers[LayerCore]},
		providers.Message{Role: providers.RoleSystem, Content: c.Layers[LayerIdentity]},
		providers.Message{Role: providers.RoleSystem, Content: c.Layers[LayerScope]},
		providers.Message{Role: providers.RoleSystem, Content: c.Layers[LayerTask]},
	)
	return providers.ChatRequest{
		Model:           model,
		Messages:        msgs,
		Tools:           tools,
		MaxTokens:       maxTokens,
		ReasoningEffort: reasoningEffort,
		ToolChoice:      toolChoice,
	}
}

// AppendActivityMessages appends the rendered activity and volatile
// layers to req.Messages. It is separate from ToChatRequest so
// callers that stream activity from the journal can project it
// without re-rendering.
func (c Compiled) AppendActivityMessages(req *providers.ChatRequest, activity []ActivityEvent) {
	events := append([]ActivityEvent(nil), activity...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Seq != events[j].Seq {
			return events[i].Seq < events[j].Seq
		}
		return events[i].Content < events[j].Content
	})
	for _, e := range events {
		m := providers.Message{Role: e.Role, Content: e.Content, ToolCalls: e.ToolCalls, ToolResult: e.ToolResult}
		req.Messages = append(req.Messages, m)
	}
	req.Messages = append(req.Messages, providers.Message{
		Role:    providers.RoleSystem,
		Content: c.Layers[LayerVolatile],
	})
}

// RoleTarget returns the design working range for a caller role
// (11.2): master 8-24K, child below 32K, parent below 48K. These are
// calibration goals, not hard limits.
func RoleTarget(role string, cfg Config) (int, int) {
	switch strings.ToLower(role) {
	case "master":
		return cfg.MasterTargetMin, cfg.MasterTargetMax
	case "parent":
		return 0, cfg.ParentTargetMax
	default:
		return 0, cfg.ChildTargetMax
	}
}

// BudgetReport compares a compilation against its role target.
type BudgetReport struct {
	Role         string
	Total        int
	Min          int
	Max          int
	WithinTarget bool
	PerLayer     [NumLayers]int
}

// CheckBudget reports whether compiled fits the role target.
func CheckBudget(c Compiled, role string, cfg Config) BudgetReport {
	min, max := RoleTarget(role, cfg)
	return BudgetReport{
		Role:         role,
		Total:        c.TotalTokens,
		Min:          min,
		Max:          max,
		WithinTarget: c.TotalTokens >= min && c.TotalTokens <= max,
		PerLayer:     c.LayerTokens,
	}
}

// CatalogBudget caps the L1 catalog share of the model window: at
// most 2% of the window or 4K tokens, whichever is smaller (12.3).
func CatalogBudget(windowTokens int, cfg Config) int {
	frac := int(cfg.SkillCatalogFraction * float64(windowTokens))
	if frac < cfg.SkillCatalogMax {
		return frac
	}
	return cfg.SkillCatalogMax
}
