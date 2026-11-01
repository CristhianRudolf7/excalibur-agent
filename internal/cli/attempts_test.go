package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/attempts"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestAttemptsDisabledByDefault(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "")
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "do work", LogsDir: t.TempDir(), ProjectDir: proj,
		Work: func(ctx context.Context, run *ExecRun) error {
			if run.ProjectDir == "" {
				return fmt.Errorf("work needs ProjectDir")
			}
			return os.WriteFile(filepath.Join(run.ProjectDir, "out.txt"), []byte("ok"), 0o644)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if _, err := os.Stat(filepath.Join(out.Dir, "attempts.json")); !os.IsNotExist(err) {
		t.Fatal("single attempt must not write attempts.json")
	}
}

func TestAttemptsFanOutSelectsCompletedAndAppliesWinner(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "2")
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	work := func(ctx context.Context, run *ExecRun) error {
		// Deterministic per-attempt behavior from the isolated log
		// dir name (attempt-0 fails, attempt-1 succeeds).
		if strings.Contains(run.Dir, "attempt-0") {
			return fmt.Errorf("attempt 0 always fails")
		}
		if err := os.WriteFile(filepath.Join(run.ProjectDir, "winner.txt"), []byte("winner"), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(run.ProjectDir, "base.txt"), []byte("base-improved"), 0o644)
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "improve base", LogsDir: t.TempDir(), ProjectDir: proj, Work: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("outer state = %q, want completed (winner)", out.State)
	}
	raw, err := os.ReadFile(filepath.Join(proj, "winner.txt"))
	if err != nil || string(raw) != "winner" {
		t.Fatalf("winner.txt = %q, %v (must be applied)", raw, err)
	}
	raw, err = os.ReadFile(filepath.Join(proj, "base.txt"))
	if err != nil || string(raw) != "base-improved" {
		t.Fatalf("base.txt = %q, %v (winner overlay must apply)", raw, err)
	}
	recRaw, err := os.ReadFile(filepath.Join(out.Dir, "attempts.json"))
	if err != nil {
		t.Fatalf("attempts.json missing: %v", err)
	}
	var rec attempts.Record
	if err := json.Unmarshal(recRaw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.N != 2 || len(rec.Attempts) != 2 {
		t.Fatalf("record = %+v, want 2 attempts", rec)
	}
	if rec.Winner != 1 {
		t.Fatalf("winner = %d, want 1 (the completed attempt)", rec.Winner)
	}
	foundFailed := false
	for _, a := range rec.Attempts {
		if a.State == string(types.RunFailed) {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Fatal("record must keep the failed attempt too")
	}
}

func TestAttemptsInvalidEnvStaysSingle(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "bogus")
	proj := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: t.TempDir(), ProjectDir: proj,
		Work: func(ctx context.Context, run *ExecRun) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	if _, err := os.Stat(filepath.Join(out.Dir, "attempts.json")); !os.IsNotExist(err) {
		t.Fatal("bogus env must stay single (no attempts.json)")
	}
}

func TestAttemptsEvalRecordsPerAttemptOutcomes(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "2")
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "x", LogsDir: logs, Profile: ProfileEval, ProjectDir: proj,
		Work: func(ctx context.Context, run *ExecRun) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	for _, p := range []string{
		filepath.Join(logs, "excalibur", "attempts.json"),
		filepath.Join(logs, "attempts.json"),
	} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		var rec attempts.Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatal(err)
		}
		if rec.N != 2 || len(rec.Attempts) != 2 {
			t.Fatalf("%s = %+v, want 2 attempts", p, rec)
		}
	}
	// Per-attempt isolated logs keep their own metrics for the audit.
	if _, err := os.Stat(filepath.Join(logs, "attempts", "attempt-0", "metrics.json")); err != nil {
		t.Fatalf("attempt metrics missing: %v", err)
	}
}

func TestAttemptsBudgetSplit(t *testing.T) {
	per := attempts.DivideSteps(25, 3)
	if per != 9 {
		t.Fatalf("per-step = %d, want 9", per)
	}
	b := 9.0
	if got := attempts.DivideBudget(&b, 3); got == nil || *got != 3.0 {
		t.Fatalf("per-budget = %v", got)
	}
}
