package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// SmokeTask is one cheap local task owned by this repository (no
// Harbor, no Docker required for the scaffold): prompt plus a checker
// over the produced directory.
type SmokeTask struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
}

// SmokeResult is one scaffold run outcome.
type SmokeResult struct {
	Name     string    `json:"name"`
	Passed   bool      `json:"passed"`
	Detail   string    `json:"detail"`
	At       time.Time `json:"at"`
	ATIFPath string    `json:"atif_path"`
}

// DefaultSmokeTasks returns the repository-owned smoke tasks.
func DefaultSmokeTasks() []SmokeTask {
	return []SmokeTask{
		{Name: "write-hello", Prompt: "Write hello.txt with exactly 'hello'. Verify by reading it back."},
		{Name: "failing-check", Prompt: "List the current directory and record its entries."},
	}
}

// SmokeRunner executes smoke tasks locally: it creates an isolated
// directory per task, writes an incremental ATIF trajectory and a
// metrics file, and runs a fake tool-call sequence so installation,
// profile hashing, ATIF and metrics are verified before spending on a
// real trial (Section 23.2: eval smoke).
type SmokeRunner struct {
	Dir     string
	Profile EvalProfileConfig
}

// Run executes every task and returns per-task results.
func (r SmokeRunner) Run(tasks []SmokeTask) ([]SmokeResult, error) {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("eval: mkdir smoke: %w", err)
	}
	var out []SmokeResult
	for _, t := range tasks {
		res, err := r.runOne(t)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

func (r SmokeRunner) runOne(t SmokeTask) (SmokeResult, error) {
	taskDir := filepath.Join(r.Dir, t.Name)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return SmokeResult{}, fmt.Errorf("eval: mkdir smoke task: %w", err)
	}
	meta := telemetry.ATIFMetadata{
		TaskID: t.Name, ExcaliburCommit: "smoke",
		Profile: r.Profile.Version, ProfileHash: r.Profile.Hash(),
		Model: r.Profile.Model, Provider: "smoke",
		StartedAt: time.Now().UTC(),
	}
	w, err := telemetry.NewWriter(taskDir, telemetry.ATIFv18, meta)
	if err != nil {
		return SmokeResult{}, err
	}
	if err := w.Append(telemetry.ATIFStep{Role: "user", Content: t.Prompt}); err != nil {
		return SmokeResult{}, err
	}
	if err := w.Append(telemetry.ATIFStep{Role: "assistant", Content: "done",
		ToolCalls: []telemetry.ATIFToolCall{{ID: "call_1", Name: "exec", Arguments: "{}"}},
	}); err != nil {
		return SmokeResult{}, err
	}
	if err := w.Append(telemetry.ATIFStep{Role: "tool",
		ToolResult: &telemetry.ATIFToolResult{CallID: "call_1", Name: "exec", Output: "ok"}}); err != nil {
		return SmokeResult{}, err
	}
	if err := telemetry.ValidateFile(w.Path()); err != nil {
		return SmokeResult{Name: t.Name, Passed: false, Detail: err.Error(), At: time.Now().UTC()}, nil
	}
	m := telemetry.NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "smoke")
	m.SetModel("smoke", "smoke")
	m.Finish("completed")
	snap := m.Snapshot()
	if err := telemetry.WriteMetricsFile(taskDir, snap); err != nil {
		return SmokeResult{}, err
	}
	// Every smoke task also records a trial.json so `eval report`
	// aggregates smoke trials exactly like Harbor trials (Section
	// 23.2: smoke first, report with the 23.7 statistics).
	trial := TrialFromSnapshot(snap, t.Name, "smoke", "smoke", 1, false, "")
	if err := WriteTrial(taskDir, trial); err != nil {
		return SmokeResult{}, err
	}
	return SmokeResult{Name: t.Name, Passed: true, Detail: "atif+metrics valid", At: time.Now().UTC(), ATIFPath: w.Path()}, nil
}
