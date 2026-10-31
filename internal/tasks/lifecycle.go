// Task lifecycle: query, modify, pause, resume, cancel (Section
// 8.5) over the task states of Section 8.2.
//
// The cascade runs against the RunTree interface so this package
// stays engine-independent: the orchestrator (H8) adapts its run
// table to it, and tests use MemRunTree. Pause and cancel semantics
// mirror Section 15.8: pause keeps state, journal, worktrees and
// declared background processes and releases leases; cancel goes
// bottom-up and applies compensations where they exist.
//
// Task states derive from the root run and are never written by the
// model (Section 8.2); see types.DeriveTaskState. Resume returns to
// queued exactly the executions paused by that order, tracked via
// PausedBy. Cancel asks a one-line confirmation through ConfirmFunc.
package tasks

import (
	"fmt"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// RunNode is the slice of run state the cascade needs.
type RunNode struct {
	ID         string
	Parent     string
	Agent      string
	State      types.RunState
	PausedBy   string
	Reason     string
	LastReport string
}

// RunTree is the execution tree the cascade operates on. The
// orchestrator adapts its run table; tests use MemRunTree.
type RunTree interface {
	// RootRun returns the current root run of a task.
	RootRun(task types.TaskID) (RunNode, error)
	// Children returns the direct children of a run.
	Children(run string) []RunNode
	// SetPause pauses a run with a reason and the pausing order id.
	SetPause(run, reason, pausedBy string) error
	// SetResume returns a paused run to queued.
	SetResume(run string) error
	// SetCancel cancels a run, applying compensations bottom-up.
	SetCancel(run string) error
	// BlockingQuestions reports unanswered blocking questions of a run.
	BlockingQuestions(run string) []string
}

// ConfirmFunc asks the one-line confirmation cancel requires
// (Section 8.5). It receives a human-readable summary.
type ConfirmFunc func(summary string) (bool, error)

// Snapshot is the no-model query result of Section 8.5: the task
// tree, states, progress, blocks, responsible agent and last report.
type Snapshot struct {
	Task     types.Task
	Progress string
	Children []ChildSnapshot
	Blocks   []string
	Report   string
}

// ChildSnapshot is one row of the task tree.
type ChildSnapshot struct {
	Run      string
	Agent    string
	State    types.RunState
	Progress string
	Reason   string
}

// Query builds the snapshot from the database without calling any
// model (Section 8.5).
func Query(store PlanningStore, tree RunTree, task types.TaskID) (Snapshot, error) {
	t, err := store.GetTask(task)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Task: t}
	root, err := tree.RootRun(task)
	if err != nil {
		snap.Progress = "no active run"
		return snap, nil
	}
	snap.Report = root.LastReport
	var walk func(n RunNode)
	done, total := 0, 0
	walk = func(n RunNode) {
		total++
		if n.State.IsTerminal() && n.State != types.RunFailed && n.State != types.RunCanceled {
			done++
		}
		snap.Children = append(snap.Children, ChildSnapshot{
			Run: n.ID, Agent: n.Agent, State: n.State, Progress: n.LastReport, Reason: n.Reason,
		})
		for _, q := range tree.BlockingQuestions(n.ID) {
			snap.Blocks = append(snap.Blocks, n.ID+": "+q)
		}
		for _, c := range tree.Children(n.ID) {
			walk(c)
		}
	}
	walk(root)
	sort.Slice(snap.Children, func(i, j int) bool { return snap.Children[i].Run < snap.Children[j].Run })
	snap.Progress = fmt.Sprintf("%d/%d done", done, total)
	return snap, nil
}

// PauseOrder pauses the root run of a task and its whole subtree:
// the root with reason user, descendants with reason parent
// (Section 8.5). It returns the order id; resume takes the same id
// back so only what this order paused returns to queued.
func PauseOrder(store PlanningStore, tree RunTree, task types.TaskID, orderID string) error {
	root, err := tree.RootRun(task)
	if err != nil {
		return err
	}
	if err := tree.SetPause(root.ID, string(types.PauseUser), orderID); err != nil {
		return err
	}
	var rec func(n RunNode) error
	rec = func(n RunNode) error {
		for _, c := range tree.Children(n.ID) {
			if err := tree.SetPause(c.ID, string(types.PauseParent), orderID); err != nil {
				return err
			}
			if err := rec(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := rec(root); err != nil {
		return err
	}
	if t, err := store.GetTask(task); err == nil {
		t.State = types.TaskPaused
		t.UpdatedAt = time.Now().UTC()
		_ = store.UpdateTask(t, t.Version)
	}
	return nil
}

// ResumeOrder returns to queued exactly the executions paused by the
// given order; the engine continues from the journal with a resume
// note (Sections 8.5, 15.8).
func ResumeOrder(store PlanningStore, tree RunTree, task types.TaskID, orderID string) error {
	root, err := tree.RootRun(task)
	if err != nil {
		return err
	}
	var rec func(n RunNode) error
	rec = func(n RunNode) error {
		if n.PausedBy == orderID && n.State == types.RunPaused {
			if err := tree.SetResume(n.ID); err != nil {
				return err
			}
		}
		for _, c := range tree.Children(n.ID) {
			if err := rec(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := rec(root); err != nil {
		return err
	}
	snap, err := Query(store, tree, task)
	if err != nil {
		return err
	}
	_ = snap
	return nil
}

// CancelOrder cancels a run and its descendants bottom-up, applying
// compensations where they exist (Section 15.8). The task ends
// canceled with its report. Cancel requires a one-line confirmation.
func CancelOrder(confirm ConfirmFunc, store PlanningStore, tree RunTree, task types.TaskID, summary string) error {
	if confirm != nil {
		ok, err := confirm("cancel task " + string(task) + ": " + summary + " (one-line confirmation)")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("tasks: cancel of task %q not confirmed", task)
		}
	}
	root, err := tree.RootRun(task)
	if err != nil {
		return err
	}
	var rec func(n RunNode) error
	rec = func(n RunNode) error {
		for _, c := range tree.Children(n.ID) {
			if err := rec(c); err != nil {
				return err
			}
		}
		return tree.SetCancel(n.ID)
	}
	if err := rec(root); err != nil {
		return err
	}
	if t, err := store.GetTask(task); err == nil {
		t.State = types.TaskCanceled
		t.UpdatedAt = time.Now().UTC()
		_ = store.UpdateTask(t, t.Version)
	}
	return nil
}
