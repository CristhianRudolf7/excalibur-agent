package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
)

func TestWallBudgetSourceRecords(t *testing.T) {
	for _, source := range []string{"default", "flag", "environment"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("EXCALIBUR_EVAL_WALL_SECS", "")
			t.Setenv(engine.TimeReserveExplicitOnlySwitch, "1")
			fake := providerfake.New("budget-records")
			fake.Responses = scriptedWriteThenDone(filepath.Join(t.TempDir(), "report.txt"), "report complete")
			opts := ExecOptions{Provider: fake, Request: "Create a report", ProjectDir: t.TempDir(), LogsDir: t.TempDir(), Profile: ProfileEval}
			want := "default"
			if source == "flag" {
				opts.TimeoutSecs = 120
				want = "explicit"
			}
			if source == "environment" {
				t.Setenv("EXCALIBUR_EVAL_WALL_SECS", "150")
				want = "explicit"
			}
			out, err := RunExec(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(out.Dir, "budgets.json"))
			if err != nil {
				t.Fatal(err)
			}
			var b EffectiveBudgets
			if err = json.Unmarshal(raw, &b); err != nil {
				t.Fatal(err)
			}
			if b.WallSource != want || b.HardWallSecs != engine.HardWallSeconds(b.WallSecs, want) {
				t.Fatalf("budgets: %s", raw)
			}
			raw, err = os.ReadFile(filepath.Join(out.Dir, "excalibur", "budget.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			modelRows := 0
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var row map[string]any
				if err = json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatal(err)
				}
				if row["note"] == "model_call" {
					modelRows++
				}
				if row["wall_source"] != want {
					t.Fatalf("missing source: %s", line)
				}
			}
			if modelRows == 0 {
				t.Fatal("missing model budget points")
			}
		})
	}
}

func TestAssumedWallContextAllowsCommandPastEstimate(t *testing.T) {
	t.Setenv(engine.TimeReserveExplicitOnlySwitch, "1")
	b := EffectiveBudgets{WallSecs: 1, WallSource: "default", HardWallSecs: engine.HardWallSeconds(1, "default")}
	ctx, cancel := b.wallContext(context.Background(), time.Now())
	defer cancel()
	command := exec.CommandContext(ctx, "sh", "-c", "printf 'building\\n'; sleep 1.2; printf 'finished\\n'")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "finished") {
		t.Fatalf("command stopped at estimate: %v %s", err, output)
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	// Explicit and rollback budgets retain the original bound, and an outer
	// deadline always takes precedence over the larger fallback safety cap.
	for _, source := range []string{"explicit", "default"} {
		t.Setenv(engine.TimeReserveExplicitOnlySwitch, "0")
		hard := engine.HardWallSeconds(1, source)
		if hard != 1 {
			t.Fatalf("rollback/explicit: %d", hard)
		}
	}
	t.Setenv(engine.TimeReserveExplicitOnlySwitch, "1")
	parent, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	child, closeChild := b.wallContext(parent, time.Now())
	defer closeChild()
	<-child.Done()
	if child.Err() != context.DeadlineExceeded {
		t.Fatal(child.Err())
	}
}
