package wireengine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// TestDeferredReachableWithoutBreakingPrefix covers Section 13.3: the
// deferred harness families resolve through search_tools and run
// through call_tool, while the core prefix hash never moves no matter
// how many deferred tools register.
func TestDeferredReachableWithoutBreakingPrefix(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{RunID: "run_cat_1"})
	if err != nil {
		t.Fatal(err)
	}
	before := emb.Catalog.PrefixHash()
	ctx := context.Background()
	// search_tools finds the deferred code_graph tool by its domain.
	sq, _ := json.Marshal(map[string]string{"query": "impacted tests"})
	sres, err := emb.Exec.Execute(ctx, "search_tools", sq, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sres.Status != tools.StatusOK {
		t.Fatalf("search_tools: %+v", sres)
	}
	if !contains(sres.Output, "code_graph") {
		t.Fatalf("search_tools must surface code_graph, got %q", sres.Output)
	}
	// call_tool invokes a deferred tool by name with validated args.
	// file_history runs against the shared history without extra
	// collaborators; codeintel stats needs a graph opener.
	cq, _ := json.Marshal(map[string]any{
		"name":      "file_history",
		"arguments": map[string]any{"path": "any.txt"},
	})
	cres, err := emb.Exec.Execute(ctx, "call_tool", cq, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cres.Status != tools.StatusOK {
		t.Fatalf("call_tool must validate and run, got %+v", cres)
	}
	// The fixed prefix never moves: deferred additions resolve from
	// the next step without entering the cached prefix (11.6, 13.3).
	if got := emb.Catalog.PrefixHash(); got != before {
		t.Fatalf("prefix hash moved %s -> %s", before, got)
	}
	if emb.Catalog.IsCore("code_graph") || !emb.Catalog.IsDeferred("code_graph") {
		t.Fatal("code_graph must be deferred, never core")
	}
	for _, name := range []string{"lsp", "file_history", "revert_edit", "workspace_snapshot", "worktree", "integration_queue", "document_read", "source_search"} {
		if !emb.Catalog.IsDeferred(name) {
			t.Fatalf("%s must be deferred", name)
		}
	}
}

// TestSkillsToolsRegistered covers Section 12.4: search_skills and
// load_skill are registered in the catalog with a real index, and
// load_skill resolves the skill's deferred tools up front.
func TestSkillsToolsRegistered(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "project")
	skillDir := filepath.Join(proj, "skill-test")
	front := "---\nname: skill-test\ndescription: Fix JSON files quickly\nmetadata:\n  excalibur:\n    group: planning\n    version: 1.0.0\n---\n# Skill\n\nFix it.\n"
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := skills.NewIndex(skills.Roots{Project: proj}, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	emb, err := BuildEmbedded(EmbeddedOptions{RunID: "run_skill_1", Skills: ix})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sq, _ := json.Marshal(map[string]string{"query": "JSON"})
	sres, err := emb.Exec.Execute(ctx, "search_skills", sq, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sres.Status != tools.StatusOK || !contains(sres.Output, "skill-test") {
		t.Fatalf("search_skills must find the skill, got %+v", sres)
	}
	lq, _ := json.Marshal(map[string]string{"name": "skill-test"})
	lres, err := emb.Exec.Execute(ctx, "load_skill", lq, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lres.Status != tools.StatusOK || !contains(lres.Output, "Fix it.") {
		t.Fatalf("load_skill must return the body, got %+v", lres)
	}
}
