package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestExecFinishAuditStream(t *testing.T) {
	for _, profile := range []ProfileName{ProfileEval, ProfileDefault} {
		t.Run(string(profile), func(t *testing.T) {
			fake := providerfake.New("finish-audit")
			fake.Responses = finishScript("done", "checked")
			logs := t.TempDir()
			out, err := RunExec(context.Background(), ExecOptions{MaxSteps: 12, TimeoutSecs: 10, Request: "hi", LogsDir: logs, Profile: profile, Provider: fake, ProjectDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if out.State != types.RunCompleted {
				t.Fatal(out.State)
			}
			dir := out.Dir
			if profile == ProfileEval {
				dir = filepath.Join(logs, "excalibur")
			}
			data, err := os.ReadFile(filepath.Join(dir, "gate_attempts.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 2 {
				t.Fatalf("expected exactly two actual verdicts: %s", data)
			}
			for i, line := range lines {
				var row eval.GateAttempt
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatal(err)
				}
				want := "confirmation_request"
				if i == 1 {
					want = "acceptance"
				}
				if row.Outcome != want || row.Mechanism != "explicit_finish" || row.AttemptID == "" || row.Attempt != i+1 || row.Passed != (i == 1) {
					t.Fatal(row)
				}
			}
		})
	}
}
