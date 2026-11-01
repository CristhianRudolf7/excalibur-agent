package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
)

func TestInstructionDeadlineBudget(t *testing.T) {
	t.Setenv(engine.DeadlineSwitch, "on")
	for _, key := range engine.HarborTimeoutEnvs {
		t.Setenv(key, "")
	}
	for _, profile := range []ProfileName{ProfileDefault, ProfileEval} {
		b := EffectiveBudgetsFor(ExecOptions{Request: "You have 15 minutes to complete this task"}, profile)
		if b.WallSecs != 900 || b.WallSource != "instruction" {
			t.Fatal(b)
		}
	}
}

func TestTerminationFinalRecord(t *testing.T) {
	t.Setenv(engine.DeadlineSwitch, "on")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrTermination)
	logs := t.TempDir()
	out, err := RunExec(ctx, ExecOptions{Request: "work", ProjectDir: t.TempDir(), LogsDir: logs, Profile: ProfileEval})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "stopped_by_timeout" || out.Reason != "timeout" {
		t.Fatal(out)
	}
	// The existing finalization writes the matching final document.
	var found bool
	_ = filepath.WalkDir(logs, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == "final.json" {
			data, _ := os.ReadFile(path)
			var doc map[string]any
			if json.Unmarshal(data, &doc) == nil && doc["state"] == "stopped_by_timeout" {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatalf("missing timeout final in %s", logs)
	}
}

func TestDeadlineExpiryFinalRecord(t *testing.T) {
	t.Setenv(engine.DeadlineSwitch, "on")
	for _, key := range engine.HarborTimeoutEnvs {
		t.Setenv(key, "")
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "You have 1 second to complete this task", ProjectDir: t.TempDir(), LogsDir: t.TempDir(), Profile: ProfileEval,
		Work: func(ctx context.Context, _ *ExecRun) error { <-ctx.Done(); return ctx.Err() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "stopped_by_timeout" || out.Reason != "timeout" {
		t.Fatalf("deadline must never finalize as running or work failure: %+v", out)
	}
}
