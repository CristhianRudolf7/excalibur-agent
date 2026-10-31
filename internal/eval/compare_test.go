package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCompareRun builds a finished run directory: manifest plus a
// trials.jsonl index with n trials per task at the given rewards.
func writeCompareRun(t *testing.T, dir, arm string, switches map[string]string, rewards map[string][]float64, cost float64) {
	t.Helper()
	profileHash := DefaultEvalProfile().Hash()
	m := RunManifest{
		Dataset: "quick/local", Arm: arm, Switches: switches,
		Model: "m", Provider: "openai", Effort: "low",
		ProfileHash: profileHash, ArmProfileHash: ArmProfileHash(profileHash, arm, switches),
	}
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	for task, rs := range rewards {
		for i, r := range rs {
			cat := ""
			failed := false
			if r == 0 {
				cat = FailVerifier
			}
			row := BenchTrialIndex{
				Task: task, Trial: task + "__t" + string(rune('0'+i)),
				Reward: r, FailCategory: cat,
				InputTokens: 1000, OutputTokens: 100, CostUSD: cost,
				WallSecs: 5, ModelSecs: 2, ToolSecs: 1,
				PeakRSS: 1 << 20, HarnessPeakRSS: 1 << 20, CPUSeconds: 0.5,
				ModelCalls: 3, ToolCallSum: 5,
			}
			_ = failed
			if err := AppendTrialIndex(dir, row); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCompareClearWin(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	tasks := map[string][]float64{"quick/a": {1, 1, 0}, "quick/b": {0, 0, 0}, "quick/c": {1, 0, 0}}
	writeCompareRun(t, dirA, "", nil, tasks, 0.01)
	better := map[string][]float64{"quick/a": {1, 1, 1}, "quick/b": {1, 1, 0}, "quick/c": {1, 1, 1}}
	writeCompareRun(t, dirB, "no-verify", map[string]string{"EXCALIBUR_SELF_VERIFY": "off"}, better, 0.02)
	rep, err := CompareRuns(dirA, dirB, 2, 2000, 7, 0.03)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "ACCEPT" || !rep.Accepted {
		t.Fatalf("clear win rejected: %+v", rep.Diff)
	}
	if rep.Diff.Low <= 0 {
		t.Fatalf("paired interval should exclude 0: %+v", rep.Diff)
	}
	if rep.PassAtKB < rep.PassAtKA || rep.PassPowKB < rep.PassPowKA {
		t.Fatalf("pass metrics should improve: %+v", rep)
	}
	if rep.SwitchDiff["EXCALIBUR_SELF_VERIFY"] != [2]string{"<absent>", "off"} {
		t.Fatalf("switch diff wrong: %v", rep.SwitchDiff)
	}
	if rep.SwitchDiff["arm"] != [2]string{"<absent>", "no-verify"} {
		t.Fatalf("arm diff missing: %v", rep.SwitchDiff)
	}
	if rep.CostPerSuccessA <= 0 || rep.CostPerSuccessB <= 0 {
		t.Fatal("per-success costs missing")
	}
	if rep.ByTask[0].Task == "" || len(rep.ByTask) != 3 {
		t.Fatalf("per-task rows wrong: %+v", rep.ByTask)
	}
	md := RenderCompareMarkdown(rep)
	for _, want := range []string{"ACCEPT", "Configuration diff", "Per task", "EXCALIBUR_SELF_VERIFY"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q", want)
		}
	}
	out := t.TempDir()
	if err := WriteCompareReport(out, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "compare.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCompareTieRejected(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	tasks := map[string][]float64{"quick/a": {1, 0}, "quick/b": {0, 1}}
	writeCompareRun(t, dirA, "", nil, tasks, 0.01)
	writeCompareRun(t, dirB, "", nil, tasks, 0.01)
	rep, err := CompareRuns(dirA, dirB, 1, 2000, 7, 0.03)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Accepted || rep.Verdict != "REJECT" {
		t.Fatalf("tie accepted: %+v", rep.Diff)
	}
	if len(rep.SwitchDiff) != 0 {
		t.Fatalf("identical arms should diff empty: %v", rep.SwitchDiff)
	}
}

func TestCompareUnpairedAndFallback(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeCompareRun(t, dirA, "", nil, map[string][]float64{"quick/a": {1}}, 0.01)
	writeCompareRun(t, dirB, "", nil, map[string][]float64{"quick/b": {1}}, 0.01)
	if _, err := CompareRuns(dirA, dirB, 1, 100, 7, 0.03); err == nil {
		t.Fatal("expected no-shared-tasks error")
	}
	// Trial.json fallback (no trials.jsonl): quick dirs without an
	// index still compare.
	dirC := t.TempDir()
	dirD := t.TempDir()
	writeTrial := func(dir, id string, reward float64) {
		t.Helper()
		tr := TrialOutcome{TrialID: id, TaskID: "quick/a", Reward: reward}
		if err := WriteTrial(filepath.Join(dir, id), tr); err != nil {
			t.Fatal(err)
		}
	}
	writeTrial(dirC, "a1", 1)
	writeTrial(dirC, "a2", 0)
	writeTrial(dirD, "b1", 1)
	writeTrial(dirD, "b2", 1)
	rep, err := CompareRuns(dirC, dirD, 1, 500, 7, 0.99)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.PairedTasks) != 1 || len(rep.UnpairedA)+len(rep.UnpairedB) != 0 {
		t.Fatalf("pairing wrong: %+v", rep)
	}
	if rep.Accepted {
		t.Fatal("impossible bar should reject")
	}
}

func TestCompareCountsFailures(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	mk := func(reward float64, cat string) BenchTrialIndex {
		return BenchTrialIndex{Task: "quick/a", Trial: "t", Reward: reward, FailCategory: cat}
	}
	for _, row := range []BenchTrialIndex{mk(1, ""), mk(0, FailVerifier), mk(0, FailTimeout)} {
		if err := AppendTrialIndex(dirA, row); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []BenchTrialIndex{mk(1, ""), mk(0, FailHarness), mk(0, FailHarness)} {
		if err := AppendTrialIndex(dirB, row); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := CompareRuns(dirA, dirB, 1, 500, 7, 0.99)
	if err != nil {
		t.Fatal(err)
	}
	if rep.FailuresA[FailVerifier] != 1 || rep.FailuresA[FailTimeout] != 1 {
		t.Fatalf("failures A: %v", rep.FailuresA)
	}
	if rep.FailuresB[FailHarness] != 2 {
		t.Fatalf("failures B: %v", rep.FailuresB)
	}
}
