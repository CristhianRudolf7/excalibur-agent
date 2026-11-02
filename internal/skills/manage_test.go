package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCatalogManagement(t *testing.T) {
	roots := Roots{User: filepath.Join(t.TempDir(), "user"), Installed: filepath.Join(t.TempDir(), "installed"), Project: filepath.Join(t.TempDir(), "project")}
	ix := NewIndex(roots, managementBuiltins(t, "task-breakdown"))
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	frozen := NewEvalBuiltinIndex()
	if err := frozen.Scan(); err != nil {
		t.Fatal(err)
	}
	hash := frozen.CatalogHash()
	name := "task-breakdown"
	raw, err := ix.Content(name)
	if err != nil || !strings.HasPrefix(raw, "---\n") {
		t.Fatalf("content: %s %v", raw, err)
	}
	if err := ix.Delete(name); err != ErrBuiltinDelete {
		t.Fatalf("delete builtin: %v", err)
	}
	if err := ix.SetState(name, StateArchived); err != nil {
		t.Fatal(err)
	}
	edited := raw + "\nAn operator addition.\n"
	e, err := ix.Update(name, edited)
	if err != nil {
		t.Fatal(err)
	}
	if e.Root != RootUser || e.State != StateArchived {
		t.Fatalf("override: %+v", e)
	}
	back, err := os.ReadFile(filepath.Join(roots.User, name, "SKILL.md"))
	if err != nil || string(back) != edited {
		t.Fatalf("override disk: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(roots.User, name, "SKILL.md.backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v %v", backups, err)
	}
	old, _ := os.ReadFile(backups[0])
	if string(old) != raw {
		t.Fatal("backup lost original")
	}
	e, err = ix.Update(name, edited+"\nSecond edit.\n")
	if err != nil || e.Root != RootUser {
		t.Fatalf("user update: %+v %v", e, err)
	}
	beforeInvalid, _ := ix.Content(name)
	_, err = ix.Update(name, "not a skill")
	if v, ok := err.(*ValidationError); !ok || len(v.Diagnostics) == 0 {
		t.Fatalf("invalid: %v", err)
	}
	afterInvalid, _ := ix.Content(name)
	if beforeInvalid != afterInvalid {
		t.Fatal("invalid update changed disk")
	}
	_, err = ix.Update(name, strings.Replace(edited, "name: task-breakdown", "name: other-name", 1))
	if v, ok := err.(*ValidationError); !ok || v.Diagnostics[0].Code != CodeNameMismatch {
		t.Fatalf("name mismatch: %v", err)
	}
	if err := ix.Delete(name); err != nil {
		t.Fatal(err)
	}
	e, ok := ix.Get(name)
	if !ok || e.Root != RootBuiltin || e.State != StateActive {
		t.Fatalf("restore: %+v", e)
	}
	backups, _ = filepath.Glob(filepath.Join(roots.User, ".backups", name+"-*", name, "SKILL.md"))
	if len(backups) != 1 {
		t.Fatal("delete backup missing")
	}
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	for _, d := range ix.Diagnostics() {
		if strings.Contains(d.Path, ".backups") {
			t.Fatal("backups scanned")
		}
	}
	if err := frozen.Scan(); err != nil {
		t.Fatal(err)
	}
	if frozen.CatalogHash() != hash {
		t.Fatal("eval catalog changed")
	}
}

func TestWritableCatalogSources(t *testing.T) {
	for _, source := range []string{RootUser, RootInstalled, RootProject} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			roots := Roots{}
			switch source {
			case RootUser:
				roots.User = root
			case RootInstalled:
				roots.Installed = root
			case RootProject:
				roots.Project = root
			}
			ix := NewIndex(roots, managementBuiltins(t, "task-breakdown"))
			if err := ix.Scan(); err != nil {
				t.Fatal(err)
			}
			raw, err := ix.Content("task-breakdown")
			if err != nil {
				t.Fatal(err)
			}
			raw = strings.Replace(raw, "task-breakdown", "custom-skill", 1)
			dir := filepath.Join(root, "custom-skill")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := ix.Scan(); err != nil {
				t.Fatal(err)
			}
			e, err := ix.Update("custom-skill", raw+"\nUpdated.\n")
			if err != nil || e.Root != source {
				t.Fatalf("update: %+v %v", e, err)
			}
			if source == RootInstalled {
				rec, ok := readRecord(root, "custom-skill")
				if !ok || rec.Hash != e.Hash {
					t.Fatal("installed pin stale")
				}
			}
			if err := ix.Delete("custom-skill"); err != nil {
				t.Fatal(err)
			}
			if source == RootInstalled {
				if _, ok := readRecord(root, "custom-skill"); ok {
					t.Fatal("deleted pin remains")
				}
			}
			if _, ok := ix.Get("custom-skill"); ok {
				t.Fatal("deleted entry still indexed")
			}
		})
	}
}

func TestBuiltinOverrideKeepsReferences(t *testing.T) {
	ix := NewIndex(Roots{User: t.TempDir()}, managementBuiltins(t, "jq-recipes"))
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	raw, err := ix.Content("jq-recipes")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := ix.Get("jq-recipes")
	after, err := ix.Update("jq-recipes", raw+"\nUpdated.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Refs) == 0 || len(after.Refs) != len(before.Refs) {
		t.Fatal("override lost references")
	}
}

// Keep mutation fixtures small while using real embedded source and assets.
func managementBuiltins(t *testing.T, name string) fs.FS {
	t.Helper()
	embedded := BuiltinFS()
	matches, err := fs.Glob(embedded, "*/"+name+"/SKILL.md")
	if err != nil || len(matches) != 1 {
		t.Fatalf("builtin fixture %s: %v %v", name, matches, err)
	}
	out := fstest.MapFS{}
	err = fs.WalkDir(embedded, strings.TrimSuffix(matches[0], "/SKILL.md"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(embedded, path)
		if err != nil {
			return err
		}
		out[path] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuiltinUpdateBacksUpInvalidOverride(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "task-breakdown")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	invalid := []byte("invalid operator content that must be preserved")
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), invalid, 0o644); err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(Roots{User: root}, managementBuiltins(t, "task-breakdown"))
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	e, ok := ix.Get("task-breakdown")
	if !ok || e.Root != RootBuiltin {
		t.Fatal("invalid user file should fall back to builtin")
	}
	raw, err := ix.Content("task-breakdown")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Update("task-breakdown", raw); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(dest, "SKILL.md.backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup: %v %v", backups, err)
	}
	saved, err := os.ReadFile(backups[0])
	if err != nil || string(saved) != string(invalid) {
		t.Fatalf("invalid override was not preserved: %q %v", saved, err)
	}
}
