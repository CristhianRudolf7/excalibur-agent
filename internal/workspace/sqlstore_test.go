// SQL workspace persistence: project registry, lease mirror, edit
// history, mode overrides, integration queue and worktree index over
// a real temporary SQLite database, plus crash/restart survival
// (Sections 9.2, 16.2, 16.4, 13.7).
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func openWorkspaceDB(t *testing.T, path string) *store.DB {
	t.Helper()
	return openWorkspaceDBBlobs(t, path, filepath.Join(t.TempDir(), "blobs"))
}

func openWorkspaceDBBlobs(t *testing.T, path, blobDir string) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.DefaultOptions()
	opts.BlobDir = blobDir
	db, err := store.Open(ctx, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLProjectStoreRoundTrip(t *testing.T) {
	db := openWorkspaceDB(t, filepath.Join(t.TempDir(), "ws.db"))
	reg := NewRegistry()
	reg.AttachProjectStore(OpenProjectStore(db))

	root := t.TempDir()
	got, err := reg.Register(Project{Name: "alpha", Alias: "a", Roots: []string{root},
		Description: "d", VCS: "git", SkillGroups: []string{"g"}, Agents: []string{"ag"},
		Settings: ProjectSettings{Build: []string{"go build ./..."}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Register(Project{Name: "alpha", Roots: []string{root}}); err == nil {
		t.Fatal("expected duplicate name error")
	}
	got.Alias = "a2"
	got.Settings.Test = []string{"go test ./..."}
	if _, err := reg.Update(*got); err != nil {
		t.Fatal(err)
	}
	if err := reg.Archive(got.ID); err != nil {
		t.Fatal(err)
	}
	if err := reg.Archive(types.ProjectID("prj_nope")); err == nil {
		t.Fatal("expected unknown project error")
	}

	// A fresh registry over the same database restores everything.
	reg2 := NewRegistry()
	reg2.AttachProjectStore(OpenProjectStore(db))
	if err := reg2.RestoreProjects(); err != nil {
		t.Fatal(err)
	}
	back, err := reg2.Get(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Alias != "a2" || back.Status != ProjectArchived || len(back.Settings.Test) != 1 || len(back.Agents) != 1 {
		t.Fatalf("restored project: %+v", back)
	}
	if !filepath.IsAbs(back.Roots[0]) {
		t.Fatalf("roots not canonical: %v", back.Roots)
	}
}

func TestSQLLeaseMirror(t *testing.T) {
	db := openWorkspaceDB(t, filepath.Join(t.TempDir(), "leases.db"))
	mgr := NewLeaseManager(WithLeaseTimings(50*time.Millisecond, 80*time.Millisecond))
	mgr.AttachLeaseStore(OpenLeaseStore(db))

	rel, err := mgr.Acquire("/tmp/a.go", "run_1", "editing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Acquire("/tmp/b.go", "run_1", "editing"); err != nil {
		t.Fatal(err)
	}
	_ = rel
	mgr.Heartbeat("run_1")
	mgr.Release("/tmp/a.go", "run_1")

	// Crash: a fresh manager restores the live lease but not the
	// released one.
	mgr2 := NewLeaseManager(WithLeaseTimings(50*time.Millisecond, 80*time.Millisecond))
	mgr2.AttachLeaseStore(OpenLeaseStore(db))
	mgr2.RestoreLeases()
	if _, ok := mgr2.Holding("/tmp/b.go"); !ok {
		t.Fatal("live lease lost across restart")
	}
	if _, ok := mgr2.Holding("/tmp/a.go"); ok {
		t.Fatal("released lease came back across restart")
	}
	// ReleaseAll writes through too.
	mgr2.ReleaseAll("run_1")
	mgr3 := NewLeaseManager()
	mgr3.AttachLeaseStore(OpenLeaseStore(db))
	mgr3.RestoreLeases()
	if len(mgr3.Snapshot()) != 0 {
		t.Fatalf("leases after release-all: %+v", mgr3.Snapshot())
	}
}

func TestSQLHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbpath := filepath.Join(t.TempDir(), "hist.db")
	blobDir := filepath.Join(t.TempDir(), "blobs")
	db := openWorkspaceDBBlobs(t, dbpath, blobDir)
	h := OpenSQLHistory(db)

	id := h.Record(path, []byte("package a\n"), []byte("package b\n"), "run_1", "tc_1")
	if vers := h.Versions(path); len(vers) != 1 || vers[0].ID != id {
		t.Fatalf("versions: %+v", vers)
	}
	// Fast-forward revert: the file still holds the postimage.
	if err := os.WriteFile(path, []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Revert(id, "run_1", "tc_2"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "package a\n" {
		t.Fatalf("reverted content: %q %v", data, err)
	}
	// Close and reopen: versions and blobs survive (kill -9 between
	// the intent and the effect still leaves a reversible row).
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openWorkspaceDBBlobs(t, dbpath, blobDir)
	h2 := OpenSQLHistory(db2)
	if vers := h2.Versions(path); len(vers) != 2 {
		t.Fatalf("versions after reopen: %+v", vers)
	}
	got, ok := h2.Get(id)
	if !ok || got.RunID != "run_1" {
		t.Fatalf("get after reopen: %+v %v", got, ok)
	}
	if _, ok := h2.Blob(got.Preimage); !ok {
		t.Fatal("preimage blob lost across reopen")
	}
}

func TestSQLModeQueueWorktree(t *testing.T) {
	db := openWorkspaceDB(t, filepath.Join(t.TempDir(), "mwq.db"))

	modes, err := OpenModeStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := modes.SetOverride("prj_1", ModeWorktrees); err != nil {
		t.Fatal(err)
	}
	modes2, err := OpenModeStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := modes2.ForProject("prj_1"); !ok || m != ModeWorktrees {
		t.Fatalf("mode after reopen: %q %v", m, ok)
	}

	q := NewIntegrationQueue(nil, "/tmp/root", "excalibur/integration/tsk_1")
	q.AttachQueueStore(OpenQueueStore(db))
	q.Enqueue("child_a", "excalibur/task/tsk_1/child_a")
	q.Enqueue("child_b", "excalibur/task/tsk_1/child_b")
	q2 := NewIntegrationQueue(nil, "/tmp/root", "excalibur/integration/tsk_1")
	q2.AttachQueueStore(OpenQueueStore(db))
	q2.RestoreQueue()
	if pending := q2.Pending(); len(pending) != 2 || pending[0].ChildID != "child_a" {
		t.Fatalf("queue after restore: %+v", pending)
	}

	mgr := &WorktreeManager{DataDir: t.TempDir()}
	mgr.AttachWorktreeIndex(OpenWorktreeIndex(db))
	idx := OpenWorktreeIndex(db)
	if err := idx.Record("prj_1", "child_a", "/tmp/wt/a", "br_a", "abc"); err != nil {
		t.Fatal(err)
	}
	if recs := idx.List("prj_1"); len(recs) != 1 || recs[0].Branch != "br_a" {
		t.Fatalf("worktree index: %+v", recs)
	}
	if err := idx.Forget("prj_1", "child_a"); err != nil {
		t.Fatal(err)
	}
	if recs := idx.List("prj_1"); len(recs) != 0 {
		t.Fatalf("worktree index after forget: %+v", recs)
	}
}
