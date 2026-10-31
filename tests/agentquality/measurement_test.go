// Package agentquality_test exercises the measurement tooling for
// agent quality (arms, compare, quick suite, resource peaks) through
// the public internal/eval API only: no model calls, no Docker, no
// network. It is the regression gate between arms run cheaply before
// any paid Terminal-Bench trial.
package agentquality_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

// TestFrozenProfileStable pins the Appendix F invariant the arms
// build on: the default eval profile hash never moves unless the
// profile itself changes, and the default arm hashes to it exactly.
func TestFrozenProfileStable(t *testing.T) {
	a := eval.DefaultEvalProfile().Hash()
	b := eval.DefaultEvalProfile().Hash()
	if a == "" || a != b {
		t.Fatalf("frozen profile hash unstable: %q vs %q", a, b)
	}
	if got := eval.ArmProfileHash(a, "", nil); got != a {
		t.Fatal("default arm must reproduce the frozen hash")
	}
}

// TestArmSwitchValidation pins the arm declaration contract the
// bench run and quick commands share.
func TestArmSwitchValidation(t *testing.T) {
	if err := eval.ValidateArm("self-verify-off"); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.ParseSwitches([]string{"EXCALIBUR_SELF_VERIFY=off"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.ParseSwitches([]string{"oops"}); err == nil {
		t.Fatal("expected key=value error")
	}
}

// TestQuickSuiteShape pins the suite contract: 25 own tasks, every
// required category present, every task runnable offline in principle
// (verifier exists, prompt is self-contained).
func TestQuickSuiteShape(t *testing.T) {
	tasks := eval.QuickTasks()
	if len(tasks) != 25 {
		t.Fatalf("suite has %d tasks, want 25", len(tasks))
	}
	cats := map[string]int{}
	for _, task := range tasks {
		if task.Name == "" || task.Prompt == "" || task.Verifies == nil {
			t.Fatalf("task incomplete: %+v", task.Name)
		}
		cats[task.Category]++
	}
	for _, want := range []string{"scripting", "fix", "build", "data", "services", "terminal", "docker"} {
		if cats[want] == 0 {
			t.Fatalf("category %s missing: %v", want, cats)
		}
	}
}

// TestQuickCompareLoop is the cheap ablation loop end to end: two
// quick dirs with different arms compare with the switch diff naming
// the change, all through the public API.
func TestQuickCompareLoop(t *testing.T) {
	run := func(dir, arm string, solve bool) {
		t.Helper()
		r := eval.QuickRunner{
			Dir: dir, Concurrency: 2, Filter: "csv-sum", Arm: arm,
			Switches: map[string]string{"EXCALIBUR_MECHANISM": arm},
			Exec: func(ctx context.Context, bin, prompt, workdir, logsDir string, env []string, timeout time.Duration) (int, error) {
				if solve {
					if err := os.WriteFile(filepath.Join(workdir, "total.txt"), []byte("67\n"), 0o644); err != nil {
						return 1, err
					}
				}
				return 0, nil
			},
		}
		outcomes, err := r.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := r.WriteResults(outcomes); err != nil {
			t.Fatal(err)
		}
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	run(dirA, "mechanism-off", false)
	run(dirB, "mechanism-on", true)
	rep, err := eval.CompareRuns(dirA, dirB, 1, 500, 7, 0.99)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Diff.Mean != 1 {
		t.Fatalf("paired diff = %v, want 1", rep.Diff.Mean)
	}
	if rep.SwitchDiff["EXCALIBUR_MECHANISM"] != [2]string{"mechanism-off", "mechanism-on"} {
		t.Fatalf("switch diff = %v", rep.SwitchDiff)
	}
	if got := eval.LoadRunManifest(dirB); got.Arm != "mechanism-on" {
		t.Fatalf("manifest arm = %q", got.Arm)
	}
}

// TestResourcePeakIndexed pins the sampler fix at the consumer
// level: a trial directory whose resources.jsonl carries a mid-run
// peak followed by a zero final row indexes the peak, not zero.
func TestResourcePeakIndexed(t *testing.T) {
	dir := t.TempDir()
	result := `{"task_name":"terminal-bench/x","trial_name":"x__1",` +
		`"verifier_result":{"rewards":{"reward":1}},` +
		`"agent_execution":{"started_at":"2026-10-31T19:05:01Z","finished_at":"2026-10-31T19:05:04Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "result.json"), []byte(result), 0o644); err != nil {
		t.Fatal(err)
	}
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resources := "{\"at\":\"2026-10-31T19:05:02Z\",\"harness_rss_bytes\":23000000,\"cpu_seconds\":0.25,\"processes\":9}\n" +
		"{\"at\":\"2026-10-31T19:05:04Z\",\"harness_rss_bytes\":0,\"cpu_seconds\":0,\"processes\":1,\"note\":\"final\"}\n"
	if err := os.WriteFile(filepath.Join(exDir, "resources.jsonl"), []byte(resources), 0o644); err != nil {
		t.Fatal(err)
	}
	row, err := eval.IndexTrial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if row.HarnessPeakRSS != 23000000 || row.PeakRSS != 23000000 {
		t.Fatalf("peaks = %d/%d, want the observed max", row.HarnessPeakRSS, row.PeakRSS)
	}
	if row.CPUSeconds != 0.25 {
		t.Fatalf("cpu = %v, want 0.25", row.CPUSeconds)
	}
}
