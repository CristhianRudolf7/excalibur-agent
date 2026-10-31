package main

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TestTaskWaitJSONExitCodes covers the Section 3.6 stable-code rule
// for `task wait`: the JSON receipt prints the status and the process
// exit follows the task state, so agents chaining orders never parse
// free text. A failed task exits 5 with --json just as without it.
func TestTaskWaitJSONExitCodes(t *testing.T) {
	seed := func(state types.TaskState) string {
		dir := t.TempDir()
		store, err := cli.ResolveStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		rec := &cli.TaskRecord{
			TaskID: types.NewTaskID(), Short: "T1",
			Request: "seeded", State: state, Summary: string(state),
		}
		if err := store.Save(rec); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if code := run([]string{"task", "wait", "T1", "--project", seed(types.TaskFailed), "--timeout", "5", "--json"}); code != ExitFailed {
		t.Fatalf("wait --json on failed exit = %d, want %d", code, ExitFailed)
	}
	if code := run([]string{"task", "wait", "T1", "--project", seed(types.TaskCompleted), "--timeout", "5", "--json"}); code != ExitOK {
		t.Fatalf("wait --json on completed exit = %d, want %d", code, ExitOK)
	}
	if code := run([]string{"task", "wait", "T1", "--project", seed(types.TaskFailed), "--timeout", "5"}); code != ExitFailed {
		t.Fatalf("wait on failed exit = %d, want %d", code, ExitFailed)
	}
}
