package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// writeTrialDoc writes a minimal Harbor result.json into dir.
func writeTrialDoc(t *testing.T, dir, task, trial string, reward float64, exception string) {
	t.Helper()
	doc := map[string]any{
		"task_name":  task,
		"trial_name": trial,
		"agent_result": map[string]any{
			"n_input_tokens": 0, "n_cache_tokens": 0,
			"n_output_tokens": 0, "cost_usd": 0.0,
		},
		"verifier_result": map[string]any{"rewards": map[string]any{"reward": reward}},
		"agent_execution": map[string]any{
			"started_at":  "2026-10-31T19:05:01.763759Z",
			"finished_at": "2026-10-31T19:05:04.078358Z",
		},
	}
	if exception != "" {
		doc["exception_info"] = map[string]any{"type": "X", "message": exception}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSONL(t *testing.T, path string, rows []any) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range rows {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// harnessTrial builds a trial dir whose harness run failed before the
// model was reached (the pilot TLS shape): reward 0, no exception,
// failed final state, one errored model call, zero usage.
func harnessTrial(t *testing.T, dir string) {
	t.Helper()
	writeTrialDoc(t, dir, "terminal-bench/dna-insert", "dna-insert__X", 0, "")
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := telemetry.NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "v1")
	m.SetModel("openai", "gpt-6-luna")
	m.Finish("failed")
	snap := m.Snapshot()
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(exDir, "metrics.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(exDir, "model_calls.jsonl"), []any{
		ModelCall{Seq: 1, Role: "executor", Model: "gpt-6-luna",
			LatencySecs: 1.9, ProviderError: "transient", Retries: 2},
	})
	writeJSONL(t, filepath.Join(exDir, "gate_attempts.jsonl"), []any{
		GateAttempt{Attempt: 1, Passed: false, Note: "failed"},
	})
	writeJSONL(t, filepath.Join(exDir, "transitions.jsonl"), []any{
		Transition{From: "executing", To: "failed", Reason: "work"},
	})
	finalize := map[string]any{"state": "failed", "reason": "work", "exit_code": 5}
	fraw, _ := json.Marshal(finalize)
	if err := os.WriteFile(filepath.Join(exDir, "final.json"), fraw, 0o644); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"state": "failed", "reason": "work", "summary": "failed: model call: tls: failed to verify certificate"}
	rraw, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(dir, "agent", "result.json"), rraw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIndexTrialHarnessError pins defect w8-bench-fixes/3: a trial
// the harness never completed is harness_error (not verifier), and
// the index carries the per-trial records that exist (steps, model
// time from the errored call, retries).
func TestIndexTrialHarnessError(t *testing.T) {
	dir := t.TempDir()
	harnessTrial(t, dir)
	row, err := IndexTrial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if row.FailCategory != FailHarness {
		t.Fatalf("fail_category = %q, want %q", row.FailCategory, FailHarness)
	}
	if row.Reward != 0 {
		t.Fatalf("reward = %v, want 0", row.Reward)
	}
	if row.Steps != 1 || row.ModelCalls != 1 {
		t.Fatalf("steps/model_calls = %d/%d, want 1/1", row.Steps, row.ModelCalls)
	}
	if row.ModelRetries != 2 {
		t.Fatalf("model_retries = %d, want 2 (gateway attempts on failure)", row.ModelRetries)
	}
	if row.ModelSecs != 1.9 {
		t.Fatalf("model_seconds = %v, want 1.9 from the errored call", row.ModelSecs)
	}
	if row.WallSecs <= 0 {
		t.Fatalf("wall_seconds = %v, want the metrics wall time", row.WallSecs)
	}
	if row.InputTokens != 0 || row.OutputTokens != 0 {
		t.Fatalf("tokens = %d/%d, want honest zeros (model never reached)",
			row.InputTokens, row.OutputTokens)
	}
}

// verifierTrial builds a trial dir whose harness run completed but
// the verifier scored 0.
func verifierTrial(t *testing.T, dir string) {
	t.Helper()
	writeTrialDoc(t, dir, "terminal-bench/write-compressor", "write-compressor__Y", 0, "")
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := telemetry.NewTaskMetrics(types.NewTaskID(), types.NewRunID(), "v1")
	m.SetModel("openai", "gpt-6-luna")
	m.Finish("completed")
	snap := m.Snapshot()
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(exDir, "metrics.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(exDir, "model_calls.jsonl"), []any{
		ModelCall{Seq: 1, Role: "executor", Model: "gpt-6-luna",
			InputTokens: 1000, OutputTokens: 50, LatencySecs: 1.5},
		ModelCall{Seq: 2, Role: "executor", Model: "gpt-6-luna",
			InputTokens: 1100, CacheRead: 900, OutputTokens: 30, LatencySecs: 1.2, Retries: 1},
	})
	writeJSONL(t, filepath.Join(exDir, "tool_calls.jsonl"), []any{
		ToolCall{Seq: 1, Tool: "exec", DurationSec: 0.5},
		ToolCall{Seq: 2, Tool: "exec", DurationSec: 0.7},
		ToolCall{Seq: 3, Tool: "subagent", DurationSec: 2.0},
	})
	writeJSONL(t, filepath.Join(exDir, "loops.jsonl"), []any{
		LoopSignal{Stage: "observed", Kind: "no_progress", Detail: "count=1"},
		LoopSignal{Stage: "final", Kind: "none", Detail: "no loop"},
	})
	writeJSONL(t, filepath.Join(exDir, "transitions.jsonl"), []any{
		Transition{From: "created", To: "executing"},
		Transition{From: "executing", To: "completed"},
	})
	finalize := map[string]any{"state": "completed", "exit_code": 0}
	fraw, _ := json.Marshal(finalize)
	if err := os.WriteFile(filepath.Join(exDir, "final.json"), fraw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIndexTrialVerifierFailure pins the other half of defect
// w8-bench-fixes/3: a completed harness run with reward 0 is
// verifier, and every Appendix F field fills from the records.
func TestIndexTrialVerifierFailure(t *testing.T) {
	dir := t.TempDir()
	verifierTrial(t, dir)
	row, err := IndexTrial(dir)
	if err != nil {
		t.Fatal(err)
	}
	if row.FailCategory != FailVerifier {
		t.Fatalf("fail_category = %q, want %q", row.FailCategory, FailVerifier)
	}
	if row.InputTokens != 2100 || row.CacheRead != 900 || row.OutputTokens != 80 {
		t.Fatalf("tokens = %d/%d/%d, want 2100/900/80",
			row.InputTokens, row.CacheRead, row.OutputTokens)
	}
	if row.ModelSecs != 2.7 {
		t.Fatalf("model_seconds = %v, want 2.7", row.ModelSecs)
	}
	if row.ToolSecs != 3.2 {
		t.Fatalf("tool_seconds = %v, want 3.2", row.ToolSecs)
	}
	if row.Steps != 2 {
		t.Fatalf("steps = %d, want 2", row.Steps)
	}
	if row.ModelRetries != 1 {
		t.Fatalf("model_retries = %d, want 1", row.ModelRetries)
	}
	if row.ToolCallSum != 3 || row.ToolCalls["exec"] != 2 {
		t.Fatalf("tool calls = %d/%v, want 3/{exec:2}", row.ToolCallSum, row.ToolCalls)
	}
	if row.Subagents != 1 {
		t.Fatalf("subagents = %d, want 1", row.Subagents)
	}
	if row.LoopsCaught != 1 {
		t.Fatalf("loops = %d, want 1 (final marker excluded)", row.LoopsCaught)
	}
	if row.Interruptions != 0 {
		t.Fatalf("interruptions = %d, want 0", row.Interruptions)
	}
}

func TestIndexTrialCategories(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string
	}{
		{"pass has no category", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 1, "")
		}, ""},
		{"harbor timeout", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 0, "agent timeout after 900s")
		}, FailTimeout},
		{"harbor verifier timeout (0.25.0 keys)", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 0, "")
			raw, err := os.ReadFile(filepath.Join(dir, "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			doc["exception_info"] = map[string]any{
				"exception_type":    "VerifierTimeoutError",
				"exception_message": "Verifier execution timed out after 900.0 seconds",
			}
			out, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "result.json"), out, 0o644); err != nil {
				t.Fatal(err)
			}
		}, FailTimeout},
		{"harbor oom", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 0, "out of memory")
		}, FailOOM},
		{"missing records", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 0, "")
		}, FailHarness},
		{"paused timeout", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 0, "")
			exDir := filepath.Join(dir, "agent", "excalibur")
			if err := os.MkdirAll(exDir, 0o755); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"state": "paused", "reason": "timeout"})
			if err := os.WriteFile(filepath.Join(exDir, "final.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}, FailTimeout},
		{"exit 137 is oom", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 0, "")
			exDir := filepath.Join(dir, "agent", "excalibur")
			if err := os.MkdirAll(exDir, 0o755); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"state": "failed", "reason": "work", "exit_code": 137})
			if err := os.WriteFile(filepath.Join(exDir, "final.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}, FailOOM},
		{"blocked", func(t *testing.T, dir string) {
			writeTrialDoc(t, dir, "t", "t__1", 0, "")
			exDir := filepath.Join(dir, "agent", "excalibur")
			if err := os.MkdirAll(exDir, 0o755); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"state": "blocked", "reason": "missing_data"})
			if err := os.WriteFile(filepath.Join(exDir, "final.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}, FailBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			row, err := IndexTrial(dir)
			if err != nil {
				t.Fatal(err)
			}
			if row.FailCategory != tc.want {
				t.Fatalf("fail_category = %q, want %q", row.FailCategory, tc.want)
			}
		})
	}
}
