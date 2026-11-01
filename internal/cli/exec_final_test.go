package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestExecDeadlineRecordsBeforeWorkerReturns(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER", "stub")
	dir, logs := t.TempDir(), t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	safego.Go("test-final-worker", nil, func() {
		out, err := RunExec(ctx, ExecOptions{Request: "own check", RunDir: dir, LogsDir: logs, Profile: ProfileEval, Work: func(context.Context, *ExecRun) error {
			close(entered)
			<-release
			return errors.New("late worker error")
		}})
		if err == nil && out.State != stoppedByTimeout {
			err = errors.New("late worker overwrote timeout")
		}
		done <- err
	})
	<-entered
	cancel()
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		raw, err := os.ReadFile(filepath.Join(logs, "excalibur", "final.json"))
		if err == nil {
			var final eval.FinalState
			if err = json.Unmarshal(raw, &final); err != nil {
				t.Fatal(err)
			}
			if final.State != "stopped_by_timeout" || final.ExitCode != ExitPaused {
				t.Fatal(string(raw))
			}
			// Wait for the remaining snapshot files after the first final marker.
			if _, err = os.Stat(filepath.Join(logs, "excalibur", "metrics.json")); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("cancellation did not persist records within 2 seconds")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A subprocess receives real SIGTERM/SIGINT without interrupting the test runner.
func TestExecSignalRecords(t *testing.T) {
	if os.Getenv("EXCALIBUR_SIGNAL_HELPER") == "1" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		_, err := RunExec(ctx, ExecOptions{Request: "wait", RunDir: os.Getenv("EXCALIBUR_SIGNAL_DIR"), Profile: ProfileEval, LogsDir: os.Getenv("EXCALIBUR_SIGNAL_DIR"), Work: func(ctx context.Context, r *ExecRun) error {
			if err := os.WriteFile(filepath.Join(r.Dir, "ready"), []byte("ready"), 0o600); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		}})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, sig := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		t.Run(sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestExecSignalRecords$")
			cmd.Env = append(os.Environ(), "EXCALIBUR_PROVIDER=stub", "EXCALIBUR_SIGNAL_HELPER=1", "EXCALIBUR_SIGNAL_DIR="+dir)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			until := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
					break
				}
				if time.Now().After(until) {
					t.Fatal("helper not ready")
				}
				time.Sleep(5 * time.Millisecond)
			}
			start := time.Now()
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("signal cleanup exceeded 2 seconds")
			}
			for _, name := range []string{"final.json", "metrics.json", "trajectory.json", "excalibur/final.json"} {
				raw, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || !json.Valid(raw) {
					t.Fatalf("invalid %s: %v", name, err)
				}
			}
			raw, _ := os.ReadFile(filepath.Join(dir, "excalibur/final.json"))
			var final eval.FinalState
			_ = json.Unmarshal(raw, &final)
			if final.State != "stopped_by_timeout" {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestDelegatedInterruptionKeepsDurableState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := RunExec(ctx, ExecOptions{Request: "delegated work", RunDir: t.TempDir(), TaskID: "delegated-task", Work: func(context.Context, *ExecRun) error { cancel(); return context.Canceled }})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCanceled || out.ExitCode != ExitCanceled {
		t.Fatalf("delegated cancellation=%s/%d", out.State, out.ExitCode)
	}
}

func TestExecOrdinaryFinalRecord(t *testing.T) {
	out, err := RunExec(context.Background(), ExecOptions{Request: "record ordinary completion", RunDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out.Dir, "final.json"))
	if err != nil {
		t.Fatal(err)
	}
	var final eval.FinalState
	if err := json.Unmarshal(raw, &final); err != nil {
		t.Fatal(err)
	}
	if final.State != "completed" || final.ExitCode != 0 {
		t.Fatal(string(raw))
	}
}
