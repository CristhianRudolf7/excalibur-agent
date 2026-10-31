// Lifecycle tests (Sections 8.2, 8.5; verification rows 8.11.3 and
// 8.11.6): task states derive from the root run without a model
// write, reopening a finished task starts a fresh root run and keeps
// the history, and pause/resume/cancel cascade over the subtree with
// order-scoped resume and a one-line cancel confirmation.
package tasks

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func seedTask(t *testing.T, store *MemoryStore, scope types.Scope, code types.TaskRef) types.TaskID {
	t.Helper()
	id := types.NewTaskID()
	task := types.Task{
		ID: id, Code: code, Scope: scope, Level: types.TaskN1,
		Request: types.RequestFile{Original: "do it", Interpretation: "do it"},
		State:   types.TaskOpen, Version: 1,
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTaskStateDerivedFromRootRun(t *testing.T) {
	// Section 8.2 table, via the shared DeriveTaskState.
	cases := map[types.RunState]types.TaskState{
		types.RunCreated:          types.TaskOpen,
		types.RunQueued:           types.TaskInProgress,
		types.RunRunning:          types.TaskInProgress,
		types.RunWaiting:          types.TaskInProgress,
		types.RunVerifying:        types.TaskInProgress,
		types.RunRecovering:       types.TaskInProgress,
		types.RunPaused:           types.TaskPaused,
		types.RunBlocked:          types.TaskBlocked,
		types.RunCompleted:        types.TaskCompleted,
		types.RunCompletedPending: types.TaskCompletedPending,
		types.RunFailed:           types.TaskFailed,
		types.RunCanceled:         types.TaskCanceled,
	}
	for run, want := range cases {
		if got := types.DeriveTaskState(run); got != want {
			t.Fatalf("run %q derives %q, want %q", run, got, want)
		}
	}
}

func TestReopenFinishedTask(t *testing.T) {
	store := NewMemoryStore()
	id := seedTask(t, store, seedScope(), "T1")
	run1 := types.NewRunID()
	if err := store.SetTaskRun(id, run1, types.DeriveTaskState(types.RunCompleted)); err != nil {
		t.Fatal(err)
	}
	run2 := types.NewRunID()
	if err := store.ReopenTask(id, run2); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != types.TaskOpen || got.RootRunID == nil || *got.RootRunID != run2 {
		t.Fatalf("reopen did not start a fresh root run: %+v", got)
	}
	hist := store.TaskHistory(id)
	if len(hist) != 1 || hist[0].RootRunID == nil || *hist[0].RootRunID != run1 {
		t.Fatalf("previous execution not kept in history: %+v", hist)
	}
	if err := store.ReopenTask(id, types.NewRunID()); err == nil {
		t.Fatal("reopening an open task must fail")
	}
}

func seedTree() (*MemRunTree, types.TaskID) {
	tree := NewMemRunTree()
	task := types.TaskID("tsk_01JTASKTASKTASKTASKTS")
	tree.Add(RunNode{ID: "run_root", State: types.RunRunning})
	tree.Add(RunNode{ID: "run_a", Parent: "run_root", State: types.RunRunning})
	tree.Add(RunNode{ID: "run_b", Parent: "run_root", State: types.RunWaiting})
	tree.Add(RunNode{ID: "run_aa", Parent: "run_a", State: types.RunRunning})
	tree.SetRoot(task, "run_root")
	return tree, task
}

// seedTreeTask registers the tree task in the store so the cascade
// can derive its state.
func seedTreeTask(t *testing.T, store *MemoryStore, task types.TaskID) {
	t.Helper()
	if err := store.CreateTask(types.Task{
		ID: task, Code: "T1", Scope: seedScope(), Level: types.TaskN1,
		Request: types.RequestFile{Original: "do it", Interpretation: "do it"},
		State:   types.TaskOpen, Version: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPauseResumeCascade(t *testing.T) {
	store := NewMemoryStore()
	tree, task := seedTree()
	seedTreeTask(t, store, task)
	if err := PauseOrder(store, tree, task, "order-1"); err != nil {
		t.Fatal(err)
	}
	for id, n := range tree.Nodes {
		if n.State != types.RunPaused {
			t.Fatalf("node %s not paused: %q", id, n.State)
		}
	}
	if tree.Nodes["run_root"].Reason != string(types.PauseUser) {
		t.Fatal("root must pause with the user motive")
	}
	if tree.Nodes["run_a"].Reason != string(types.PauseParent) {
		t.Fatal("descendants must pause with the parent motive")
	}
	// A second, unrelated pause lands on one node only.
	if err := tree.SetPause("run_b", string(types.PauseResource), "order-2"); err != nil {
		t.Fatal(err)
	}
	if err := ResumeOrder(store, tree, task, "order-1"); err != nil {
		t.Fatal(err)
	}
	if tree.Nodes["run_root"].State != types.RunQueued || tree.Nodes["run_a"].State != types.RunQueued {
		t.Fatal("resume must return what the order paused")
	}
	if tree.Nodes["run_b"].State != types.RunPaused {
		t.Fatal("resume must not touch what another order paused")
	}
}

func TestCancelCascadeBottomUpWithConfirmation(t *testing.T) {
	store := NewMemoryStore()
	tree, task := seedTree()
	seedTreeTask(t, store, task)
	confirmed := false
	confirm := func(summary string) (bool, error) {
		confirmed = true
		if summary == "" {
			t.Error("confirmation summary must not be empty")
		}
		return true, nil
	}
	if err := CancelOrder(confirm, store, tree, task, "stop work"); err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("cancel must ask its one-line confirmation")
	}
	// Bottom-up: leaves cancel before their parents.
	leaves := map[string]int{}
	for i, id := range tree.Canceled {
		leaves[id] = i
	}
	if !(leaves["run_aa"] < leaves["run_a"] && leaves["run_a"] < leaves["run_root"]) {
		t.Fatalf("cancel was not bottom-up: %v", tree.Canceled)
	}
	if tree.Nodes["run_root"].State != types.RunCanceled {
		t.Fatal("root must be canceled")
	}
}

func TestCancelWithoutConfirmationAborts(t *testing.T) {
	store := NewMemoryStore()
	tree, task := seedTree()
	seedTreeTask(t, store, task)
	deny := func(summary string) (bool, error) { return false, nil }
	if err := CancelOrder(deny, store, tree, task, "stop work"); err == nil {
		t.Fatal("denied cancel must fail")
	}
	if len(tree.Canceled) != 0 {
		t.Fatal("denied cancel must change nothing")
	}
}

func TestQuerySnapshot(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	id := seedTask(t, store, scope, "T9")
	tree := NewMemRunTree()
	tree.Add(RunNode{ID: "r", Agent: "child", State: types.RunRunning, LastReport: "half done"})
	tree.SetRoot(id, "r")
	tree.Questions["r"] = []string{"which region?"}
	snap, err := Query(store, tree, id)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Progress != "0/1 done" {
		t.Fatalf("progress wrong: %q", snap.Progress)
	}
	if len(snap.Blocks) != 1 {
		t.Fatalf("blocking question missing: %+v", snap)
	}
	if snap.Children[0].Agent != "child" || snap.Children[0].Progress != "half done" {
		t.Fatalf("child row wrong: %+v", snap.Children)
	}
}
