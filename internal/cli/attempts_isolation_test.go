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
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// argCapturingToolRunner stands in for the engine's tool runner and keeps
// the arguments each call received.
type argCapturingToolRunner struct {
	mu    sync.Mutex
	names []string
	args  []string
}

func (r *argCapturingToolRunner) Definition(name string) (tools.Definition, error) {
	return tools.Definition{Name: name}, nil
}

func (r *argCapturingToolRunner) Execute(_ context.Context, name string, args json.RawMessage, _ tools.ExecuteOptions) (tools.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	r.args = append(r.args, string(args))
	return tools.Result{}, nil
}

func jsonArgs(t *testing.T, v map[string]string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestAttemptPathRunnerMapsArgsBeforeTool: a tool call an attempt makes
// with absolute paths under the real directory reaches the tool with the
// attempt's copy, and the text content is left alone.
func TestAttemptPathRunnerMapsArgsBeforeTool(t *testing.T) {
	real := t.TempDir()
	copyDir := t.TempDir()
	m := attempts.WorkdirMap{Real: real, Attempt: copyDir}
	base := &argCapturingToolRunner{}
	r := attemptPathRunnerFor(base, &m)

	args := jsonArgs(t, map[string]string{
		"path":    filepath.Join(real, "x.txt"),
		"content": "see " + filepath.Join(real, "y"),
	})
	if _, err := r.Execute(context.Background(), "write_file", args, tools.ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(base.args[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got["path"] != filepath.Join(copyDir, "x.txt") {
		t.Fatalf("path reached the tool as %q, want the attempt copy", got["path"])
	}
	if got["content"] != "see "+filepath.Join(real, "y") {
		t.Fatalf("content was rewritten: %q", got["content"])
	}

	// The exec tool runs in the copy unless the model names another dir.
	if _, err := r.Execute(context.Background(), "exec", json.RawMessage(`{"command":"make"}`), tools.ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	var execArgs map[string]string
	if err := json.Unmarshal([]byte(base.args[1]), &execArgs); err != nil {
		t.Fatal(err)
	}
	if execArgs["workdir"] != copyDir {
		t.Fatalf("exec workdir = %q, want %q", execArgs["workdir"], copyDir)
	}

	// Without a map the runner is the base runner: arguments untouched.
	plain := attemptPathRunnerFor(base, nil)
	if _, err := plain.Execute(context.Background(), "write_file", args, tools.ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	if base.args[2] != string(args) {
		t.Fatalf("nil map must pass arguments through: %s", base.args[2])
	}
}

// TestAttemptArtifactStatResolvesThroughMap: the engine's completion gate
// inside an attempt checks the attempt's copy for absolute requirement
// paths under the real directory.
func TestAttemptArtifactStatResolvesThroughMap(t *testing.T) {
	real := t.TempDir()
	copyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(copyDir, "out.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := attempts.WorkdirMap{Real: real, Attempt: copyDir}
	stat := attemptArtifactStat(&m)
	if _, err := stat(filepath.Join(real, "out.txt")); err != nil {
		t.Fatalf("attempt copy must satisfy the real path: %v", err)
	}
	if _, err := stat(filepath.Join(real, "missing.txt")); err == nil {
		t.Fatal("a file absent from the copy must not be found")
	}
	if attemptArtifactStat(nil) != nil || attemptArtifactRead(nil) != nil {
		t.Fatal("nil map keeps the default checks")
	}
}

// attemptTag names the attempt behind a work hook from its log dir.
func attemptTag(run *ExecRun) string {
	if strings.Contains(run.Dir, "attempt-0") {
		return "a"
	}
	if strings.Contains(run.Dir, "attempt-1") {
		return "b"
	}
	return "fallback"
}

// TestAttemptsLosersNeverReachRealDirectory: every attempt runs in its own
// copy, and only the winner's file is promoted into the real directory.
func TestAttemptsLosersNeverReachRealDirectory(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "2")
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []string
	work := func(ctx context.Context, run *ExecRun) error {
		mu.Lock()
		seen = append(seen, run.ProjectDir)
		mu.Unlock()
		return os.WriteFile(filepath.Join(run.ProjectDir, "marker-"+attemptTag(run)+".txt"), []byte("m"), 0o644)
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write a marker", LogsDir: t.TempDir(), ProjectDir: proj, Work: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed", out.State)
	}
	if len(seen) != 2 {
		t.Fatalf("attempts ran %d times, want 2", len(seen))
	}
	for _, dir := range seen {
		if dir == proj || !strings.HasSuffix(dir, proj[len(filepath.VolumeName(proj)):]) {
			t.Fatalf("attempt workdir %q must be a copy under the attempt root", dir)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, "marker-a.txt")); err != nil {
		t.Fatalf("winner's marker must be promoted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "marker-b.txt")); !os.IsNotExist(err) {
		t.Fatal("a losing attempt's file must never reach the real directory")
	}
	raw, err := os.ReadFile(filepath.Join(out.Dir, "attempts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec attempts.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if !rec.Isolated || rec.AppliedFileCount != 1 || rec.Fallback != "" {
		t.Fatalf("record = isolated %v applied %d fallback %q, want isolated, 1 applied, no fallback",
			rec.Isolated, rec.AppliedFileCount, rec.Fallback)
	}
}

// TestAttemptsDeliverableDecidesWinner: the attempt that wrote the named
// output wins, even though the other attempt also changed files.
func TestAttemptsDeliverableDecidesWinner(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "2")
	proj := t.TempDir()
	req := "Write the result to `" + filepath.Join(proj, "out.txt") + "`."
	work := func(ctx context.Context, run *ExecRun) error {
		if attemptTag(run) == "a" {
			return os.WriteFile(filepath.Join(run.ProjectDir, "other.txt"), []byte("x"), 0o644)
		}
		return os.WriteFile(filepath.Join(run.ProjectDir, "out.txt"), []byte("result"), 0o644)
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: req, LogsDir: t.TempDir(), ProjectDir: proj, Work: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed (the named output exists)", out.State)
	}
	raw, err := os.ReadFile(filepath.Join(out.Dir, "attempts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec attempts.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Winner != 1 {
		t.Fatalf("winner = %d, want 1 (the attempt that produced the named output)", rec.Winner)
	}
	if _, err := os.Stat(filepath.Join(proj, "out.txt")); err != nil {
		t.Fatalf("named output must be promoted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "other.txt")); !os.IsNotExist(err) {
		t.Fatal("the losing attempt's file must not be promoted")
	}
}

// escapingWinnerWork is the apply-failure scenario: attempt b writes a
// file the real directory already holds as a populated directory (an
// escaped write), so promoting its change cannot complete.
func escapingWinnerWork(proj string) func(context.Context, *ExecRun) error {
	return func(ctx context.Context, run *ExecRun) error {
		if run.ProjectDir == proj {
			// The fallback: one normal attempt in the real directory.
			return os.WriteFile(filepath.Join(proj, "fallback.txt"), []byte("fallback"), 0o644)
		}
		if attemptTag(run) == "a" {
			return fmt.Errorf("attempt a fails")
		}
		if err := os.WriteFile(filepath.Join(run.ProjectDir, "data"), []byte("winner"), 0o644); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(proj, "data"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(proj, "data", "escaped.txt"), []byte("escape"), 0o644)
	}
}

// TestAttemptsApplyFailureFallsBackToRealDirectory: the winner's changes
// cannot be promoted, so the run records the failure and runs one normal
// attempt in the real directory with the remaining time.
func TestAttemptsApplyFailureFallsBackToRealDirectory(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "2")
	t.Setenv("EXCALIBUR_ATTEMPTS_FALLBACK", "")
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "produce output", LogsDir: t.TempDir(), ProjectDir: proj, Work: escapingWinnerWork(proj),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed from the fallback attempt", out.State)
	}
	if !strings.Contains(out.Summary, "attempts fallback") {
		t.Fatalf("summary = %q, want the fallback marker", out.Summary)
	}
	if _, err := os.Stat(filepath.Join(proj, "fallback.txt")); err != nil {
		t.Fatalf("the fallback attempt must run in the real directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "data", "escaped.txt")); err != nil {
		t.Fatal("the refused promotion must leave the real directory as it was")
	}
	if _, err := os.Stat(filepath.Join(proj, "data")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out.Dir, "attempts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec attempts.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.Fallback, "apply:") {
		t.Fatalf("record fallback = %q, want the apply failure", rec.Fallback)
	}
}

// TestAttemptsApplyFailureWithoutFallbackFails: with the fallback switch
// off, the same failure ends the run as failed and is not hidden.
func TestAttemptsApplyFailureWithoutFallbackFails(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "2")
	t.Setenv("EXCALIBUR_ATTEMPTS_FALLBACK", "0")
	proj := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "produce output", LogsDir: t.TempDir(), ProjectDir: proj, Work: escapingWinnerWork(proj),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunFailed {
		t.Fatalf("state = %q, want failed when the fallback is off", out.State)
	}
	if _, err := os.Stat(filepath.Join(proj, "fallback.txt")); !os.IsNotExist(err) {
		t.Fatal("no fallback attempt may run when the switch is off")
	}
}
