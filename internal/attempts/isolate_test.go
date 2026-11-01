package attempts

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCopyDirSkipsGit(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "keep.txt")); err != nil {
		t.Fatalf("keep.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must not be copied")
	}
}

func TestApplyWinnerOverlaysAndDeletes(t *testing.T) {
	orig := t.TempDir()
	if err := os.WriteFile(filepath.Join(orig, "same.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orig, "change.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orig, "gone.txt"), []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}
	winner := t.TempDir()
	if err := os.WriteFile(filepath.Join(winner, "same.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(winner, "change.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(winner, "added.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyWinner(winner, orig)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 {
		t.Fatalf("applied = %v, want 3 entries", applied)
	}
	if raw, _ := os.ReadFile(filepath.Join(orig, "change.txt")); string(raw) != "new" {
		t.Fatalf("change.txt = %q, want new", raw)
	}
	if _, err := os.Stat(filepath.Join(orig, "gone.txt")); !os.IsNotExist(err) {
		t.Fatal("gone.txt must be deleted")
	}
}

func TestPrepareCopiesEmptyAndCopy(t *testing.T) {
	dirs, cleanup, err := PrepareCopies("", 2, filepath.Join(t.TempDir(), "work"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(dirs) != 2 {
		t.Fatalf("dirs = %d, want 2", len(dirs))
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Fatalf("work dir %q missing: %v", d, err)
		}
	}

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs, cleanup, err = PrepareCopies(src, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, d := range dirs {
		if raw, err := os.ReadFile(filepath.Join(d, "a.txt")); err != nil || string(raw) != "a" {
			t.Fatalf("copy %q/a.txt = %q, %v", d, raw, err)
		}
	}
	// Isolation: writing in one copy must not leak into the other.
	if err := os.WriteFile(filepath.Join(dirs[0], "only0.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dirs[1], "only0.txt")); !os.IsNotExist(err) {
		t.Fatal("copies must be isolated")
	}
}

func TestPrepareCopiesGitWorktreeWhenAvailable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	src := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %v (%s)", err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-qm", "init")
	dirs, cleanup, err := PrepareCopies(src, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, d := range dirs {
		if raw, err := os.ReadFile(filepath.Join(d, "a.txt")); err != nil || string(raw) != "a" {
			t.Fatalf("worktree %q/a.txt = %q, %v", d, raw, err)
		}
	}
}
