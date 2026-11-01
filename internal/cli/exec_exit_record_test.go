package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// fixedToolRunner returns one canned exec result for every call.
type fixedToolRunner struct{ res tools.Result }

func (f fixedToolRunner) Definition(name string) (tools.Definition, error) {
	return tools.Definition{Name: name}, nil
}

func (f fixedToolRunner) Execute(_ context.Context, _ string, _ json.RawMessage, _ tools.ExecuteOptions) (tools.Result, error) {
	return f.res, nil
}

// The trial record must carry the real shell exit, not a 0/1 guess
// derived from the status: a failing exec records its exit code and
// is_error, and a signal exit records 128+n.
func TestRecordingToolRunnerRecordsRealExitCode(t *testing.T) {
	cases := []struct {
		name     string
		res      tools.Result
		wantExit int
		wantErr  bool
	}{
		{"exit 3", tools.Result{Status: tools.StatusToolError, Output: "exit: 3  duration: 0.0s  session: sess-1  alive: false\n", ExitCode: intPtr(3)}, 3, true},
		{"signal", tools.Result{Status: tools.StatusToolError, Output: "exit: 139 (SIGSEGV)  duration: 0.0s  session: sess-2  alive: false\n", ExitCode: intPtr(139)}, 139, true},
		{"success", tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 0.0s  session: sess-3  alive: false\n", ExitCode: intPtr(0)}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			rec, err := eval.NewTrialRecorder(dir)
			if err != nil {
				t.Fatal(err)
			}
			r := &recordingToolRunner{base: fixedToolRunner{res: c.res}, run: &ExecRun{Recorder: rec}}
			if _, err := r.Execute(context.Background(), "exec", json.RawMessage(`{"command":"x"}`), tools.ExecuteOptions{}); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "tool_calls.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var call struct {
				ExitCode int  `json:"exit_code"`
				IsError  bool `json:"is_error"`
			}
			line := strings.TrimSpace(string(raw))
			if err := json.Unmarshal([]byte(line), &call); err != nil {
				t.Fatalf("parse %q: %v", line, err)
			}
			if call.ExitCode != c.wantExit || call.IsError != c.wantErr {
				t.Fatalf("record exit %d is_error %v, want %d %v", call.ExitCode, call.IsError, c.wantExit, c.wantErr)
			}
		})
	}
}

func intPtr(n int) *int { return &n }
