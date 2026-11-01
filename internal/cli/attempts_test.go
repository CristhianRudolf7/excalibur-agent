package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/attempts"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// attemptAwareFake is a stateless-per-workdir scripted provider for the
// attempts fan-out: it extracts the isolated workdir from the project
// context in the request and serves write -> read -> done per workdir.
// Sharing one instance across concurrent attempts stays isolated
// because counts are keyed by workdir. Each attempt needs 3 model
// calls (3 steps) to complete.
type attemptAwareFake struct {
	mu     sync.Mutex
	counts map[string]int
	calls  int
}

func (f *attemptAwareFake) Name() string { return "attempt-aware-fake" }
func (f *attemptAwareFake) Close() error { return nil }
func (f *attemptAwareFake) Stream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamEvent) error) error {
	return nil
}

func (f *attemptAwareFake) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	workdir := ""
	for _, m := range req.Messages {
		if i := strings.Index(m.Content, "project directory: "); i >= 0 {
			rest := m.Content[i+len("project directory: "):]
			if j := strings.Index(rest, "\n"); j >= 0 {
				workdir = strings.TrimSpace(rest[:j])
			} else {
				workdir = strings.TrimSpace(rest)
			}
			break
		}
	}
	f.mu.Lock()
	if f.counts == nil {
		f.counts = map[string]int{}
	}
	n := f.counts[workdir]
	f.counts[workdir] = n + 1
	f.calls++
	callSeq := f.calls
	f.mu.Unlock()
	target := filepath.Join(workdir, "winner.txt")
	if workdir == "" {
		target = filepath.Join(os.TempDir(), "attempt-fake-winner.txt")
	}
	switch n {
	case 0:
		args, _ := json.Marshal(map[string]string{"path": target, "content": "winner"})
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant,
			ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("c%dw", callSeq), Name: "write_file", Arguments: args}}}}, nil
	case 1:
		args, _ := json.Marshal(map[string]string{"path": target})
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant,
			ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("c%dr", callSeq), Name: "read_file", Arguments: args}}}}, nil
	default:
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "done"}}, nil
	}
}

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
	// Full-budget default: each attempt runs to its own verification.
	if got := attempts.StepsForAttempt(25, 3); got != 25 {
		t.Fatalf("full steps = %d, want 25", got)
	}
	if got := attempts.StepsForAttempt(0, 3); got != 25 {
		t.Fatalf("full default steps = %d, want 25", got)
	}
	fb := 9.0
	if got := attempts.BudgetForAttempt(&fb, 3); got == nil || *got != 9.0 {
		t.Fatalf("full budget = %v, want 9", got)
	}
	t.Setenv("EXCALIBUR_ATTEMPTS_FULL_BUDGET", "0")
	if got := attempts.StepsForAttempt(25, 3); got != 9 {
		t.Fatalf("legacy split steps = %d, want 9", got)
	}
}

// TestAttemptsFullBudgetCompletesWhereSplitWouldPause is the
// container-like end-to-end reproduction for the EXCALIBUR_ATTEMPTS=3
// paused-outcome bug: with the legacy ceil(parent/N) split, parent
// MaxSteps=6 gives 2 steps per attempt, so a 3-step write/read/done
// script pauses every attempt and the outer run ends paused with the
// bare summary "paused [best of 3 attempts]". With the full-budget
// default (each attempt gets 6 steps) the same run completes, the
// winner's file is applied, and the outer excalibur/ records contain
// every attempt's model/tool calls tagged by attempt.
func TestAttemptsFullBudgetCompletesWhereSplitWouldPause(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "3")
	// Default ON: full budget. The legacy split is covered by
	// TestAttemptsBudgetSplit with the switch off.
	t.Setenv("EXCALIBUR_ATTEMPTS_FULL_BUDGET", "")
	t.Setenv("EXCALIBUR_ATTEMPTS_MERGE_RECORDS", "")
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := t.TempDir()
	fake := &attemptAwareFake{}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write winner", LogsDir: logs, Profile: ProfileEval,
		ProjectDir: proj, Provider: fake, MaxSteps: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("outer state = %q, want completed (full budget runs to verification)", out.State)
	}
	if !strings.Contains(out.Summary, "best of 3 attempts") {
		t.Fatalf("summary = %q, want best-of-3 marker", out.Summary)
	}
	// Winner applied to the real directory.
	raw, err := os.ReadFile(filepath.Join(proj, "winner.txt"))
	if err != nil || string(raw) != "winner" {
		t.Fatalf("winner.txt = %q, %v (winner overlay must apply)", raw, err)
	}
	// Every attempt's calls merged into the outer records tagged by attempt:
	// 2 tool calls (write+read) and 3 model calls per attempt.
	for _, tc := range []struct {
		name string
		min  int
	}{{"tool_calls.jsonl", 6}, {"model_calls.jsonl", 9}} {
		p := filepath.Join(logs, "excalibur", tc.name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("outer %s missing (merge): %v", tc.name, err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) < tc.min {
			t.Fatalf("outer %s has %d lines, want >=%d (3 attempts)", tc.name, len(lines), tc.min)
		}
		seen := map[string]bool{}
		for _, ln := range lines {
			var row map[string]any
			if err := json.Unmarshal([]byte(ln), &row); err != nil {
				t.Fatalf("outer %s invalid JSON: %v", tc.name, err)
			}
			a, ok := row["attempt"]
			if !ok {
				t.Fatalf("outer %s row missing attempt tag: %s", tc.name, ln)
			}
			seen[fmt.Sprintf("%v", a)] = true
		}
		for _, want := range []string{"0", "1", "2"} {
			if !seen[want] {
				t.Fatalf("outer %s missing attempt %s rows (seen %v)", tc.name, want, seen)
			}
		}
	}
	// attempts.json still records per-attempt outcomes.
	recRaw, err := os.ReadFile(filepath.Join(logs, "excalibur", "attempts.json"))
	if err != nil {
		t.Fatalf("attempts.json missing: %v", err)
	}
	var rec attempts.Record
	if err := json.Unmarshal(recRaw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.N != 3 || len(rec.Attempts) != 3 {
		t.Fatalf("record = %+v, want 3 attempts", rec)
	}
	completed := 0
	for _, a := range rec.Attempts {
		if a.State == string(types.RunCompleted) {
			completed++
		}
	}
	if completed == 0 {
		t.Fatalf("no attempt completed: %+v", rec.Attempts)
	}
}

// TestAttemptsLegacySplitPauses documents the pre-fix paused outcome:
// with EXCALIBUR_ATTEMPTS_FULL_BUDGET=0 the legacy ceil(parent/N) split
// gives 2 steps per attempt for parent MaxSteps=6, so the same 3-step
// script pauses every attempt. The outer run must then end paused with
// an honest budget reason (not an empty one) instead of silently
// completing.
func TestAttemptsLegacySplitPauses(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "3")
	t.Setenv("EXCALIBUR_ATTEMPTS_FULL_BUDGET", "0")
	t.Setenv("EXCALIBUR_ATTEMPTS_MERGE_RECORDS", "")
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := t.TempDir()
	fake := &attemptAwareFake{}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write winner", LogsDir: logs, Profile: ProfileEval,
		ProjectDir: proj, Provider: fake, MaxSteps: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunPaused {
		t.Fatalf("legacy split state = %q, want paused (2 steps cannot run the 3-step script)", out.State)
	}
	if out.Reason != "budget" {
		t.Fatalf("legacy split reason = %q, want budget", out.Reason)
	}
}
