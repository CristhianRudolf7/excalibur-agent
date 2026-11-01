package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDiffRun builds a finished run directory with explicit rewards
// plus distinct resource counters per arm so mean differences are
// checkable. Rewards map task -> per-trial rewards.
func writeDiffRun(t *testing.T, dir, arm string, rewards map[string][]float64, modelCalls, toolCalls int64, wall float64, tokens int64) {
	t.Helper()
	profileHash := DefaultEvalProfile().Hash()
	m := RunManifest{
		Dataset: "quick/local", Arm: arm,
		Model: "m", Provider: "openai", Effort: "low",
		ProfileHash: profileHash, ArmProfileHash: ArmProfileHash(profileHash, arm, nil),
	}
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	for task, rs := range rewards {
		for i, r := range rs {
			cat := ""
			if r == 0 {
				cat = FailVerifier
			}
			row := BenchTrialIndex{
				Task: task, Trial: task + "__t" + string(rune('0'+i)),
				Reward: r, FailCategory: cat,
				InputTokens: tokens, OutputTokens: tokens / 10, CostUSD: 0.01,
				WallSecs: wall, ModelSecs: wall / 2, ToolSecs: wall / 4,
				PeakRSS: 1 << 20, HarnessPeakRSS: 1 << 20, CPUSeconds: 0.5,
				ModelCalls: modelCalls, ToolCallSum: toolCalls,
			}
			if err := AppendTrialIndex(dir, row); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDiffClearWinWithFlips(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	// flip1: A fails everywhere, B passes (improved).
	// flip2: A passes, B fails everywhere (regressed).
	// both: mixed on both sides (no flip row).
	tasksA := map[string][]float64{"t/flip1": {0, 0}, "t/flip2": {1, 1}, "t/both": {1, 0}}
	tasksB := map[string][]float64{"t/flip1": {1, 1}, "t/flip2": {0, 0}, "t/both": {0, 1}}
	writeDiffRun(t, dirA, "", tasksA, 3, 5, 5.0, 1000)
	writeDiffRun(t, dirB, "no-verify", tasksB, 6, 9, 9.0, 2000)
	rep, err := DiffRuns(dirA, dirB, DiffOptions{K: 1, Reps: 2000, Seed: 7, Bar: 0.03, NoTriage: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.PairedTasks) != 3 || len(rep.ByTask) != 3 {
		t.Fatalf("paired tasks wrong: %+v", rep.PairedTasks)
	}
	if len(rep.Flips) != 2 {
		t.Fatalf("flips wrong: %+v", rep.Flips)
	}
	byTask := map[string]FlipDetail{}
	for _, f := range rep.Flips {
		byTask[f.Task] = f
	}
	if byTask["t/flip1"].Direction != FlipImproved {
		t.Fatalf("flip1 direction: %+v", byTask["t/flip1"])
	}
	if byTask["t/flip2"].Direction != FlipRegressed {
		t.Fatalf("flip2 direction: %+v", byTask["t/flip2"])
	}
	// Failing side trials are listed: flip1 fails on A (2 trials),
	// flip2 fails on B (2 trials).
	if len(byTask["t/flip1"].Failed) != 2 || byTask["t/flip1"].Failed[0].Side != "A" {
		t.Fatalf("flip1 failed trials: %+v", byTask["t/flip1"].Failed)
	}
	if len(byTask["t/flip2"].Failed) != 2 || byTask["t/flip2"].Failed[0].Side != "B" {
		t.Fatalf("flip2 failed trials: %+v", byTask["t/flip2"].Failed)
	}
	if len(rep.Resources) != 4 {
		t.Fatalf("resources wrong: %+v", rep.Resources)
	}
	for _, r := range rep.Resources {
		if r.Diff <= 0 {
			t.Fatalf("B is heavier on every metric: %+v", rep.Resources)
		}
	}
	md := RenderDiffMarkdown(rep)
	for _, want := range []string{"Per-task pass rates", "Flipped tasks", "Mean resource differences", "Verdict", "t/flip1", "t/flip2"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q", want)
		}
	}
	out := t.TempDir()
	if err := WriteDiffReport(out, rep); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "diff.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back DiffReport
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Flips) != 2 || len(back.Resources) != 4 {
		t.Fatalf("round trip wrong: %+v", back)
	}
}

func TestDiffTriageFromFile(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeDiffRun(t, dirA, "", map[string][]float64{"t/x": {0}}, 3, 5, 5.0, 1000)
	writeDiffRun(t, dirB, "", map[string][]float64{"t/x": {1}}, 3, 5, 5.0, 1000)
	// The failing side is A trial "t/x__t0".
	triage := []TriageResult{{
		Task: "t/x", Trial: "t/x__t0", Reward: 0,
		Cause: TriageToolMisuse, Evidence: "harness rejected the call. Quote: \"unknown target\"",
		FailStep: 3, ToolCalls: 4, FinalState: "completed", DecidedBy: "deterministic",
	}}
	if err := WriteTriageJSONL(filepath.Join(dirA, "triage.jsonl"), triage); err != nil {
		t.Fatal(err)
	}
	rep, err := DiffRuns(dirA, dirB, DiffOptions{K: 1, Reps: 500, Seed: 7, NoTriage: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Flips) != 1 {
		t.Fatalf("flips: %+v", rep.Flips)
	}
	if len(rep.Flips[0].Failed) != 1 {
		t.Fatalf("failed: %+v", rep.Flips[0].Failed)
	}
	got := rep.Flips[0].Failed[0]
	if got.Cause != TriageToolMisuse || !strings.Contains(got.Evidence, "unknown target") {
		t.Fatalf("triage not attached: %+v", got)
	}
	if got.Decided != "deterministic" {
		t.Fatalf("decided: %+v", got)
	}
	// --no-triage leaves the rows without cause.
	repOff, err := DiffRuns(dirA, dirB, DiffOptions{K: 1, Reps: 500, Seed: 7, NoTriage: true})
	if err != nil {
		t.Fatal(err)
	}
	if repOff.Flips[0].Failed[0].Cause != "" {
		t.Fatalf("no-triage should skip: %+v", repOff.Flips[0].Failed[0])
	}
	// Env switch off does the same.
	t.Setenv(DiffTriageSwitch, "0")
	repEnv, err := DiffRuns(dirA, dirB, DiffOptions{K: 1, Reps: 500, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if repEnv.Flips[0].Failed[0].Cause != "" {
		t.Fatalf("switch off should skip: %+v", repEnv.Flips[0].Failed[0])
	}
}

func TestDiffBaselineFile(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeDiffRun(t, dirA, "", map[string][]float64{"t/a": {1, 0}, "t/b": {0, 0}}, 3, 5, 5.0, 1000)
	writeDiffRun(t, dirB, "", map[string][]float64{"t/a": {1, 1}, "t/b": {1, 0}}, 3, 5, 5.0, 1000)
	base := filepath.Join(t.TempDir(), "baseline.json")
	doc := map[string]any{"baseline_rate": map[string]float64{"t/a": 0.5, "t/b": 0.25}}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(base, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := DiffRuns(dirA, dirB, DiffOptions{K: 1, Reps: 500, Seed: 7, BasePath: base, NoTriage: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Baseline["t/a"] != 0.5 || rep.Baseline["t/b"] != 0.25 {
		t.Fatalf("baseline: %v", rep.Baseline)
	}
	if !strings.Contains(RenderDiffMarkdown(rep), "Baseline:") {
		t.Fatal("markdown should name the baseline file")
	}
	if _, err := LoadBaselineFile(base); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"baseline_rate": {"t/a": 2}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaselineFile(bad); err == nil {
		t.Fatal("out-of-range rate should fail")
	}
	if _, err := DiffRuns(dirA, dirB, DiffOptions{BasePath: filepath.Join(t.TempDir(), "missing.json"), NoTriage: true}); err == nil {
		t.Fatal("missing baseline should fail")
	}
}

func TestDiffLazyTriage(t *testing.T) {
	// Run A carries both the trials.jsonl row and the Harbor trial
	// directory, so the lazy deterministic pass (no triage.jsonl,
	// no model) still attaches a cause to the flipped trial.
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeTriageTrial(t, dirA, triageFixture{
		task: "flip", trial: "flip__t0", reward: "0",
		exception:  "operation timed out after 300s",
		finalState: "completed", modelCalls: 2,
	})
	row := BenchTrialIndex{
		Task: "flip", Trial: "flip__t0", Reward: 0, FailCategory: FailTimeout,
		InputTokens: 1000, OutputTokens: 100, CostUSD: 0.01,
		WallSecs: 5, ModelCalls: 3, ToolCallSum: 5,
	}
	if err := AppendTrialIndex(dirA, row); err != nil {
		t.Fatal(err)
	}
	profileHash := DefaultEvalProfile().Hash()
	if err := WriteManifest(dirA, RunManifest{
		Dataset: "quick/local", Model: "m", Provider: "openai", Effort: "low",
		ProfileHash: profileHash, ArmProfileHash: profileHash,
	}); err != nil {
		t.Fatal(err)
	}
	writeDiffRun(t, dirB, "", map[string][]float64{"flip": {1}}, 3, 5, 5.0, 1000)
	// Rename B's trial to a different name so the flip mapping is by
	// task (B passes, A fails): flip detection is task-level.
	rep, err := DiffRuns(dirA, dirB, DiffOptions{K: 1, Reps: 500, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Flips) != 1 || rep.Flips[0].Direction != FlipImproved {
		t.Fatalf("flips: %+v", rep.Flips)
	}
	if len(rep.Flips[0].Failed) != 1 {
		t.Fatalf("failed: %+v", rep.Flips[0].Failed)
	}
	got := rep.Flips[0].Failed[0]
	if got.Cause != TriageTimeout {
		t.Fatalf("lazy triage cause: %+v", got)
	}
	if got.Evidence == "" {
		t.Fatal("lazy triage should carry an evidence quote")
	}
}

func TestDiffErrors(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeDiffRun(t, dirA, "", map[string][]float64{"t/a": {1}}, 3, 5, 5.0, 1000)
	writeDiffRun(t, dirB, "", map[string][]float64{"t/b": {1}}, 3, 5, 5.0, 1000)
	if _, err := DiffRuns(dirA, dirB, DiffOptions{NoTriage: true}); err == nil {
		t.Fatal("no shared tasks should fail")
	}
	empty := t.TempDir()
	if _, err := DiffRuns(empty, dirB, DiffOptions{NoTriage: true}); err == nil {
		t.Fatal("empty run should fail")
	}
}
