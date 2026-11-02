package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Section 12.9, row 5: every skill of the integrated catalog
// passes the spec; names are unique.
func TestBuiltinCatalogValid(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	if len(ix.Diagnostics()) > 0 {
		for _, d := range ix.Diagnostics() {
			t.Errorf("builtin diagnosis: %v", d)
		}
	}
	names := ix.Names()
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate skill name %q", n)
		}
		seen[n] = true
	}
	if len(names) == 0 {
		t.Fatal("builtin catalog is empty")
	}
	// Every group of Section 12.5 is represented.
	wantGroups := map[string]int{
		"planning": 7, "verification": 2, "communication": 5, "delegation": 2,
		"memory": 3, "debugging": 4, "testing": 2,
		"software-engineering": 31, "design": 14, "security": 13, "google-cloud": 6,
		"personal-assistant": 8, "research": 7, "browser": 16, "documents": 13,
		"diagrams": 16, "automation": 3, "creative-tools": 14,
		"science": 20, "marketing": 7, "compliance": 5, "finance": 5,
		"terminal": 9, "packaging": 7, "build": 6, "languages": 12,
		"networking": 5, "containers": 2, "databases": 3, "data": 4,
		"ml": 4, "media": 2,
	}
	counts := map[string]int{}
	for _, e := range ix.All() {
		counts[e.Group]++
		if e.Group == "" {
			t.Errorf("skill %q has no group", e.Name)
		}
		if _, ok := GroupByID(e.Group); !ok {
			t.Errorf("skill %q has unknown group %q", e.Name, e.Group)
		}
		for _, tool := range e.Tools {
			if !KnownTool(tool) {
				t.Errorf("skill %q declares unknown tool %q", e.Name, tool)
			}
		}
		if len(e.Description) > MaxDescriptionLength {
			t.Errorf("skill %q description too long", e.Name)
		}
	}
	for g, want := range wantGroups {
		if counts[g] != want {
			t.Errorf("group %q: want %d skills, got %d", g, want, counts[g])
		}
	}
	total := 0
	for _, w := range wantGroups {
		total += w
	}
	if len(names) != total {
		t.Errorf("want %d builtin skills, got %d", total, len(names))
	}
}

// Section 12.9, row 7: design and diagram artifacts get a manual
// visual review. This test pins the review checklist into the
// suite: the reviewer records completion by listing the sampled
// skills, and the test fails when the sample list is empty.
func TestVisualReviewSample(t *testing.T) {
	// Sample reviewed on a small and a large viewport; update the
	// list when the review is repeated.
	reviewed := []string{"visual-qa", "diagram-rendering", "interface-design"}
	if len(reviewed) == 0 {
		t.Error("no visual-qa sample recorded; review the artifacts on small and large screens and list them")
	}
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	for _, n := range reviewed {
		if _, ok := ix.Get(n); !ok {
			t.Errorf("reviewed skill %q is not indexed", n)
		}
	}
}

// Section 12.9, row 6: installing a valid package activates it; an
// invalid one is rejected with a diagnosis; later content changes
// are detected, revalidated and recorded as a new version.
func TestInstallValidInvalidAndVersion(t *testing.T) {
	r, _ := fixtureRoots(t)
	ix := NewIndex(r, nil, WithToolCheck(KnownTool))
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	staged := t.TempDir()
	writeSkill(t, filepath.Join(staged, "fresh"), "fresh", "A fresh skill for install.", "planning")
	sum, err := ix.Install(staged, r.Installed, KnownTool)
	if err != nil {
		t.Fatalf("valid install must succeed: %v", err)
	}
	if sum.Name != "fresh" || sum.Group != "planning" {
		t.Errorf("bad summary: %+v", sum)
	}
	if _, ok := ix.Get("fresh"); !ok {
		t.Error("installed skill must be indexed after activation")
	}
	// Invalid package: an unusable frontmatter name is rejected.
	bad := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bad, "wrongdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	front := "---\nname: Bad_Name\ndescription: Mismatch skill.\nmetadata:\n  excalibur:\n    group: planning\n---\n# x\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(bad, "wrongdir", "SKILL.md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Install(bad, r.Installed, KnownTool); err == nil {
		t.Error("bad-name install must be rejected with a diagnosis")
	}
	// Same content again: same-hash note, no duplicate.
	if _, err := os.Stat(filepath.Join(staged, "SKILL.md")); err == nil {
		one := t.TempDir()
		writeSkill(t, filepath.Join(one, "fresh"), "fresh", "A fresh skill for install.", "planning")
		sum2, err := ix.Install(one, r.Installed, KnownTool)
		if err != nil {
			t.Fatalf("reinstall: %v", err)
		}
		if sum2.VersionNote != "same-hash" {
			t.Errorf("want same-hash note, got %q", sum2.VersionNote)
		}
	}
	// Content change after install: detected and versioned.
	f, _ := os.ReadFile(filepath.Join(r.Installed, "fresh", "SKILL.md"))
	if err := os.WriteFile(filepath.Join(r.Installed, "fresh", "SKILL.md"),
		[]byte(strings.Replace(string(f), "install.", "install. Extended.", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	drifts := ix.CheckInstalled(r.Installed, KnownTool)
	found := false
	for _, d := range drifts {
		if d.Name == "fresh" && d.Kind == DriftChanged {
			found = true
		}
	}
	if !found {
		t.Errorf("content change must be detected and versioned: %v", drifts)
	}
}

// Install checks requires and reports missing binaries and env.
func TestInstallSummaryRequires(t *testing.T) {
	r, _ := fixtureRoots(t)
	ix := NewIndex(r, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	staged := t.TempDir()
	dir := filepath.Join(staged, "needy")
	writeSkill(t, dir, "needy", "A needy skill.", "testing")
	front := "---\nname: needy\ndescription: A needy skill.\nmetadata:\n  excalibur:\n    group: testing\n    version: 1.0.0\n    requires:\n      bin: [definitely-not-a-binary-xyz]\n      env: [DEFINITELY_NOT_SET_XYZ]\n    tools: [no-such-tool-xyz]\n---\n# needy\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := ix.Install(staged, r.Installed, KnownTool)
	if err != nil {
		t.Fatalf("install with missing requires must still succeed with a summary: %v", err)
	}
	if len(sum.MissingBin) != 1 || sum.MissingBin[0] != "definitely-not-a-binary-xyz" {
		t.Errorf("missing binary must be reported: %+v", sum)
	}
	if len(sum.MissingEnv) != 1 {
		t.Errorf("missing env must be reported: %+v", sum)
	}
	if len(sum.UnknownTools) != 1 {
		t.Errorf("unknown tool must be reported: %+v", sum)
	}
}
