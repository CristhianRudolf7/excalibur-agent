package engine

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestRecheckGuardWarnsEveryEightWithoutStopping(t *testing.T) {
	t.Setenv("EXCALIBUR_RECHECK_GUARD", "on")
	run := &Run{Workdir: t.TempDir()}
	res := tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 0.1s  session: sess-1  alive: false\nunchanged data"}
	run.observeRecheck(res, false) // Content seen before the refusal.
	run.LastRefusal = "verify calculated values; inspect artifacts"
	baseline := run.recheckSnapshot()
	if baseline == "" {
		t.Fatal("missing baseline")
	}
	for i := 1; i <= 16; i++ {
		note := run.observeRecheck(res, baseline == run.recheckSnapshot())
		if (note != "") != (i%8 == 0) {
			t.Fatalf("call %d: %q", i, note)
		}
		if note != "" && !strings.Contains(note, "verify calculated values") {
			t.Fatal(note)
		}
	}
	// Actual changes, new output and live sessions each interrupt the streak.
	for _, reset := range []func() string{
		func() string {
			if err := os.WriteFile(filepath.Join(run.Workdir, "result"), []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
			return run.observeRecheck(res, baseline == run.recheckSnapshot())
		},
		func() string { return run.observeRecheck(tools.Result{Output: "new useful evidence"}, true) },
		func() string { return run.observeRecheck(tools.Result{Output: "exit: running  alive: true"}, true) },
	} {
		for i := 0; i < 7; i++ {
			run.observeRecheck(res, true)
		}
		if note := reset(); note != "" || run.rechecks.consecutive != 0 {
			t.Fatalf("reset: %q %+v", note, run.rechecks)
		}
	}
	t.Setenv("EXCALIBUR_RECHECK_GUARD", "off")
	if run.recheckSnapshot() != "" || run.observeRecheck(res, true) != "" {
		t.Fatal("switch off")
	}
}

func TestWorkspaceCheckHashDetectsContentAndDeletion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	old := workspaceCheckHash(dir)
	info, _ := os.Stat(path)
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(path, info.ModTime(), info.ModTime())
	if got := workspaceCheckHash(dir); got == old || got == "" {
		t.Fatal("same-size content change missed")
	}
	_ = os.Remove(path)
	if workspaceCheckHash(dir) == old {
		t.Fatal("deletion missed")
	}
}

func TestRecheckGuardIntegratedLoopKeepsRunning(t *testing.T) {
	t.Setenv("EXCALIBUR_RECHECK_GUARD", "on")
	e, run, model, runner := meteredRun(t, tasks.Limit{MaxSteps: 20}, 9, 0, 0)
	for i := 0; i < 8; i++ {
		runner.add("read_file", types.EffectReadOnly, types.IdemPure, tools.Result{Status: tools.StatusOK, Output: "contents"})
	}
	e.deps.Quality = &QualityConfig{}
	run.Workdir = t.TempDir()
	run.LastRefusal = "verify the transformed data"
	if err := e.Run(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if model.calls != 10 {
		t.Fatalf("guard interrupted calls: %d", model.calls)
	}
	if countContaining(run.VolatileNotes(), "the last 8 calls only re-checked unchanged results") != 1 {
		t.Fatal(run.VolatileNotes())
	}
}

func TestRecheckGuardKeepsMeaningfulOutputValues(t *testing.T) {
	run := &Run{LastRefusal: "check values"}
	first := tools.Result{Output: "exit: 0  duration: 0.1s  session: sess-1\nmeasured at 10:20:30"}
	run.observeRecheck(first, true)
	for i := 0; i < 7; i++ {
		run.observeRecheck(first, true)
	}
	if note := run.observeRecheck(tools.Result{Output: "exit: 0  duration: 0.2s  session: sess-2\nmeasured at 10:20:31"}, true); note != "" {
		t.Fatal("new measurement must interrupt the streak")
	}
	if run.rechecks.consecutive != 0 {
		t.Fatal("measurement did not reset the streak")
	}
}
