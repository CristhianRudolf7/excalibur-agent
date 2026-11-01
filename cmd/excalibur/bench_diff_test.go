package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeCLIRun(t *testing.T, dir string, rewards map[string][]float64) {
	t.Helper()
	for task, rs := range rewards {
		for i, r := range rs {
			rec := map[string]any{
				"trial_id": task + "__t0" + string(rune('0'+i)),
				"task_id":  task,
				"reward":   r,
			}
			sub := filepath.Join(dir, task+"__t0"+string(rune('0'+i)))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			buf, _ := json.Marshal(rec)
			if err := os.WriteFile(filepath.Join(sub, "trial.json"), append(buf, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestBenchDiffCommand(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	out := t.TempDir()
	writeCLIRun(t, dirA, map[string][]float64{"ta": {0, 0}, "tb": {1, 1}})
	writeCLIRun(t, dirB, map[string][]float64{"ta": {1, 1}, "tb": {0, 0}})
	if code := run([]string{"bench", "diff", dirA, dirB, "--no-triage", "--out", out, "--reps", "500"}); code != ExitOK {
		t.Fatalf("bench diff exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(out, "diff.json")); err != nil {
		t.Fatalf("diff.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "diff.md")); err != nil {
		t.Fatalf("diff.md missing: %v", err)
	}
	if code := run([]string{"bench", "diff", dirA}); code == ExitOK {
		t.Fatal("bench diff without two runs should fail")
	}
}

func TestBenchInterimCommand(t *testing.T) {
	runDir := t.TempDir()
	refDir := t.TempDir()
	writeCLIRun(t, runDir, map[string][]float64{"ta": {1, 0}})
	writeCLIRun(t, refDir, map[string][]float64{"ta": {1, 0}, "tb": {1}})
	if code := run([]string{"bench", "interim", runDir, "--against", refDir, "--out", t.TempDir(), "--reps", "500"}); code != ExitOK {
		t.Fatalf("bench interim exit = %d", code)
	}
	base := filepath.Join(t.TempDir(), "baseline.json")
	doc := map[string]any{"baseline_rate": map[string]float64{"ta": 0.5}}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(base, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"bench", "interim", runDir, "--against", base, "--out", t.TempDir(), "--reps", "500"}); code != ExitOK {
		t.Fatalf("bench interim baseline exit = %d", code)
	}
	if code := run([]string{"bench", "interim", runDir}); code == ExitOK {
		t.Fatal("bench interim without --against should fail")
	}
}
