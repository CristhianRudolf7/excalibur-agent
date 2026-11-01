package attempts

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, root, rel string, data []byte, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(raw)
}

func TestCopyDirCopiesGitAndSkipsHarnessState(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, src, "keep.txt", []byte("v1"), 0o644)
	writeTestFile(t, src, ".git/HEAD", []byte("ref"), 0o644)
	writeTestFile(t, src, ".excalibur/state.json", []byte("{}"), 0o644)
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(dst, "keep.txt")); got != "v1" {
		t.Fatalf("keep.txt = %q", got)
	}
	// .git is part of the full copy so the agent's own git commands
	// see the same repository; it is never promoted back.
	if got := readTestFile(t, filepath.Join(dst, ".git", "HEAD")); got != "ref" {
		t.Fatalf(".git/HEAD = %q, want copied", got)
	}
	if _, err := os.Stat(filepath.Join(dst, ".excalibur")); !os.IsNotExist(err) {
		t.Fatal(".excalibur must not be copied")
	}
}

func TestCopyDirPreservesModesAndLinks(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, src, "bin/tool", []byte("#!/bin/sh\n"), 0o755)
	writeTestFile(t, src, "data/readonly.txt", []byte("ro"), 0o444)
	if err := os.Symlink("bin/tool", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(src, "data"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "data"), 0o755) })
	dst := filepath.Join(t.TempDir(), "copy")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dst, "data"), 0o755) })
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dst, "bin", "tool"))
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("tool mode = %v, %v; want 0755", st, err)
	}
	target, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil || target != "bin/tool" {
		t.Fatalf("link = %q, %v; want bin/tool", target, err)
	}
	if st, err := os.Stat(filepath.Join(dst, "data")); err != nil || st.Mode().Perm() != 0o555 {
		t.Fatalf("read-only dir mode = %v, %v; want 0555", st, err)
	}
	if st, err := os.Stat(filepath.Join(dst, "data", "readonly.txt")); err != nil || st.Mode().Perm() != 0o444 {
		t.Fatalf("read-only file mode = %v, %v; want 0444", st, err)
	}
}

func TestScanSkipsGitAndHarnessStateAndCountsFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", []byte("a"), 0o644)
	writeTestFile(t, root, "d/b.txt", []byte("b"), 0o644)
	writeTestFile(t, root, ".git/index", []byte("i"), 0o644)
	writeTestFile(t, root, ".excalibur/x", []byte("x"), 0o644)
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"a.txt", "d", "d/b.txt"} {
		if _, ok := m[rel]; !ok {
			t.Fatalf("manifest lacks %s: %v", rel, m)
		}
	}
	for rel := range m {
		if strings.HasPrefix(rel, ".git") || strings.HasPrefix(rel, ".excalibur") {
			t.Fatalf("manifest must skip %s", rel)
		}
	}
	// A directory-only change is structure, not a file change.
	base := m
	cur := Manifest{}
	for k, v := range m {
		cur[k] = v
	}
	cur["emptydir"] = Entry{Kind: "dir", Mode: 0o755}
	ch := Diff(base, cur)
	if len(ch.Added) != 1 || ch.Files != 0 {
		t.Fatalf("dir-only add: %+v, want 1 added and 0 files", ch)
	}
}

func TestPrepareWorkspacesIsolatesCopies(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, src, "base.txt", []byte("base"), 0o644)
	ws, baseline, cleanup, err := PrepareWorkspaces(src, 2, filepath.Join(t.TempDir(), "work"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(ws) != 2 {
		t.Fatalf("workspaces = %d, want 2", len(ws))
	}
	if _, ok := baseline["base.txt"]; !ok {
		t.Fatalf("baseline lacks base.txt: %v", baseline)
	}
	for i, w := range ws {
		// The copy keeps the real layout under the attempt root.
		if want := filepath.Join(w.Root, src); w.Dir != want {
			t.Fatalf("attempt %d dir = %q, want %q", i, w.Dir, want)
		}
		if got := readTestFile(t, filepath.Join(w.Dir, "base.txt")); got != "base" {
			t.Fatalf("attempt %d base.txt = %q", i, got)
		}
		if !w.Map.Enabled() || w.Map.Path(filepath.Join(src, "base.txt")) != filepath.Join(w.Dir, "base.txt") {
			t.Fatalf("attempt %d map does not translate the real path: %+v", i, w.Map)
		}
	}
	// Isolation: a write through attempt 0 never reaches attempt 1 or
	// the real directory.
	writeTestFile(t, ws[0].Dir, "only0.txt", []byte("x"), 0o644)
	if _, err := os.Stat(filepath.Join(ws[1].Dir, "only0.txt")); !os.IsNotExist(err) {
		t.Fatal("copies must be isolated from each other")
	}
	if _, err := os.Stat(filepath.Join(src, "only0.txt")); !os.IsNotExist(err) {
		t.Fatal("real directory must stay untouched")
	}
}

func TestPrepareWorkspacesCopiesBuildProducts(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, src, ".gitignore", []byte("build/\n"), 0o644)
	writeTestFile(t, src, "build/lib/prebuilt.so", []byte("elf"), 0o755)
	ws, _, cleanup, err := PrepareWorkspaces(src, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got := readTestFile(t, filepath.Join(ws[0].Dir, "build", "lib", "prebuilt.so")); got != "elf" {
		t.Fatalf("ignored build output must be copied, got %q", got)
	}
}

func TestPrepareWorkspacesEmptySource(t *testing.T) {
	ws, baseline, cleanup, err := PrepareWorkspaces("", 2, filepath.Join(t.TempDir(), "work"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(ws) != 2 || len(baseline) != 0 {
		t.Fatalf("ws=%d baseline=%d, want 2 empty dirs", len(ws), len(baseline))
	}
	for _, w := range ws {
		if w.Map.Enabled() {
			t.Fatal("no real directory means no mapping")
		}
		if st, err := os.Stat(w.Dir); err != nil || !st.IsDir() {
			t.Fatalf("work dir %q missing: %v", w.Dir, err)
		}
	}
}

func TestPrepareWorkspacesCleanupRemovesRoots(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, src, "a.txt", []byte("a"), 0o644)
	ws, _, cleanup, err := PrepareWorkspaces(src, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{ws[0].Root, ws[1].Root}
	cleanup()
	for _, r := range roots {
		if _, err := os.Stat(r); !os.IsNotExist(err) {
			t.Fatalf("attempt root %q must be removed", r)
		}
	}
}

func TestPrepareWorkspacesRefusesBaseInsideProject(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, src, "a.txt", []byte("a"), 0o644)
	if _, _, _, err := PrepareWorkspaces(src, 2, filepath.Join(src, "work")); err == nil {
		t.Fatal("work base inside the project directory must be refused")
	}
}

func TestApplyChangesOverlaysAndDeletes(t *testing.T) {
	orig := t.TempDir()
	writeTestFile(t, orig, "same.txt", []byte("same"), 0o644)
	writeTestFile(t, orig, "change.txt", []byte("old"), 0o644)
	writeTestFile(t, orig, "gone.txt", []byte("bye"), 0o644)
	baseline, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	if err := CopyDir(orig, winner); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, winner, "change.txt", []byte("new"), 0o644)
	writeTestFile(t, winner, "added.txt", []byte("hi"), 0o644)
	if err := os.Remove(filepath.Join(winner, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyChanges(winner, orig, baseline)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"added.txt", "change.txt", "gone.txt"}
	if !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied = %v, want %v", applied, want)
	}
	if got := readTestFile(t, filepath.Join(orig, "change.txt")); got != "new" {
		t.Fatalf("change.txt = %q, want new", got)
	}
	if _, err := os.Stat(filepath.Join(orig, "gone.txt")); !os.IsNotExist(err) {
		t.Fatal("gone.txt must be deleted")
	}
	if got := readTestFile(t, filepath.Join(orig, "same.txt")); got != "same" {
		t.Fatalf("unchanged file moved: %q", got)
	}
}

// TestApplyChangesPromotesBuildProducts covers item 49: a build product
// the task's own .gitignore excludes, plus an executable and a new
// directory, must reach the real directory.
func TestApplyChangesPromotesBuildProducts(t *testing.T) {
	orig := t.TempDir()
	writeTestFile(t, orig, ".gitignore", []byte("build/\n__pycache__/\n"), 0o644)
	writeTestFile(t, orig, "src/main.py", []byte("print(1)"), 0o644)
	baseline, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	if err := CopyDir(orig, winner); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, winner, "build/lib/native.so", []byte("elf"), 0o755)
	writeTestFile(t, winner, "__pycache__/mod.cpython.pyc", []byte("pyc"), 0o644)
	applied, err := ApplyChanges(winner, orig, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 {
		t.Fatal("build products must be promoted")
	}
	if got := readTestFile(t, filepath.Join(orig, "build", "lib", "native.so")); got != "elf" {
		t.Fatalf("native.so = %q", got)
	}
	st, err := os.Stat(filepath.Join(orig, "build", "lib", "native.so"))
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("native.so mode = %v, %v; want 0755", st, err)
	}
	if got := readTestFile(t, filepath.Join(orig, "__pycache__", "mod.cpython.pyc")); got != "pyc" {
		t.Fatalf("pyc = %q", got)
	}
}

func TestApplyChangesFileTypesModesAndLinks(t *testing.T) {
	orig := t.TempDir()
	writeTestFile(t, orig, "mode", []byte("same"), 0o644)
	writeTestFile(t, orig, "file-to-dir", []byte("old"), 0o644)
	writeTestFile(t, orig, "dir-to-file/child", []byte("old"), 0o644)
	writeTestFile(t, orig, "target", []byte("untouched"), 0o644)
	if err := os.Symlink("target", filepath.Join(orig, "link")); err != nil {
		t.Fatal(err)
	}
	baseline, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	writeTestFile(t, winner, "mode", []byte("same"), 0o755)
	writeTestFile(t, winner, "file-to-dir/child", []byte{0, 255, 0}, 0o755)
	writeTestFile(t, winner, "dir-to-file", []byte{255, 0, 1}, 0o644)
	writeTestFile(t, winner, "target", []byte("untouched"), 0o644)
	if err := os.Symlink("file-to-dir/child", filepath.Join(winner, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyChanges(winner, orig, baseline); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(orig, "mode")); err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("mode-only change not applied: %v, %v", st, err)
	}
	if got := readTestFile(t, filepath.Join(orig, "file-to-dir", "child")); got != "\x00\xff\x00" {
		t.Fatalf("file-to-dir child = %q", got)
	}
	if got := readTestFile(t, filepath.Join(orig, "dir-to-file")); got != "\xff\x00\x01" {
		t.Fatalf("dir-to-file = %q", got)
	}
	if target, err := os.Readlink(filepath.Join(orig, "link")); err != nil || target != "file-to-dir/child" {
		t.Fatalf("link = %q, %v", target, err)
	}
	// Re-applying the same winner is a no-op: the real tree now matches.
	again, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	if ch := Diff(again, mustScan(t, winner)); ch.Total() != 0 {
		t.Fatalf("real directory differs from the winner after apply: %+v", ch)
	}
}

func mustScan(t *testing.T, root string) Manifest {
	t.Helper()
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestApplyChangesRollsBackOnFailure: a failure part-way through the
// promotion restores the real directory exactly, so no half-applied
// state reaches the fallback run.
func TestApplyChangesRollsBackOnFailure(t *testing.T) {
	orig := t.TempDir()
	writeTestFile(t, orig, "gone.txt", []byte("bye"), 0o644)
	writeTestFile(t, orig, "change.txt", []byte("old"), 0o644)
	writeTestFile(t, orig, "dir/keep.txt", []byte("keep"), 0o600)
	baseline, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	if err := CopyDir(orig, winner); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(winner, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, winner, "change.txt", []byte("new"), 0o644)
	writeTestFile(t, winner, "added.txt", []byte("hi"), 0o644)

	before := mustScan(t, orig)
	applyHook = func(rel string) error {
		if rel == "change.txt" {
			return errors.New("injected failure")
		}
		return nil
	}
	t.Cleanup(func() { applyHook = nil })
	if _, err := ApplyChanges(winner, orig, baseline); err == nil {
		t.Fatal("injected failure must surface as an error")
	}
	applyHook = nil
	after := mustScan(t, orig)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback did not restore the real directory:\nbefore %v\nafter  %v", before, after)
	}
	if got := readTestFile(t, filepath.Join(orig, "gone.txt")); got != "bye" {
		t.Fatalf("deleted file not restored: %q", got)
	}
}

// TestApplyChangesLeavesEscapedWritesAlone: a command that escaped the
// workdir mapping wrote to the real directory after the baseline. The
// winner's promotion must not revert that unrelated write.
func TestApplyChangesLeavesEscapedWritesAlone(t *testing.T) {
	orig := t.TempDir()
	writeTestFile(t, orig, "a.txt", []byte("a"), 0o644)
	baseline, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	if err := CopyDir(orig, winner); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, winner, "w.txt", []byte("winner"), 0o644)
	writeTestFile(t, orig, "escaped.txt", []byte("escape"), 0o644)
	if _, err := ApplyChanges(winner, orig, baseline); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(orig, "escaped.txt")); got != "escape" {
		t.Fatalf("escaped write was reverted: %q", got)
	}
	if got := readTestFile(t, filepath.Join(orig, "w.txt")); got != "winner" {
		t.Fatalf("w.txt = %q", got)
	}
}

func TestApplyChangesNoChangesIsNoop(t *testing.T) {
	orig := t.TempDir()
	writeTestFile(t, orig, "a.txt", []byte("a"), 0o644)
	baseline, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	if err := CopyDir(orig, winner); err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyChanges(winner, orig, baseline)
	if err != nil || len(applied) != 0 {
		t.Fatalf("no-change apply = %v, %v", applied, err)
	}
}

func TestCheckAppliedZeroFilesIsFailure(t *testing.T) {
	ch := Changes{Added: []string{"x.txt"}, Files: 1}
	if err := checkApplied(ch, nil); err == nil {
		t.Fatal("a winner that changed files must not apply zero")
	}
	if err := checkApplied(Changes{}, nil); err != nil {
		t.Fatalf("no changes applies zero without error: %v", err)
	}
}

func TestTouchedReportsEscapedWrites(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", []byte("a"), 0o644)
	baseline, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "a.txt", []byte("changed"), 0o644)
	ch, err := Touched(baseline, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ch.Changed, []string{"a.txt"}) {
		t.Fatalf("touched = %+v, want a.txt changed", ch)
	}
}

// TestApplyChangesRefusesToReplaceNonEmptyDirectory: an escaped write
// created a directory the winner wants as a file. The plan is refused
// before the real directory changes.
func TestApplyChangesRefusesToReplaceNonEmptyDirectory(t *testing.T) {
	orig := t.TempDir()
	writeTestFile(t, orig, "a.txt", []byte("a"), 0o644)
	baseline, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	if err := CopyDir(orig, winner); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, winner, "data", []byte("winner"), 0o644)
	writeTestFile(t, orig, "data/escaped.txt", []byte("escape"), 0o644)
	before := mustScan(t, orig)
	if _, err := ApplyChanges(winner, orig, baseline); err == nil {
		t.Fatal("replacing a directory with entries outside the winner must be refused")
	}
	if after := mustScan(t, orig); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused plan must not change the real directory")
	}
}
