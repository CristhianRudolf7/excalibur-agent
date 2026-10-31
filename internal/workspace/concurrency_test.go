package workspace

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Section 23.5 concurrency suite: two agents race on one file. The
// lease serializes them; the loser learns who holds it; the merged
// outcome keeps both agents' disjoint edits through history revert
// semantics.
func TestConcurrencySharedFile(t *testing.T) {
	m := NewLeaseManager(WithLeaseTimings(2*time.Second, time.Minute))
	h := NewHistory()
	dir := t.TempDir()
	f := filepath.Join(dir, "shared.go")
	base := "package main\n\nfunc A() {}\n\nfunc B() {}\n"
	if err := os.WriteFile(f, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	// Agent 1 edits func A; agent 2 edits func B. Both serialize on
	// the lease and record history.
	edit := func(holder, old, new string) {
		defer wg.Done()
		rel, err := m.Acquire(f, holder, "editing "+holder)
		if err != nil {
			errs <- err
			return
		}
		defer rel()
		m.Heartbeat(holder)
		data, err := os.ReadFile(f)
		if err != nil {
			errs <- err
			return
		}
		cur := string(data)
		idx := indexOf(cur, old)
		if idx < 0 {
			errs <- busyError(holder + " missed its match (lost race)")
			return
		}
		updated := cur[:idx] + new + cur[idx+len(old):]
		pre := []byte(cur)
		if err := os.WriteFile(f, []byte(updated), 0o644); err != nil {
			errs <- err
			return
		}
		h.Record(f, pre, []byte(updated), holder, "edit_file")
		errs <- nil
	}
	wg.Add(2)
	// Small stagger so both contend rather than serialize trivially.
	go edit("agent-1", "func A() {}", "func A() { /* a1 */ }")
	go func() {
		time.Sleep(20 * time.Millisecond)
		edit("agent-2", "func B() {}", "func B() { /* a2 */ }")
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(f)
	got := string(data)
	if !contains(got, "a1") || !contains(got, "a2") {
		t.Fatalf("both disjoint edits must land: %q", got)
	}
	if len(h.Versions(f)) != 2 {
		t.Fatalf("want 2 recorded versions, got %d", len(h.Versions(f)))
	}
	// The loser path: with the lease held, acquire reports holder.
	rel, err := m.Acquire(f, "agent-1", "hold")
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	if _, err := NewLeaseManager(WithLeaseTimings(30*time.Millisecond, time.Minute)).Acquire(f, "x", ""); err != nil {
		t.Fatalf("fresh manager has no memory of the other manager: %v", err)
	}
}

// The five H7 tools register through the shared Registry like any
// extension (Section 14.12) and answer with validated names.
func TestWorkspaceToolsRegistered(t *testing.T) {
	reg := tools.NewMapRegistry()
	deps := &ToolDeps{
		History:   NewHistory(),
		Snapshots: NewSnapshotManager(t.TempDir(), nil),
		Worktrees: &WorktreeManager{DataDir: t.TempDir()},
		Registry:  NewRegistry(),
		Modes:     &ModeStore{},
		RunID:     "run-t",
	}
	deps.Queue = NewIntegrationQueue(deps.Worktrees, t.TempDir(), "excalibur/integration/t")
	if err := RegisterWorkspaceTools(reg, deps); err != nil {
		t.Fatal(err)
	}
	for _, name := range WorkspaceToolNames() {
		tool, err := reg.Get(name)
		if err != nil {
			t.Fatalf("tool %q not registered: %v", name, err)
		}
		if err := tool.Definition().Validate(); err != nil {
			t.Fatalf("tool %q invalid: %v", name, err)
		}
		if err := tools.ValidateName(name); err != nil {
			t.Fatalf("bad tool name %q: %v", name, err)
		}
	}
	names := reg.Names()
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Fatalf("duplicate tool %q", n)
		}
		seen[n] = true
	}
}

// file_history + revert_edit round trip through the tool surface.
func TestToolsHistoryRevert(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewHistory()
	id := h.Record(f, []byte("one\n"), []byte("two\n"), "run-1", "write_file")
	if err := os.WriteFile(f, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &ToolDeps{History: h, RunID: "run-1"}
	fh := &FileHistoryTool{Deps: deps}
	res, err := fh.Invoke(context.Background(), tools.Invocation{Tool: "file_history", Arguments: jsonRaw(`{"path":` + jsonStr(f) + `}`)})
	if err != nil || res.Status != tools.StatusOK {
		t.Fatalf("file_history: %v %+v", err, res)
	}
	if !contains(res.Output, id) {
		t.Fatalf("history must list the edit: %q", res.Output)
	}
	rv := &RevertEditTool{Deps: deps}
	res, err = rv.Invoke(context.Background(), tools.Invocation{Tool: "revert_edit", Arguments: jsonRaw(`{"edit_id":` + jsonStr(id) + `}`)})
	if err != nil || res.Status != tools.StatusOK {
		t.Fatalf("revert_edit: %v %+v", err, res)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "one\n" {
		t.Fatalf("bad revert: %q", data)
	}
}

// workspace_snapshot take/diff/restore through the tool surface.
func TestToolsSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &ToolDeps{Snapshots: NewSnapshotManager(t.TempDir(), nil), RunID: "r"}
	sn := &WorkspaceSnapshotTool{Deps: deps}
	res, _ := sn.Invoke(context.Background(), tools.Invocation{Tool: "workspace_snapshot",
		Arguments: jsonRaw(`{"action":"take","project":"p","project_dir":` + jsonStr(root) + `}`)})
	if res.Status != tools.StatusOK {
		t.Fatalf("take: %+v", res)
	}
	snaps := deps.Snapshots.List("p")
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ = sn.Invoke(context.Background(), tools.Invocation{Tool: "workspace_snapshot",
		Arguments: jsonRaw(`{"action":"diff","project_dir":` + jsonStr(root) + `,"snapshot_id":` + jsonStr(snaps[0].ID) + `}`)})
	if res.Status != tools.StatusOK || !contains(res.Output, "a.txt") {
		t.Fatalf("diff must show a.txt: %+v", res)
	}
	res, _ = sn.Invoke(context.Background(), tools.Invocation{Tool: "workspace_snapshot",
		Arguments: jsonRaw(`{"action":"restore","project_dir":` + jsonStr(root) + `,"snapshot_id":` + jsonStr(snaps[0].ID) + `}`)})
	if res.Status != tools.StatusOK {
		t.Fatalf("restore: %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "v1\n" {
		t.Fatalf("bad restore: %q", data)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool { return indexOf(s, sub) >= 0 }

type busyError string

func (e busyError) Error() string { return "workspace: " + string(e) + " missed its match (lost race)" }
