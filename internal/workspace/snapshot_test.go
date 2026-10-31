package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// Section 13.10 row: undo a command with the shadow snapshot;
// projects without auto_commit get no commit on the user branch.
func TestSnapshotRestoreCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sm := NewSnapshotManager(t.TempDir(), nil)
	snap, err := sm.Take("proj", root, "run-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Files) != 1 {
		t.Fatalf("want 1 tracked file, got %d (%v skipped)", len(snap.Files), snap.Skipped)
	}
	// The command changes and adds files.
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("v2 by command\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gen.out"), []byte("output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, added, deleted, err := sm.Diff(root, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "main.go" {
		t.Fatalf("bad changed: %v", changed)
	}
	if len(added) != 1 || added[0] != "gen.out" {
		t.Fatalf("bad added: %v", added)
	}
	if len(deleted) != 0 {
		t.Fatalf("bad deleted: %v", deleted)
	}
	restored, err := sm.Restore(root, snap.ID, RestoreOptions{PruneAdded: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Fatalf("want main.go plus pruned gen.out, got %v", restored)
	}
	data, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if string(data) != "v1\n" {
		t.Fatalf("bad restore: %q", data)
	}
	if _, err := os.Stat(filepath.Join(root, "gen.out")); !os.IsNotExist(err) {
		t.Fatal("prune must remove added files")
	}
	// Empty diff after restore.
	changed, added, deleted, err = sm.Diff(root, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 || len(added) != 0 || len(deleted) != 0 {
		t.Fatalf("want clean diff after restore: %v %v %v", changed, added, deleted)
	}
}

// Shadow snapshots survive a manager restart (daemon kill): the
// manifest and blobs persist on disk.
func TestSnapshotPersistsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	sm := NewSnapshotManager(dataDir, nil)
	snap, err := sm.Take("proj", root, "run-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	// New manager over the same data dir, empty memory.
	sm2 := NewSnapshotManager(dataDir, nil)
	got, err := sm2.Get(snap.ID)
	if err != nil {
		t.Fatalf("snapshot must load from disk: %v", err)
	}
	if len(got.Files) != 1 {
		t.Fatalf("bad reloaded snapshot: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sm2.Restore(root, snap.ID, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "keep\n" {
		t.Fatalf("bad restore after restart: %q", data)
	}
}

// Binaries and huge files are skipped, never snapshotted.
func TestSnapshotSkips(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte{0, 1, 2, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, SnapshotMaxFile+1)
	if err := os.WriteFile(filepath.Join(root, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	sm := NewSnapshotManager(t.TempDir(), nil)
	snap, err := sm.Take("proj", root, "run-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Files) != 0 {
		t.Fatalf("binary and huge files must skip, got %v", snap.Files)
	}
	if len(snap.Skipped) != 2 {
		t.Fatalf("skips must be recorded, got %v", snap.Skipped)
	}
}
