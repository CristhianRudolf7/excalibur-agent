package ctxcompiler_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// fixedCounter counts 4 chars per token exactly so tests are exact.
type fixedCounter struct{ per int }

func (f fixedCounter) Count(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + f.per - 1) / f.per
}

func testSnapshot() ctxcompiler.Snapshot {
	return ctxcompiler.Snapshot{
		HarnessVersion: "h6-test",
		Role:           "child",
		CoreTools: []ctxcompiler.CoreTool{
			{Name: "read_file", Description: "read a file window"},
			{Name: "exec", Description: "run a command"},
		},
		AgentProfile:   "software-engineer",
		SkillCatalog:   "planning -- 2 skills; use search_skills\n",
		Preferences:    []ctxcompiler.Preference{{Key: "tone", Value: "brief", Always: true}},
		ProjectContext: "repo: demo\nbuild: go build ./...\n",
		Instructions: []ctxcompiler.InstructionView{
			{Scope: "general", ID: "ins_0001", Text: "Always use English."},
		},
		Task: ctxcompiler.TaskView{
			OriginalRequest:   "Fix the crash on empty input.",
			FixedInstructions: []string{"Do not touch unrelated files."},
			Requirements:      []ctxcompiler.RequirementView{{Code: "R9", Text: "No crash on empty input", State: "accepted"}},
			Criteria:          []ctxcompiler.CriterionView{{Code: "C9.1", Statement: "repro test passes", State: "unverified"}},
			Plan:              []ctxcompiler.PlanItemView{{Code: "P1", Title: "Reproduce", State: "done"}},
			Memories:          []ctxcompiler.MemoryHit{{Text: "crash is in parser.go:41"}},
			SuggestedSkills:   []ctxcompiler.SkillView{{Name: "root-cause-first", Why: "crash needs a cause"}},
			Delegation:        "solo run\n",
		},
		Activity: []ctxcompiler.ActivityEvent{
			{Seq: 1, Role: providers.RoleUser, Content: "go\n"},
			{Seq: 2, Role: providers.RoleAssistant, Content: "reading parser\n",
				ToolCalls: []providers.ToolCall{{ID: "tc_1", Name: "read_file", Arguments: []byte(`{"path":"parser.go"}`)}}},
			{Seq: 3, Role: providers.RoleTool, Content: "package parser\n",
				ToolResult: &providers.ToolResult{CallID: "tc_1", Name: "read_file", Output: "package parser\n"}},
		},
		Volatile: ctxcompiler.Volatile{
			Now:         time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
			BudgetState: "steps 3/60",
		},
	}
}

// 11.8 row 1: same state compiles to the same bytes, across a
// serialize/deserialize round trip standing in for a restart.
func TestCompileDeterministic(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	snap := testSnapshot()
	a, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	for l := ctxcompiler.LayerCore; l <= ctxcompiler.LayerVolatile; l++ {
		if a.LayerText(l) != b.LayerText(l) {
			t.Fatalf("layer %s differs between identical compilations", l.Name())
		}
		if a.LayerHash(l) != b.LayerHash(l) {
			t.Fatalf("layer %s hash differs between identical compilations", l.Name())
		}
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var restored ctxcompiler.Snapshot
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	c, err := ctxcompiler.Compile(cfg, restored, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	for l := ctxcompiler.LayerCore; l <= ctxcompiler.LayerVolatile; l++ {
		if a.LayerHash(l) != c.LayerHash(l) {
			t.Fatalf("layer %s hash changed after state round trip", l.Name())
		}
	}
}

// 11.1 rule 1: nothing variable before the last stable mark. Only L5
// may carry the clock.
func TestClockOnlyInVolatile(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	early := testSnapshot()
	late := testSnapshot()
	late.Volatile.Now = late.Volatile.Now.Add(90 * time.Minute)
	a, err := ctxcompiler.Compile(cfg, early, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ctxcompiler.Compile(cfg, late, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	for l := ctxcompiler.LayerCore; l <= ctxcompiler.LayerActivity; l++ {
		if a.LayerHash(l) != b.LayerHash(l) {
			t.Fatalf("layer %s carries the clock", l.Name())
		}
	}
	if a.LayerHash(ctxcompiler.LayerVolatile) == b.LayerHash(ctxcompiler.LayerVolatile) {
		t.Fatal("L5 must change when the clock advances")
	}
}

// 11.1 rule 2: tools always serialize in name order.
func TestToolsSerializeByName(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	snap := testSnapshot()
	snap.CoreTools = []ctxcompiler.CoreTool{
		{Name: "write_file", Description: "w"},
		{Name: "exec", Description: "e"},
		{Name: "read_file", Description: "r"},
	}
	c, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	l0 := c.LayerText(ctxcompiler.LayerCore)
	exec := strings.Index(l0, "- exec:")
	read := strings.Index(l0, "- read_file:")
	write := strings.Index(l0, "- write_file:")
	if exec < 0 || read < 0 || write < 0 || !(exec < read && read < write) {
		t.Fatalf("tools not name-ordered in L0:\n%s", l0)
	}
}

// 11.1 rule 4: messages append, never extend in place.
func TestActivityAppends(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	before := testSnapshot()
	a, err := ctxcompiler.Compile(cfg, before, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	after := testSnapshot()
	after.Activity = append(after.Activity, ctxcompiler.ActivityEvent{
		Seq: 4, Role: providers.RoleUser, Content: "status?\n",
	})
	b, err := ctxcompiler.Compile(cfg, after, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.LayerText(ctxcompiler.LayerActivity), a.LayerText(ctxcompiler.LayerActivity)) {
		t.Fatal("appending history must only append L4 bytes")
	}
}

// 11.8 row 2: task changes travel as deltas; L0-L2 keep their hash
// and the drift detector stays silent.
func TestDeltasKeepStablePrefix(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	detector := ctxcompiler.NewDriftDetector()
	snap := testSnapshot()
	first, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if drifts := detector.Observe("run_1", first.Hashes); len(drifts) != 0 {
		t.Fatalf("first observation must not drift: %v", drifts)
	}
	// Task events arrive as deltas and new history, never as L0-L2
	// rewrites.
	ctxcompiler.AppendDelta(&snap, 4, 7, "R10 added", "C9.1 satisfied")
	snap.Activity = append(snap.Activity, ctxcompiler.ActivityEvent{
		Seq: 5, Role: providers.RoleAssistant, Content: "noted\n",
	})
	second, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []ctxcompiler.Layer{ctxcompiler.LayerCore, ctxcompiler.LayerIdentity, ctxcompiler.LayerScope} {
		if first.LayerHash(l) != second.LayerHash(l) {
			t.Fatalf("layer %s changed on task delta", l.Name())
		}
	}
	if drifts := detector.Observe("run_1", second.Hashes); len(drifts) != 0 {
		t.Fatalf("delta must not trip drift detection: %v", drifts)
	}
	if !strings.Contains(second.LayerText(ctxcompiler.LayerActivity), "Context update v7: R10 added; C9.1 satisfied") {
		t.Fatal("delta missing from L4")
	}
}

// 11.1 pertinence: at most 3 suggested skills enter L3.
func TestSuggestedSkillsCapped(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	snap := testSnapshot()
	for i := 0; i < 5; i++ {
		snap.Task.SuggestedSkills = append(snap.Task.SuggestedSkills,
			ctxcompiler.SkillView{Name: "extra", Why: "x"})
	}
	c, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	l3 := c.LayerText(ctxcompiler.LayerTask)
	if got := strings.Count(l3, "- extra: x"); got != 2 {
		t.Fatalf("want first 3 skills (1 fixture + 2 extra), got %d extra lines", got)
	}
}

// Provider projection keeps tools, system, messages order with fixed
// per-role settings (11.6).
func TestToChatRequestShape(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	snap := testSnapshot()
	c, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	tools := ctxcompiler.ProviderTools(snap.CoreTools)
	req := c.ToChatRequest("model-x", tools, 4096, "low", "auto")
	if len(req.Tools) != 2 || req.Tools[0].Name != "exec" || req.Tools[1].Name != "read_file" {
		t.Fatalf("tools must travel name-ordered: %+v", req.Tools)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("want 4 system layers, got %d", len(req.Messages))
	}
	for _, m := range req.Messages {
		if m.Role != providers.RoleSystem {
			t.Fatalf("layer message must be system, got %s", m.Role)
		}
	}
	if req.ReasoningEffort != "low" || req.ToolChoice != "auto" {
		t.Fatal("per-role settings must travel fixed")
	}
	c.AppendActivityMessages(&req, snap.Activity)
	if len(req.Messages) != 4+len(snap.Activity)+1 {
		t.Fatalf("want layers + activity + volatile, got %d messages", len(req.Messages))
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != providers.RoleSystem || !strings.Contains(last.Content, "now: ") {
		t.Fatal("volatile tail must close the messages with the clock")
	}
}

// Budgets report against the 11.2 role targets.
func TestRoleBudgets(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	if min, max := ctxcompiler.RoleTarget("master", cfg); min != 8000 || max != 24000 {
		t.Fatalf("master target = %d-%d, want 8000-24000", min, max)
	}
	if _, max := ctxcompiler.RoleTarget("child", cfg); max != 32000 {
		t.Fatalf("child max = %d, want 32000", max)
	}
	if _, max := ctxcompiler.RoleTarget("parent", cfg); max != 48000 {
		t.Fatalf("parent max = %d, want 48000", max)
	}
	snap := testSnapshot()
	c, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	rep := ctxcompiler.CheckBudget(c, "child", cfg)
	if !rep.WithinTarget {
		t.Fatalf("tiny fixture must fit the child budget: %+v", rep)
	}
	if got := ctxcompiler.CatalogBudget(200000, cfg); got != 4000 {
		t.Fatalf("catalog budget = %d, want min(2%% of window, 4K) = 4000", got)
	}
	if got := ctxcompiler.CatalogBudget(100000, cfg); got != 2000 {
		t.Fatalf("catalog budget = %d, want 2000", got)
	}
}
