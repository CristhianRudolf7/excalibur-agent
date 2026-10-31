package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEvalSmokeCommand(t *testing.T) {
	dir := t.TempDir()
	if code := run([]string{"eval", "smoke", "--dir", dir}); code != ExitOK {
		t.Fatalf("eval smoke exit = %d", code)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("smoke wrote no trials: %v %v", entries, err)
	}
}

func TestEvalReportCommand(t *testing.T) {
	dir := t.TempDir()
	trials := filepath.Join(dir, "trials")
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(trials, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{"trial_id": "a1", "task_id": "task-a", "reward": 1.0}
	buf, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(trials, "a1.json"), append(buf, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"eval", "report", "--out", out, "--k", "1", trials}); code != ExitOK {
		t.Fatalf("eval report exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(out, "report.json")); err != nil {
		t.Fatalf("report.json missing: %v", err)
	}
	if code := run([]string{"eval", "report"}); code == ExitOK {
		t.Fatal("eval report without dir should fail")
	}
}
