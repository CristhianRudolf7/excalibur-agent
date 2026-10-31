package skills

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

// Section 12.9, row 3: the L1 catalog with many groups respects
// the 2%-or-4K budget and non-pertinent groups appear as headers.
func TestCatalogBudgetAndHeaders(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	if n := len(ix.Names()); n == 0 {
		t.Fatal("builtin catalog must not be empty")
	}
	out := ix.RenderCatalog(CatalogConfig{
		WindowTokens: 200000, // 2% = 4000
		Pertinent:    DefaultPertinentForRole("programming"),
	})
	if got := EstimateTokens(out); got > CatalogBudget(200000) {
		t.Errorf("catalog %d tokens exceeds budget %d", got, CatalogBudget(200000))
	}
	for _, want := range []string{"- root-cause-first:", "[security]"} {
		if !strings.Contains(out, want) {
			t.Errorf("catalog must contain %q", want)
		}
	}
	// Headers-only groups carry no skill lines.
	lines := strings.Split(out, "\n")
	inSecurity := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "[security]") {
			inSecurity = true
			if !strings.Contains(ln, "search_skills") {
				t.Errorf("non-pertinent header must point at search_skills: %q", ln)
			}
			continue
		}
		if strings.HasPrefix(ln, "[") {
			inSecurity = false
			continue
		}
		if inSecurity && strings.HasPrefix(ln, "- ") {
			t.Errorf("non-pertinent group must list no skills, got %q", ln)
		}
	}
	// A tiny window still yields a bounded catalog.
	small := ix.RenderCatalog(CatalogConfig{WindowTokens: 1000, Pertinent: DefaultPertinentForRole("programming")})
	if got := EstimateTokens(small); got > CatalogBudget(1000) && EstimateTokens(small) > 0 {
		t.Errorf("small-window catalog %d tokens exceeds budget %d", got, CatalogBudget(1000))
	}
}

// Section 12.9, row 3 continued: pertinent groups list fully with
// short descriptions capped at 120 chars.
func TestCatalogShortDescriptions(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	out := ix.RenderCatalog(CatalogConfig{
		WindowTokens: 1000000, // budget capped at 4000
		Pertinent:    []string{"planning"},
	})
	if !strings.Contains(out, "[planning]") {
		t.Fatalf("planning must be fully listed: %q", out[:200])
	}
	for _, ln := range strings.Split(out, "\n") {
		if !strings.HasPrefix(ln, "- ") {
			continue
		}
		// "- name: desc" — the description part respects 120 chars.
		parts := strings.SplitN(ln[2:], ": ", 2)
		if len(parts) != 2 {
			t.Errorf("skill line must be '- name: desc': %q", ln)
			continue
		}
		if len(parts[1]) > MaxShortDescription+3 { // +ellipsis margin
			t.Errorf("description exceeds L1 budget: %q", ln)
		}
	}
}

// Section 12.9, row 4: at most 3 suggested skills.
func TestSuggestCapsAtThree(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	got := ix.Suggest("debug the failing test for the payment bug in production code", "model-x", 10)
	if len(got) > DefaultSuggestionsMax {
		t.Errorf("want at most %d suggestions, got %d", DefaultSuggestionsMax, len(got))
	}
	if len(got) == 0 {
		t.Error("a debugging query should suggest something")
	}
	for _, s := range got {
		if s.Why == "" || s.Name == "" {
			t.Errorf("suggestion needs name and reason: %+v", s)
		}
	}
}

// Section 12.9, row 4 continued: load_skill must not change the
// L0-L2 hashes: loading appends tool output to history (L4), never
// touches the stable prefix.
func TestLoadSkillKeepsStablePrefix(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	cfg := ctxcompiler.DefaultConfig()
	snap := ctxcompiler.Snapshot{
		HarnessVersion: "test",
		Role:           "child",
		AgentProfile:   "test profile",
		SkillCatalog:   ix.RenderCatalog(CatalogConfig{WindowTokens: 200000, Pertinent: DefaultPertinentForRole("programming")}),
		Task:           ctxcompiler.TaskView{OriginalRequest: "fix the bug"},
	}
	before, err := ctxcompiler.Compile(cfg, snap, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ix.Load("root-cause-first")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Body == "" {
		t.Fatal("loaded body must not be empty")
	}
	// Simulate the agent call: the skill body lands in L4 history.
	snap.Activity = append(snap.Activity, ctxcompiler.ActivityEvent{Seq: 1, Content: loaded.Body})
	after, err := ctxcompiler.Compile(cfg, snap, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []ctxcompiler.Layer{ctxcompiler.LayerCore, ctxcompiler.LayerIdentity, ctxcompiler.LayerScope} {
		if before.LayerHash(l) != after.LayerHash(l) {
			t.Errorf("load_skill changed stable %s hash: %s -> %s", l.Name(), before.LayerHash(l), after.LayerHash(l))
		}
	}
	if before.LayerHash(ctxcompiler.LayerActivity) == after.LayerHash(ctxcompiler.LayerActivity) {
		t.Error("loaded skill body must land in history (L4 hash should change)")
	}
}

// Section 12.9, row 4 continued: declared tools resolve with the
// skill (13.3) and search_skills filters by group.
func TestSearchAndDeclaredTools(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	hits := ix.Search("regression test isolates the faulty commit", "debugging")
	found := false
	for _, h := range hits {
		if h.Name == "fault-isolation" {
			found = true
			if h.Description == "" || h.Group != "debugging" || h.Path == "" {
				t.Errorf("hit must carry description, group and path: %+v", h)
			}
		}
	}
	if !found {
		t.Error("fault-isolation should match a regression/bisect query in debugging")
	}
	loaded, err := ix.Load("fault-isolation")
	if err != nil {
		t.Fatal(err)
	}
	has := func(list []string, want string) bool {
		for _, x := range list {
			if x == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"exec", "revert_edit"} {
		if !has(loaded.Tools, want) {
			t.Errorf("fault-isolation must declare tool %q, got %v", want, loaded.Tools)
		}
		if !KnownTool(want) {
			t.Errorf("declared tool %q must be a known harness tool", want)
		}
	}
}

// Section 12.4, last paragraph: negative per-model evidence
// disables a group's suggestions for that model.
func TestNegativeEvidenceDisablesGroup(t *testing.T) {
	r, _ := fixtureRoots(t)
	writeSkill(t, filepath.Join(r.User, "a-one"), "a-one", "First testing helper.", "testing")
	writeSkill(t, filepath.Join(r.User, "a-two"), "a-two", "Second testing helper.", "testing")
	ix := NewIndex(r, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	// Without-skill baseline succeeds; with-skill tasks fail.
	for i := 0; i < 6; i++ {
		ix.RecordOutcome("a-one", "cheap-model", false, true)
		ix.RecordOutcome("a-one", "cheap-model", true, false)
	}
	if !ix.SuggestDisabled("cheap-model", "testing") {
		t.Error("testing should be disabled for cheap-model after negative evidence")
	}
	if ix.SuggestDisabled("other-model", "testing") {
		t.Error("negative evidence must not leak across models")
	}
	if ix.SuggestDisabled("cheap-model", "planning") {
		t.Error("negative evidence must not leak across groups")
	}
}
