package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkill writes a minimal valid skill dir for fixture tests.
func writeSkill(t *testing.T, dir, name, desc, group string) {
	t.Helper()
	front := "---\nname: " + name + "\ndescription: " + desc + "\nmetadata:\n  excalibur:\n    group: " + group + "\n    version: 1.0.0\n---\n# " + name + "\n\nBody text.\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureRoots(t *testing.T) (Roots, func()) {
	t.Helper()
	base := t.TempDir()
	r := Roots{
		Project:   filepath.Join(base, "project", ".agents", "skills"),
		User:      filepath.Join(base, "user"),
		Installed: filepath.Join(base, "installed"),
	}
	for _, d := range []string{r.Project, r.User, r.Installed} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r, func() {}
}

// Section 12.9, row 1: invalid skills are excluded with a visible
// diagnosis: bad name, overlong description, compatibility over
// 500 chars, directory/name mismatch.
func TestValidationRejectsBadSkills(t *testing.T) {
	cases := []struct {
		name  string
		front string
		code  string
	}{
		{"Bad_Name", "---\nname: Bad_Name\ndescription: Does things.\nmetadata:\n  excalibur:\n    group: planning\n---\n# x\n\nBody.\n", CodeBadName},
		{"trailing-", "---\nname: trailing-\ndescription: Does things.\nmetadata:\n  excalibur:\n    group: planning\n---\n# x\n\nBody.\n", CodeBadName},
		{"double--dash", "---\nname: double--dash\ndescription: Does things.\nmetadata:\n  excalibur:\n    group: planning\n---\n# x\n\nBody.\n", CodeBadName},
		{"toolongdesc", "---\nname: toolongdesc\ndescription: " + strings.Repeat("x", MaxDescriptionLength+1) + "\nmetadata:\n  excalibur:\n    group: planning\n---\n# x\n\nBody.\n", CodeBadDescription},
		{"badcompat", "---\nname: badcompat\ndescription: Does things.\ncompatibility: " + strings.Repeat("y", MaxCompatLength+1) + "\nmetadata:\n  excalibur:\n    group: planning\n---\n# x\n\nBody.\n", CodeBadCompat},
		{"mismatch", "---\nname: other-name\ndescription: Does things.\nmetadata:\n  excalibur:\n    group: planning\n---\n# x\n\nBody.\n", CodeNameMismatch},
		{"badgroup", "---\nname: badgroup\ndescription: Does things.\nmetadata:\n  excalibur:\n    group: nope\n---\n# x\n\nBody.\n", CodeBadGroup},
	}
	for _, c := range cases {
		sf, err := ParseSkillFile([]byte(c.front))
		if err != nil {
			t.Fatalf("%s: unexpected parse error: %v", c.name, err)
		}
		diags := Validate(c.name, "/tmp/"+c.name, sf, groupOK, nil)
		found := false
		for _, d := range diags {
			if d.Code == c.code {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want diagnosis %q, got %v", c.name, c.code, diags)
		}
	}
}

// Missing SKILL.md and unclosed fences are visible diagnoses too.
func TestMissingFileAndFence(t *testing.T) {
	r, _ := fixtureRoots(t)
	if err := os.MkdirAll(filepath.Join(r.User, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.User, "unclosed", "SKILL.md"), []byte("---\nname: unclosed\n"), 0o644); err != nil {
		_ = os.MkdirAll(filepath.Join(r.User, "unclosed"), 0o755)
		if err := os.WriteFile(filepath.Join(r.User, "unclosed", "SKILL.md"), []byte("---\nname: unclosed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix := NewIndex(r, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Get("empty"); ok {
		t.Error("empty dir must not be indexed")
	}
	if _, ok := ix.Get("unclosed"); ok {
		t.Error("unclosed fence must not be indexed")
	}
	codes := map[string]bool{}
	for _, d := range ix.Diagnostics() {
		codes[d.Code] = true
	}
	if !codes[CodeMissingFile] || !codes[CodeBadFrontmatter] {
		t.Errorf("want missing_file and bad_frontmatter diagnoses, got %v", ix.Diagnostics())
	}
}

// Section 12.9, row 2: the same skill in project and user roots:
// project wins and the conflict is recorded.
func TestPrecedenceAndConflict(t *testing.T) {
	r, _ := fixtureRoots(t)
	writeSkill(t, filepath.Join(r.Project, "dup"), "dup", "Project copy.", "planning")
	writeSkill(t, filepath.Join(r.User, "dup"), "dup", "User copy.", "debugging")
	ix := NewIndex(r, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	got, ok := ix.Get("dup")
	if !ok {
		t.Fatal("dup must be indexed")
	}
	if got.Root != RootProject {
		t.Errorf("want project root to win, got %q", got.Root)
	}
	if got.Group != "planning" {
		t.Errorf("want project copy, got group %q", got.Group)
	}
	confs := ix.Conflicts()
	if len(confs) != 1 || confs[0].Name != "dup" || confs[0].LoserRoot != RootUser {
		t.Errorf("want one recorded user conflict, got %v", confs)
	}
}

// Section 12.9, row 2 continued: modifying SKILL.md with a session
// open updates the index and the frozen session sees a delta.
func TestRescanUpdatesAndSessionDelta(t *testing.T) {
	r, _ := fixtureRoots(t)
	writeSkill(t, filepath.Join(r.User, "alpha"), "alpha", "First version.", "planning")
	ix := NewIndex(r, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	snap := ix.Freeze(nil)
	before, _ := ix.Get("alpha")
	writeSkill(t, filepath.Join(r.User, "alpha"), "alpha", "Second version with more text.", "planning")
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	after, _ := ix.Get("alpha")
	if before.Hash == after.Hash {
		t.Error("hash must change after SKILL.md modification")
	}
	delta := snap.Delta(ix)
	if len(delta) != 1 || delta[0].Kind != ChangeUpdated || delta[0].Name != "alpha" {
		t.Errorf("want one updated delta for alpha, got %v", delta)
	}
	// A fresh snapshot sees no drift.
	if d := ix.Freeze(nil).Delta(ix); len(d) != 0 {
		t.Errorf("fresh snapshot must be clean, got %v", d)
	}
}

// Section 12.9, row 5 (install part): same-name skill in two roots
// after install keeps precedence; removal reports drift.
func TestCheckInstalledDrift(t *testing.T) {
	r, _ := fixtureRoots(t)
	writeSkill(t, filepath.Join(r.Installed, "pack"), "pack", "Packed skill.", "testing")
	ix := NewIndex(r, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	staged := t.TempDir()
	writeSkill(t, filepath.Join(staged, "pack"), "pack", "Packed skill.", "testing")
	sum, err := ix.Install(staged, r.Installed, KnownTool)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if sum.VersionNote != "new" {
		t.Errorf("want new install note, got %q", sum.VersionNote)
	}
	// Tamper with the installed content: drift must be detected.
	f, _ := os.ReadFile(filepath.Join(r.Installed, "pack", "SKILL.md"))
	if err := os.WriteFile(filepath.Join(r.Installed, "pack", "SKILL.md"), append(f, []byte("\nExtra.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	drifts := ix.CheckInstalled(r.Installed, KnownTool)
	found := false
	for _, d := range drifts {
		if d.Name == "pack" && d.Kind == DriftChanged {
			found = true
		}
	}
	if !found {
		t.Errorf("want a changed drift for pack, got %v", drifts)
	}
}
