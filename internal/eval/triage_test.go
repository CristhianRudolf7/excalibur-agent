package eval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// triageFixture builds one trial directory mimicking the recorded
// Harbor shape: result.json, verifier/reward.txt, agent records and
// the instruction in agent/trajectory.json.
type triageFixture struct {
	task        string
	trial       string
	reward      string
	exception   string
	instruction string
	finalState  string
	finalReason string
	tools       []ToolCall
	loops       []LoopSignal
	gates       []GateAttempt
	modelCalls  int
}

func writeTriageTrial(t *testing.T, parent string, f triageFixture) string {
	t.Helper()
	dir := filepath.Join(parent, f.trial)
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "verifier"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"task_name":  f.task,
		"trial_name": f.trial,
		"verifier_result": map[string]any{
			"rewards": map[string]any{"reward": 0.0},
		},
		"config": map[string]any{
			"agent": map[string]any{"model_name": "openai/gpt-6-luna"},
		},
	}
	if f.reward == "1" {
		doc["verifier_result"] = map[string]any{
			"rewards": map[string]any{"reward": 1.0},
		}
	}
	if f.exception != "" {
		doc["exception_info"] = map[string]any{"type": "TimeoutError", "message": f.exception}
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, "result.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "verifier", "reward.txt"), []byte(f.reward+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	traj, _ := json.Marshal(map[string]any{
		"steps": []map[string]any{{"role": "user", "content": f.instruction}},
	})
	if err := os.WriteFile(filepath.Join(dir, "agent", "trajectory.json"), traj, 0o644); err != nil {
		t.Fatal(err)
	}
	final, _ := json.Marshal(map[string]any{
		"state": f.finalState, "reason": f.finalReason, "exit_code": 0,
	})
	if err := os.WriteFile(filepath.Join(exDir, "final.json"), final, 0o644); err != nil {
		t.Fatal(err)
	}
	writeTriageJSONL(t, filepath.Join(exDir, "tool_calls.jsonl"), f.tools)
	var models []ModelCall
	for i := 0; i < f.modelCalls; i++ {
		models = append(models, ModelCall{Seq: i + 1, Role: "executor"})
	}
	writeTriageJSONL(t, filepath.Join(exDir, "model_calls.jsonl"), models)
	writeTriageJSONL(t, filepath.Join(exDir, "loops.jsonl"), f.loops)
	writeTriageJSONL(t, filepath.Join(exDir, "gate_attempts.jsonl"), f.gates)
	return dir
}

func writeTriageJSONL(t *testing.T, path string, rows any) {
	t.Helper()
	var list []any
	switch v := rows.(type) {
	case []ToolCall:
		for _, r := range v {
			list = append(list, r)
		}
	case []ModelCall:
		for _, r := range v {
			list = append(list, r)
		}
	case []LoopSignal:
		for _, r := range v {
			list = append(list, r)
		}
	case []GateAttempt:
		for _, r := range v {
			list = append(list, r)
		}
	default:
		t.Fatalf("unsupported rows %T", rows)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range list {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// triageTool builds one exec tool call with inline output only (no
// blob), as the classifier reads StdoutInline directly.
func triageTool(seq int, command string, exit int, isError bool, output string) ToolCall {
	args, _ := json.Marshal(map[string]any{"command": command})
	return ToolCall{
		Seq: seq, Tool: "exec", Arguments: string(args),
		ExitCode: exit, IsError: isError, StdoutInline: output,
	}
}

func triageRunDeterministic(t *testing.T, runDir string) ([]TriageResult, TriageStats) {
	t.Helper()
	t.Setenv("EXCALIBUR_TRIAGE_MODEL", "1")
	results, stats, err := TriageRun(context.Background(), runDir, TriageOptions{NoModel: true})
	if err != nil {
		t.Fatal(err)
	}
	return results, stats
}

func TestTriageTimeout(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/timeout", trial: "timeout__A", reward: "0",
		exception:   "agent timed out after 1800s",
		instruction: "Do the thing with the data.",
		finalState:  "paused", finalReason: "timeout",
		tools:      []ToolCall{triageTool(1, "sleep 60", 0, false, "session: sess-1 alive: false")},
		modelCalls: 3,
	})
	results, stats := triageRunDeterministic(t, run)
	if stats.Failed != 1 {
		t.Fatalf("failed = %d, want 1", stats.Failed)
	}
	if results[0].Cause != TriageTimeout {
		t.Fatalf("cause = %q, want timeout", results[0].Cause)
	}
	if results[0].DecidedBy != "deterministic" {
		t.Fatalf("decided_by = %q, want deterministic", results[0].DecidedBy)
	}
}

func TestTriageEnvMissing(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/env", trial: "env__A", reward: "0",
		instruction: "Do the thing with the data.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "which foo", 0, false, "session: sess-1 alive: false\n/app"),
			triageTool(2, "Rscript -e 'x <- 1'", 127, true, "session: sess-2 alive: false\n/usr/bin/bash: line 1: Rscript: command not found"),
			triageTool(3, "Rscript -e 'y <- 2'", 127, true, "session: sess-3 alive: false\nRscript: command not found"),
		},
		modelCalls: 4,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageEnvMissing {
		t.Fatalf("cause = %q, want environment/tooling-missing", results[0].Cause)
	}
	if results[0].FailStep != 3 {
		t.Fatalf("fail_step = %d, want 3", results[0].FailStep)
	}
}

// TestTriageEnvMissingExitZero pins the exit-code-independent rule:
// executor sessions can report exit 0 while the command body clearly
// failed (seen in real records: a Python traceback ending in
// FileNotFoundError with exit_code 0).
func TestTriageEnvMissingExitZero(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/env0", trial: "env0__A", reward: "0",
		instruction: "Do the thing with the data.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls /app", 0, false, "session: sess-1 alive: false\ndata"),
			triageTool(2, "python3 job.py", 0, false, "session: sess-2 alive: false\nTraceback (most recent call last):\nFileNotFoundError: [Errno 2] No such file or directory: 'main.db-wal'"),
		},
		modelCalls: 3,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageEnvMissing {
		t.Fatalf("cause = %q, want environment/tooling-missing", results[0].Cause)
	}
}

func TestTriageSkipsNoReward(t *testing.T) {
	run := t.TempDir()
	dir := filepath.Join(run, "noreward__A")
	exDir := filepath.Join(dir, "agent", "excalibur")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No verifier/reward.txt and a null verifier result: the verifier
	// never scored this trial.
	raw, _ := json.Marshal(map[string]any{"task_name": "t/n", "trial_name": "noreward__A"})
	if err := os.WriteFile(filepath.Join(dir, "result.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	writeTriageTrial(t, run, triageFixture{
		task: "t/n", trial: "ok__B", reward: "1",
		instruction: "x", finalState: "completed",
		tools: []ToolCall{triageTool(1, "a", 0, false, "o")},
	})
	results, stats, err := TriageRun(context.Background(), run, TriageOptions{NoModel: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 || stats.Passed != 1 || stats.Failed != 0 || stats.Skipped != 1 {
		t.Fatalf("stats = %+v, want total=2 passed=1 failed=0 skipped=1", stats)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want no rows for skipped trials", results)
	}
}

func TestTriageToolMisuse(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/misuse", trial: "misuse__A", reward: "0",
		instruction: "Do the thing with the data.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "pwd", 1, true, "unknown target \"container\" (remote nodes land in H13)"),
			triageTool(2, "pwd", 1, true, "unknown target \"H13\" (remote nodes land in H13)"),
			triageTool(3, "pwd", 1, true, "unknown target \"container\" (remote nodes land in H13)"),
			triageTool(4, "pwd", 1, true, "unknown target \"container\" (remote nodes land in H13)"),
		},
		modelCalls: 5,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageToolMisuse {
		t.Fatalf("cause = %q, want tool-misuse/harness-friction", results[0].Cause)
	}
}

func TestTriageLoop(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/loop", trial: "loop__A", reward: "0",
		instruction: "Do the thing with the data.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls /app", 0, false, "session: sess-1 alive: false\na.txt"),
			triageTool(2, "ls /app", 0, false, "session: sess-2 alive: false\na.txt"),
			triageTool(3, "ls /app", 0, false, "session: sess-3 alive: false\na.txt"),
			triageTool(4, "ls /app", 0, false, "session: sess-4 alive: false\na.txt"),
		},
		loops:      []LoopSignal{{Stage: "detect", Kind: "repeated-command"}, {Stage: "detect", Kind: "repeated-command"}},
		modelCalls: 5,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageLoop {
		t.Fatalf("cause = %q, want loop/no-progress", results[0].Cause)
	}
}

func TestTriageLoopRepeatedCommands(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/repeat", trial: "repeat__A", reward: "0",
		instruction: "Do the thing with the data.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls /app", 0, false, "session: sess-1 alive: false\na.txt"),
			triageTool(2, "ls /app", 0, false, "session: sess-2 alive: false\na.txt"),
			triageTool(3, "ls /app", 0, false, "session: sess-3 alive: false\na.txt"),
			triageTool(4, "ls /app", 0, false, "session: sess-4 alive: false\na.txt"),
			triageTool(5, "ls /app", 0, false, "session: sess-5 alive: false\na.txt"),
		},
		modelCalls: 6,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageLoop {
		t.Fatalf("cause = %q, want loop/no-progress", results[0].Cause)
	}
}

func TestTriageIncomplete(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/shallow", trial: "shallow__A", reward: "0",
		instruction: "Do the thing with the data.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls", 0, false, "session: sess-1 alive: false\na"),
			triageTool(2, "pwd", 0, false, "session: sess-2 alive: false\n/app"),
		},
		modelCalls: 2,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageIncomplete {
		t.Fatalf("cause = %q, want incomplete-stopped-early", results[0].Cause)
	}
}

func TestTriageWrongDone(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/wrong", trial: "wrong__A", reward: "0",
		instruction: "Do the thing with the data and report back.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls /app", 0, false, "session: sess-1 alive: false\ndata.csv"),
			triageTool(2, "wc -l /app/data.csv", 0, false, "session: sess-2 alive: false\n42"),
			triageTool(3, "head /app/data.csv", 0, false, "session: sess-3 alive: false\na,b"),
			triageTool(4, "cat notes.txt", 0, false, "session: sess-4 alive: false\ndone"),
			triageTool(5, "echo ok", 0, false, "session: sess-5 alive: false\nok"),
			triageTool(6, "echo finished", 0, false, "session: sess-6 alive: false\nfinished"),
		},
		gates:      []GateAttempt{{Attempt: 1, Passed: true}},
		modelCalls: 7,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageWrongDone {
		t.Fatalf("cause = %q, want wrong-solution-declared-done", results[0].Cause)
	}
	if results[0].DecidedBy != "deterministic" {
		t.Fatalf("decided_by = %q, want deterministic with NoModel", results[0].DecidedBy)
	}
}

func TestTriageMisunderstood(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/mis", trial: "mis__A", reward: "0",
		instruction: "Write your R implementation to /app/ars.R and include a test function.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls /app", 0, false, "session: sess-1 alive: false\ndata.csv"),
			triageTool(2, "cat /app/data.csv", 0, false, "session: sess-2 alive: false\na,b"),
			triageTool(3, "echo done", 0, false, "session: sess-3 alive: false\ndone"),
			triageTool(4, "ls /tmp", 0, false, "session: sess-4 alive: false\nx"),
		},
		modelCalls: 5,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageMisunderstood {
		t.Fatalf("cause = %q, want misunderstood-instruction", results[0].Cause)
	}
}

func TestTriageKnowledgeGap(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/kg", trial: "kg__A", reward: "0",
		instruction: "Fix the python script in fix.py so it parses the input.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "cat fix.py", 0, false, "session: sess-1 alive: false\nprint(x)"),
			triageTool(2, "python fix.py", 1, true, "session: sess-2 alive: false\nTraceback (most recent call last):\nNameError: name 'x' is not defined"),
			triageTool(3, "python fix.py", 1, true, "session: sess-3 alive: false\nTraceback (most recent call last):\nNameError: name 'x' is not defined"),
			triageTool(4, "python fix.py", 1, true, "session: sess-4 alive: false\nTraceback (most recent call last):\nTypeError: bad type"),
		},
		modelCalls: 5,
	})
	results, _ := triageRunDeterministic(t, run)
	if results[0].Cause != TriageKnowledgeGap {
		t.Fatalf("cause = %q, want knowledge-gap", results[0].Cause)
	}
	if results[0].Domain != "python" {
		t.Fatalf("domain = %q, want python", results[0].Domain)
	}
}

func TestTriageSkipsPasses(t *testing.T) {
	run := t.TempDir()
	writeTriageTrial(t, run, triageFixture{
		task: "t/a", trial: "a__pass", reward: "1",
		instruction: "Do the thing.", finalState: "completed",
		tools:      []ToolCall{triageTool(1, "echo ok", 0, false, "session: sess-1 alive: false\nok")},
		modelCalls: 1,
	})
	writeTriageTrial(t, run, triageFixture{
		task: "t/b", trial: "b__fail", reward: "0",
		instruction: "Do the thing.", finalState: "completed",
		tools:      []ToolCall{triageTool(1, "x", 1, true, "unknown target \"container\" (remote nodes land in H13)")},
		modelCalls: 1,
	})
	results, stats, err := TriageRun(context.Background(), runDirOf(run), TriageOptions{NoModel: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 || stats.Passed != 1 || stats.Failed != 1 {
		t.Fatalf("stats = %+v, want total=2 passed=1 failed=1", stats)
	}
	if len(results) != 1 || results[0].Trial != "b__fail" {
		t.Fatalf("results = %+v, want only the failed trial", results)
	}
}

func runDirOf(dir string) string { return dir }

// TestTriageNeverReadsVerifier plants decoy secrets in the verifier's
// test outputs and reference files and asserts none leak into the
// triage output: only reward.txt (pass/fail) may be consulted.
func TestTriageNeverReadsVerifier(t *testing.T) {
	run := t.TempDir()
	dir := writeTriageTrial(t, run, triageFixture{
		task: "t/secret", trial: "secret__A", reward: "0",
		instruction: "Do the thing with the data.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls", 0, false, "session: sess-1 alive: false\na"),
			triageTool(2, "cat a", 0, false, "session: sess-2 alive: false\nx"),
		},
		modelCalls: 2,
	})
	marker := "VERIFIER_SECRET_MARKER_9f8c"
	for _, name := range []string{"test-stdout.txt", "test-stderr.txt", "ctrf.json", "solution.patch", "tests.json"} {
		if err := os.WriteFile(filepath.Join(dir, "verifier", name), []byte("hidden "+marker), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	results, _, err := TriageRun(context.Background(), run, TriageOptions{NoModel: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), marker) {
			t.Fatalf("triage output leaked verifier content: %s", raw)
		}
	}
	md := RenderTriageMarkdown(results, TriageStats{Total: 1, Failed: 1}, run)
	if strings.Contains(md, marker) {
		t.Fatalf("triage markdown leaked verifier content")
	}
}

func TestTriageMarkdown(t *testing.T) {
	results := []TriageResult{
		{Task: "t/a", Trial: "a__1", Cause: TriageTimeout, Evidence: "timed out. Quote: \"timeout\"", FailStep: 5, DecidedBy: "deterministic"},
		{Task: "t/b", Trial: "b__1", Cause: TriageKnowledgeGap, Domain: "R", Evidence: "fails. Quote: \"Error in\"", FailStep: 3, DecidedBy: "deterministic"},
		{Task: "t/c", Trial: "c__1", Cause: TriageKnowledgeGap, Domain: "R", Evidence: "fails. Quote: \"Error in\"", FailStep: 4, DecidedBy: "model"},
	}
	md := RenderTriageMarkdown(results, TriageStats{Total: 3, Failed: 3}, "/run")
	for _, want := range []string{
		"| " + TriageTimeout + " | 1 |",
		"| " + TriageKnowledgeGap + " | 2 |",
		"| R | 2 |",
		"a__1", "b__1", "c__1",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
	if err := WriteTriageJSONL(filepath.Join(t.TempDir(), "triage.jsonl"), results); err != nil {
		t.Fatal(err)
	}
}

// judgeServer serves one canned verdict, optionally failing the first
// request with 429 to exercise the fallback-key retry.
func judgeServer(t *testing.T, verdict string, failFirst bool, seenAuth *atomic.Value) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["reasoning_effort"] != "low" {
			t.Errorf("reasoning_effort = %v, want low", body["reasoning_effort"])
		}
		seenAuth.Store(r.Header.Get("Authorization"))
		if failFirst && calls.Add(1) == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": verdict}}},
		})
		_, _ = w.Write(resp)
	}))
}

func writeCreds(t *testing.T, baseURL, key, fallback, model string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{
		"base_url": baseURL, "api_key": key,
		"fallback_api_key": fallback, "model": model,
	})
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ambiguousTrial(t *testing.T, run, trial string) {
	t.Helper()
	writeTriageTrial(t, run, triageFixture{
		task: "t/amb", trial: trial, reward: "0",
		instruction: "Do the thing with the data and report back.",
		finalState:  "completed",
		tools: []ToolCall{
			triageTool(1, "ls /app", 0, false, "session: sess-1 alive: false\ndata.csv"),
			triageTool(2, "wc -l /app/data.csv", 0, false, "session: sess-2 alive: false\n42"),
			triageTool(3, "head /app/data.csv", 0, false, "session: sess-3 alive: false\na,b"),
			triageTool(4, "cat notes.txt", 0, false, "session: sess-4 alive: false\ndone"),
			triageTool(5, "echo ok", 0, false, "session: sess-5 alive: false\nok"),
			triageTool(6, "echo finished", 0, false, "session: sess-6 alive: false\nfinished"),
		},
		gates:      []GateAttempt{{Attempt: 1, Passed: true}},
		modelCalls: 7,
	})
}

func TestTriageModelFallback(t *testing.T) {
	t.Setenv("EXCALIBUR_TRIAGE_MODEL", "1")
	run := t.TempDir()
	ambiguousTrial(t, run, "amb__A")
	var seen atomic.Value
	srv := judgeServer(t, `{"cause":"incomplete-stopped-early","domain":"","evidence":"Stopped while checks were pending. Quote: \"finished\"","fail_step":6}`, false, &seen)
	defer srv.Close()
	creds := writeCreds(t, srv.URL, "test-key", "", "judge-model")
	results, _, err := TriageRun(context.Background(), run, TriageOptions{CredentialsFile: creds})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Cause != TriageIncomplete || results[0].DecidedBy != "model" {
		t.Fatalf("got %+v, want model-decided incomplete", results[0])
	}
	if seen.Load() != "Bearer test-key" {
		t.Fatalf("auth = %v, want Bearer test-key", seen.Load())
	}
}

func TestTriageModelFallbackKey(t *testing.T) {
	t.Setenv("EXCALIBUR_TRIAGE_MODEL", "1")
	run := t.TempDir()
	ambiguousTrial(t, run, "amb__B")
	var seen atomic.Value
	srv := judgeServer(t, `{"cause":"loop/no-progress","domain":"","evidence":"Repeated listing. Quote: \"ls\"","fail_step":6}`, true, &seen)
	defer srv.Close()
	creds := writeCreds(t, srv.URL, "primary", "fallback-key", "judge-model")
	results, _, err := TriageRun(context.Background(), run, TriageOptions{CredentialsFile: creds})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Cause != TriageLoop || results[0].DecidedBy != "model" {
		t.Fatalf("got %+v, want model-decided loop after fallback", results[0])
	}
	if seen.Load() != "Bearer fallback-key" {
		t.Fatalf("auth = %v, want Bearer fallback-key", seen.Load())
	}
}

func TestTriageModelRejectsUnknownCause(t *testing.T) {
	t.Setenv("EXCALIBUR_TRIAGE_MODEL", "1")
	run := t.TempDir()
	ambiguousTrial(t, run, "amb__C")
	var seen atomic.Value
	srv := judgeServer(t, `{"cause":"made-up","domain":"","evidence":"x","fail_step":1}`, false, &seen)
	defer srv.Close()
	creds := writeCreds(t, srv.URL, "k", "", "judge-model")
	results, _, err := TriageRun(context.Background(), run, TriageOptions{CredentialsFile: creds})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].DecidedBy != "deterministic" || results[0].Cause != TriageWrongDone {
		t.Fatalf("got %+v, want deterministic fallback on unknown cause", results[0])
	}
}

func TestTriageJudgeErrorFallsBack(t *testing.T) {
	t.Setenv("EXCALIBUR_TRIAGE_MODEL", "1")
	run := t.TempDir()
	ambiguousTrial(t, run, "amb__E")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()
	creds := writeCreds(t, srv.URL, "k", "", "judge-model")
	results, stats, err := TriageRun(context.Background(), run, TriageOptions{CredentialsFile: creds})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].DecidedBy != "deterministic" || results[0].Cause != TriageWrongDone {
		t.Fatalf("got %+v, want deterministic fallback on judge error", results[0])
	}
	if stats.JudgeCalls != 1 || stats.JudgeErrors != 1 {
		t.Fatalf("stats = %+v, want 1 call and 1 error", stats)
	}
}

func TestTriageModelSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_TRIAGE_MODEL", "0")
	run := t.TempDir()
	ambiguousTrial(t, run, "amb__D")
	// No credentials and the switch off: must stay deterministic and
	// never attempt a model call.
	results, _, err := TriageRun(context.Background(), run, TriageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].DecidedBy != "deterministic" {
		t.Fatalf("decided_by = %q, want deterministic with switch off", results[0].DecidedBy)
	}
}

func TestFindTrialDirs(t *testing.T) {
	// Single trial directory.
	single := t.TempDir()
	writeTriageTrial(t, single, triageFixture{
		task: "t/s", trial: "s__A", reward: "0",
		instruction: "x", finalState: "completed",
		tools: []ToolCall{triageTool(1, "a", 0, false, "o")},
	})
	dirs, err := FindTrialDirs(single)
	if err != nil || len(dirs) != 1 {
		t.Fatalf("single: dirs=%v err=%v", dirs, err)
	}
	// Job directory with trials.
	job := t.TempDir()
	writeTriageTrial(t, job, triageFixture{
		task: "t/j", trial: "j__A", reward: "0",
		instruction: "x", finalState: "completed",
		tools: []ToolCall{triageTool(1, "a", 0, false, "o")},
	})
	writeTriageTrial(t, job, triageFixture{
		task: "t/j", trial: "j__B", reward: "1",
		instruction: "x", finalState: "completed",
		tools: []ToolCall{triageTool(1, "a", 0, false, "o")},
	})
	dirs, err = FindTrialDirs(job)
	if err != nil || len(dirs) != 2 {
		t.Fatalf("job: dirs=%v err=%v", dirs, err)
	}
	// A job-level result.json must not shadow the trials beneath it.
	if err := os.WriteFile(filepath.Join(job, "result.json"), []byte(`{"job":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs, err = FindTrialDirs(job)
	if err != nil || len(dirs) != 2 {
		t.Fatalf("job with job-level result.json: dirs=%v err=%v", dirs, err)
	}
	// Run root with nested job directories.
	root := t.TempDir()
	nested := filepath.Join(root, "2026-10-31__00-00-00")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTriageTrial(t, nested, triageFixture{
		task: "t/n", trial: "n__A", reward: "0",
		instruction: "x", finalState: "completed",
		tools: []ToolCall{triageTool(1, "a", 0, false, "o")},
	})
	dirs, err = FindTrialDirs(root)
	if err != nil || len(dirs) != 1 {
		t.Fatalf("root: dirs=%v err=%v", dirs, err)
	}
	if _, err := FindTrialDirs(t.TempDir()); err == nil {
		t.Fatalf("empty dir should error")
	}
}

func TestTriageModelEnabled(t *testing.T) {
	for _, v := range []string{"", "1", "true", "yes", "on", "banana"} {
		t.Setenv("EXCALIBUR_TRIAGE_MODEL", v)
		if !TriageModelEnabled() {
			t.Fatalf("env %q should enable", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off"} {
		t.Setenv("EXCALIBUR_TRIAGE_MODEL", v)
		if TriageModelEnabled() {
			t.Fatalf("env %q should disable", v)
		}
	}
}
