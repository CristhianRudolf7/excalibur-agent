package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TestRunFinishedSummaryNeverEchoesRequest: the run summary is the agent's
// finish summary or a generic state name, never the task prompt.
func TestRunFinishedSummaryNeverEchoesRequest(t *testing.T) {
	const request = "Create secret-marker.py that prints 2 and run it."
	for _, explicit := range []string{"on", "off"} {
		t.Run("explicit="+explicit, func(t *testing.T) {
			t.Setenv("EXCALIBUR_EXPLICIT_FINISH", explicit)
			dir := t.TempDir()
			out, err := RunExec(context.Background(), ExecOptions{
				Request: request, ProjectDir: dir, LogsDir: t.TempDir(), RunDir: t.TempDir(),
				Profile: ProfileDefault, Work: func(context.Context, *ExecRun) error { return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.Summary, "secret-marker") || out.Summary == "" {
				t.Fatalf("summary echoes the request: %q", out.Summary)
			}
			if out.Summary != "completed" {
				t.Fatalf("summary = %q, want the generic completed state", out.Summary)
			}
		})
	}
}

// TestStopPathPersistsRunFinishedTimeout: a run stopped by its deadline while
// a tool is still running ends with exactly one run_finished (state timeout)
// in events.jsonl and a stopped final record.
func TestStopPathPersistsRunFinishedTimeout(t *testing.T) {
	runDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	out, err := RunExec(ctx, ExecOptions{
		Request: "Wait for a long job.", ProjectDir: t.TempDir(), LogsDir: t.TempDir(), RunDir: runDir,
		Profile: ProfileDefault,
		Work: func(ctx context.Context, _ *ExecRun) error {
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond)
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != stoppedByTimeout {
		t.Fatalf("state = %s, want %s", out.State, stoppedByTimeout)
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var closing []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev["event"] == "run_finished" {
			closing = append(closing, ev)
		}
	}
	if len(closing) != 1 {
		t.Fatalf("run_finished rows = %d, want exactly 1: %s", len(closing), raw)
	}
	if closing[0]["state"] != stopEventState {
		t.Fatalf("run_finished state = %v, want %s", closing[0]["state"], stopEventState)
	}
	fin, err := os.ReadFile(filepath.Join(runDir, "final.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fin), string(stoppedByTimeout)) {
		t.Fatalf("final.json state: %s", fin)
	}
}

// TestFinishDetailReachesRunFinishedAndFinal: an accepted finish carries its
// summary and evidence into the trial record.
func TestFinishDetailReachesRunFinishedAndFinal(t *testing.T) {
	fake := providerfake.New("finish-detail")
	fake.Responses = finishScript("wrote the report", "report.txt read back, exit 0")
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "write the report", LogsDir: logs, Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted || out.Summary != "wrote the report" {
		t.Fatalf("state=%s summary=%q", out.State, out.Summary)
	}
	raw, err := os.ReadFile(filepath.Join(logs, "excalibur", "final.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fin struct {
		State          string `json:"state"`
		Summary        string `json:"summary"`
		FinishEvidence string `json:"finish_evidence"`
	}
	if err := json.Unmarshal(raw, &fin); err != nil {
		t.Fatal(err)
	}
	if fin.State != string(types.RunCompleted) || fin.Summary != "wrote the report" || fin.FinishEvidence != "report.txt read back, exit 0" {
		t.Fatalf("trial final.json %s", raw)
	}
}

// TestAcceptedFinishSurvivesLaterError (item 48): only a completed run keeps
// its state when an engine or provider error follows; every other state
// propagates the error as before.
func TestAcceptedFinishSurvivesLaterError(t *testing.T) {
	if !acceptedFinishSurvives(types.RunCompleted) {
		t.Fatal("a completed run must survive a later error")
	}
	for _, st := range []types.RunState{types.RunRunning, types.RunFailed, types.RunVerifying, types.RunPaused} {
		if acceptedFinishSurvives(st) {
			t.Fatalf("state %s must not swallow the error", st)
		}
	}
}
