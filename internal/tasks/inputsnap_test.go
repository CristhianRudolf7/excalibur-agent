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
