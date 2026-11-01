package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInterimRun builds a quick-shaped run directory: trials.jsonl
// plus manifest, with n trials per task at the given reward pattern.
func writeInterimRun(t *testing.T, dir string, rewards map[string][]float64) {
	t.Helper()
	profileHash := DefaultEvalProfile().Hash()
	m := RunManifest{
		Dataset: "quick/local", Model: "m", Provider: "openai", Effort: "low",
		ProfileHash: profileHash, ArmProfileHash: profileHash,
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
				InputTokens: 1000, OutputTokens: 100, CostUSD: 0.01,
				WallSecs: 5, ModelSecs: 2, ToolSecs: 1,
				ModelCalls: 3, ToolCallSum: 5,
			}
			if err := AppendTrialIndex(dir, row); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func writeBaselineFile(t *testing.T, rates map[string]float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.json")
	doc := map[string]any{"baseline_rate": rates}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInterimContinueAgainstRun(t *testing.T) {
	run := t.TempDir()
	ref := t.TempDir()
	// Current matches the reference on the completed tasks.
	writeInterimRun(t, run, map[string][]float64{"t/a": {1, 0}, "t/b": {1, 1}})
	writeInterimRun(t, ref, map[string][]float64{"t/a": {1, 0}, "t/b": {1, 1}, "t/c": {1}})
	rep, err := InterimEstimate(run, InterimOptions{Against: ref, Reps: 2000, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if rep.AgainstKind != "run" {
		t.Fatalf("kind: %s", rep.AgainstKind)
	}
	if len(rep.CompletedTasks) != 2 {
		t.Fatalf("paired tasks: %v", rep.CompletedTasks)
	}
	if rep.Verdict != InterimContinue {
		t.Fatalf("matching run should continue: %+v", rep.Diff)
	}
	if rep.Reference != 0.75 {
		t.Fatalf("reference mean: %v", rep.Reference)
	}
	md := RenderInterimMarkdown(rep)
	if !strings.Contains(md, "CONTINUE") || !strings.Contains(md, "t/a") {
		t.Fatalf("markdown: %s", md)
	}
	out := t.TempDir()
	if err := WriteInterimReport(out, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "interim.json")); err != nil {
		t.Fatal(err)
	}
}

func TestInterimStopAgainstBaseline(t *testing.T) {
	run := t.TempDir()
	// Current fails everywhere on 8 tasks; the baseline passes: the
	// constant -1 diffs give a degenerate CI at -1, far below the bar.
	rewards := map[string][]float64{}
	rates := map[string]float64{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		rewards["t/"+name] = []float64{0, 0}
		rates["t/"+name] = 1.0
	}
	writeInterimRun(t, run, rewards)
	base := writeBaselineFile(t, rates)
	rep, err := InterimEstimate(run, InterimOptions{Against: base, Reps: 2000, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if rep.AgainstKind != "baseline" {
		t.Fatalf("kind: %s", rep.AgainstKind)
	}
	if rep.Verdict != InterimStop {
		t.Fatalf("hopeless run should stop: %+v", rep.Diff)
	}
	if rep.Diff.High >= -DefaultPracticalBar {
		t.Fatalf("upper bound should clear the bar: %+v", rep.Diff)
	}
}

func TestInterimPartialOverlap(t *testing.T) {
	run := t.TempDir()
	// Only t/a completed so far (t/b has no trials yet); the
	// reference covers both. The estimate uses just t/a.
	writeInterimRun(t, run, map[string][]float64{"t/a": {1}})
	ref := t.TempDir()
	writeInterimRun(t, ref, map[string][]float64{"t/a": {1}, "t/b": {0}})
	rep, err := InterimEstimate(run, InterimOptions{Against: ref, Reps: 500, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.CompletedTasks) != 1 || rep.CompletedTasks[0] != "t/a" {
		t.Fatalf("partial pairing: %v", rep.CompletedTasks)
	}
	if rep.Verdict != InterimContinue {
		t.Fatalf("tie should continue: %+v", rep.Diff)
	}
}

func TestInterimHarborPartialScan(t *testing.T) {
	// A Harbor-shaped partial run without trials.jsonl: two finished
	// trial dirs with result.json plus one still-executing dir
	// without result.json (ignored).
	run := t.TempDir()
	mkTrial := func(name, task string, reward float64) {
		t.Helper()
		dir := filepath.Join(run, name)
		if err := os.MkdirAll(filepath.Join(dir, "agent", "excalibur"), 0o755); err != nil {
			t.Fatal(err)
		}
		doc := map[string]any{
			"task_name": task, "trial_name": name,
			"verifier_result": map[string]any{"rewards": map[string]any{"score": reward}},
		}
		raw, _ := json.Marshal(doc)
		if err := os.WriteFile(filepath.Join(dir, "result.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkTrial("t1__a", "task-one", 1)
	mkTrial("t2__a", "task-two", 0)
	if err := os.MkdirAll(filepath.Join(run, "t3__running", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	trials, err := LoadPartialRunTrials(run)
	if err != nil {
		t.Fatal(err)
	}
	if len(trials) != 2 {
		t.Fatalf("partial scan: %+v", trials)
	}
	base := writeBaselineFile(t, map[string]float64{"task-one": 1, "task-two": 1})
	rep, err := InterimEstimate(run, InterimOptions{Against: base, Reps: 500, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.CompletedTasks) != 2 {
		t.Fatalf("completed: %v", rep.CompletedTasks)
	}
}

func TestInterimErrors(t *testing.T) {
	run := t.TempDir()
	ref := t.TempDir()
	writeInterimRun(t, run, map[string][]float64{"t/a": {1}})
	writeInterimRun(t, ref, map[string][]float64{"t/b": {1}})
	if _, err := InterimEstimate(run, InterimOptions{Against: ref, Reps: 100, Seed: 7}); err == nil {
		t.Fatal("no overlap should fail")
	}
	if _, err := InterimEstimate(run, InterimOptions{Reps: 100, Seed: 7}); err == nil {
		t.Fatal("missing --against should fail")
	}
	if _, err := InterimEstimate(t.TempDir(), InterimOptions{Against: ref}); err == nil {
		t.Fatal("empty run should fail")
	}
	if _, err := InterimEstimate(run, InterimOptions{Against: filepath.Join(t.TempDir(), "missing.json")}); err == nil {
		t.Fatal("missing baseline should fail")
	}
	// Env bar override is honored when the flag is unset.
	t.Setenv(InterimBarSwitch, "0.50")
	if got := InterimBar(0); got != 0.50 {
		t.Fatalf("env bar: %v", got)
	}
	if got := InterimBar(0.03); got != 0.03 {
		t.Fatalf("flag wins: %v", got)
	}
}
