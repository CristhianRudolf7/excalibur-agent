package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Modified, vanished and added inputs: the snapshot reports the first two
// (a provided file that changed or disappeared), never the new file, and
// it never walks build or dependency directories.
func TestInputSnapshotReportsModifiedAndVanished(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "alpha")
	writeFile(t, filepath.Join(dir, "b.txt"), "bravo")
	writeFile(t, filepath.Join(dir, "sub", "c.txt"), "charlie")
	writeFile(t, filepath.Join(dir, "node_modules", "x.js"), "skipped")
	snap, err := TakeInputSnapshot(dir, InputSnapshotHashBudget)
	if err != nil || snap == nil {
		t.Fatalf("snapshot: %v %v", snap, err)
	}
	if snap.Has(filepath.Join(dir, "node_modules", "x.js")) {
		t.Fatal("dependency directory must be skipped")
	}
	if !snap.Has(filepath.Join(dir, "sub", "c.txt")) {
		t.Fatal("nested provided file must be recorded")
	}
	// Same size, different content: caught by the hash.
	writeFile(t, filepath.Join(dir, "a.txt"), "ALPHA")
	if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "d.txt"), "new file")
	changes := snap.Changes(nil)
	got := map[string]string{}
	for _, c := range changes {
		got[filepath.Base(c.Path)] = c.Kind
	}
	if got["a.txt"] != "modified" || got["b.txt"] != "vanished" || len(got) != 2 {
		t.Fatalf("changes = %v, want a.txt modified and b.txt vanished", got)
	}
}

// Exempt paths (the instruction asked to change them) are not reported.
func TestInputSnapshotExemptsRequestedChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.ini"), "a=1")
	snap, err := TakeInputSnapshot(dir, InputSnapshotHashBudget)
	if err != nil || snap == nil {
		t.Fatalf("snapshot: %v %v", snap, err)
	}
	writeFile(t, filepath.Join(dir, "config.ini"), "a=2")
	exempt := func(p string) bool { return filepath.Base(p) == "config.ini" }
	if changes := snap.Changes(exempt); len(changes) != 0 {
		t.Fatalf("exempt path reported: %v", changes)
	}
	if changes := snap.Changes(nil); len(changes) != 1 {
		t.Fatalf("unexempted change must be reported once, got %v", changes)
	}
}

// Beyond the hash budget a file is recorded by size and mtime only: a
// size change is still caught.
func TestInputSnapshotBudgetFallsBackToMetadata(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "big.dat"), "0123456789")
	snap, err := TakeInputSnapshot(dir, 4)
	if err != nil || snap == nil {
		t.Fatalf("snapshot: %v %v", snap, err)
	}
	for _, e := range snap.Entries {
		if e.SHA256 != "" {
			t.Fatalf("file beyond the %d-byte budget must not be hashed", 4)
		}
	}
	writeFile(t, filepath.Join(dir, "big.dat"), "short")
	if changes := snap.Changes(nil); len(changes) != 1 || changes[0].Kind != "modified" {
		t.Fatalf("size change must be caught without a hash: %v", changes)
	}
}

// Roots that cannot be snapshotted yield no snapshot.
func TestInputSnapshotRootsWithoutSnapshot(t *testing.T) {
	if snap, err := TakeInputSnapshot("", InputSnapshotHashBudget); snap != nil || err != nil {
		t.Fatalf("empty root: %v %v", snap, err)
	}
	if snap, err := TakeInputSnapshot("/", InputSnapshotHashBudget); snap != nil || err != nil {
		t.Fatalf("filesystem root: %v %v", snap, err)
	}
	if snap, err := TakeInputSnapshot(filepath.Join(t.TempDir(), "missing"), InputSnapshotHashBudget); snap != nil || err != nil {
		t.Fatalf("missing root: %v %v", snap, err)
	}
}

func TestInputSnapshotPreservesPrivateBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "input.txt")
	writeFile(t, path, "original bytes")
	copyDir := filepath.Join(t.TempDir(), "originals")
	snap, err := TakeInputSnapshotWithCopies(dir, InputSnapshotHashBudget, copyDir)
	if err != nil || snap == nil {
		t.Fatalf("snapshot: %v %v", snap, err)
	}
	copy := snap.Entries[path].OriginalCopy
	if copy == "" {
		t.Fatal("missing original byte copy")
	}
	writeFile(t, path, "modified")
	content, err := os.ReadFile(copy)
	if err != nil || string(content) != "original bytes" {
		t.Fatalf("modified source damaged copy: %q %v", content, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(copy)
	if err != nil || string(content) != "original bytes" {
		t.Fatalf("deleted source lost copy: %q %v", content, err)
	}
	info, err := os.Stat(copyDir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private copy directory: %v %v", info, err)
	}
}

func TestInputSnapshotSkipsOversizedTreeCopies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "small.txt"), "small")
	large := filepath.Join(dir, "large.dat")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(InputSnapshotCopyBudget)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "copies")
	snap, err := TakeInputSnapshotWithCopies(dir, 0, dest)
	if err != nil || snap == nil || len(snap.Entries) != 2 {
		t.Fatalf("metadata still required: %+v %v", snap, err)
	}
	for _, entry := range snap.Entries {
		if entry.OriginalCopy != "" {
			t.Fatal("oversized tree was partially copied")
		}
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("oversized copy destination exists: %v", err)
	}
}

func TestInputSnapshotCopySkipsBuildCacheAndOutsideSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, outside, "outside")
	for _, name := range []string{"build", ".cache", "node_modules"} {
		writeFile(t, filepath.Join(dir, name, "skip"), "ignored")
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "private", "copies")
	writeFile(t, filepath.Join(dest, "old"), "old snapshot")
	path := filepath.Join(dir, "input")
	writeFile(t, path, "input")
	snap, err := TakeInputSnapshotWithCopies(dir, 1000, dest)
	if err != nil || snap == nil || len(snap.Entries) != 1 || snap.Entries[path].OriginalCopy == "" {
		t.Fatalf("unexpected entries: %+v %v", snap, err)
	}
}

func TestManifestRestore(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "input.dat")
	if err := os.WriteFile(original, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	snap, err := TakeInputSnapshotWithCopies(root, 0, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{"", "scratch/copy.dat"} {
		if err := snap.Restore("input.dat", dest); err != nil {
			t.Fatal(err)
		}
		target := original
		if dest != "" {
			target = filepath.Join(root, dest)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "original" {
			t.Fatalf("restore: %q %v", data, err)
		}
	}
	for _, pair := range [][2]string{{"other.dat", ""}, {"../input.dat", ""}, {"input.dat", "../escape"}, {"input.dat", "a/../escape"}} {
		if err := snap.Restore(pair[0], pair[1]); err == nil {
			t.Fatal("accepted", pair)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := snap.Restore("input.dat", "link/escape"); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if err := os.WriteFile(snap.Entries[original].OriginalCopy, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := snap.Restore("input.dat", ""); err == nil {
		t.Fatal("corrupt copy accepted")
	}
}
