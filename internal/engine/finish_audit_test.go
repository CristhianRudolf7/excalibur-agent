package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestFinishAuditRecordsEveryVerdictDurably(t *testing.T) {
	t.Setenv("EXCALIBUR_FINISH_AUDIT", "on")
	// Audit is independent of whether verdicts are rewritten into tool output.
	t.Setenv("EXCALIBUR_FINISH_VERDICT_RESULT", "off")
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	r := explicitRun()
	startRun(t, e, r, js)
	dir := t.TempDir()
	recorder, err := eval.NewTrialRecorder(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rows []FinishAuditVerdict
	r.SetFinishAudit(func(v FinishAuditVerdict) error {
		rows = append(rows, v)
		return recorder.RecordGate(eval.GateAttempt{At: v.At, Attempt: v.Sequence, AttemptID: v.AttemptID, Mechanism: v.Mechanism, Outcome: v.Outcome, Note: v.Note})
	})
	ctx := finishTestContext(t)
	e.refuseCall(ctx, r, toolCall("refused", "finish", `{}`), []string{"missing output"})
	r.clearRefusal()
	r.fin.refused = false
	if stop, err := e.execFinishBatch(ctx, r, toolCall("confirm", "finish", finishArgs("done", "checked")), ""); err != nil || stop {
		t.Fatal(stop, err, rows, r.LastRefusal)
	}
	if stop, err := e.execFinishBatch(ctx, r, toolCall("accepted", "finish", finishArgs("done", "checked")), ""); err != nil || !stop {
		t.Fatal(stop, err, rows, r.LastRefusal)
	}
	mustState(t, r, types.RunCompleted)
	if len(rows) != 3 {
		t.Fatalf("verdicts: %+v", rows)
	}
	for i, want := range []string{"refusal", "confirmation_request", "acceptance"} {
		if rows[i].Outcome != want || rows[i].Sequence != i+1 || rows[i].AttemptID == "" || rows[i].Mechanism != "explicit_finish" {
			t.Fatal(rows)
		}
	}
	// Reopen the file independently: all decisions already survived before
	// finalization, without synthesizing passing rows from terminal state.
	f, err := os.Open(filepath.Join(dir, "gate_attempts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	for _, want := range rows {
		var row eval.GateAttempt
		if err := decoder.Decode(&row); err != nil {
			t.Fatal(err)
		}
		if row.Outcome != want.Outcome || row.AttemptID != want.AttemptID || row.Mechanism != want.Mechanism {
			t.Fatal(row)
		}
	}
	t.Setenv("EXCALIBUR_FINISH_AUDIT", "off")
	e.setFinishVerdict(r, "disabled", "finish refused: disabled", true)
	if len(rows) != 3 {
		t.Fatal("disabled audit wrote a row")
	}
}

func TestFinishAuditAutomaticPaths(t *testing.T) {
	for _, kind := range []string{"reserve", "safety", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
			e.deps.SelfVerify = selfVerifyOffCfg()
			e.deps.Artifacts = artifactOffCfg()
			r := explicitRun()
			r.Workdir = t.TempDir()
			startRun(t, e, r, js)
			var rows []FinishAuditVerdict
			r.SetFinishAudit(func(v FinishAuditVerdict) error { rows = append(rows, v); return nil })
			ctx := finishTestContext(t)
			want := ""
			switch kind {
			case "reserve":
				if err := e.completeReservedFinish(ctx, r, FinishArgs{Status: "completed"}, "reserve-call"); err != nil {
					t.Fatal(err)
				}
				want = "reserve_acceptance"
			case "safety":
				r.Tracker.Limit = tasks.Limit{}
				r.Steps = 201
				if stop, err := e.step(ctx, r); err != nil || !stop {
					t.Fatal(stop, err, rows, r.LastRefusal)
				}
				want = "safety_cap"
			case "deadline":
				t.Setenv("EXCALIBUR_FINISH_TIME_RESERVE", "off")
				r.Profile.WallSource = "explicit"
				r.Tracker.Limit.MaxSeconds = 100
				r.armConfirm(FinishArgs{Status: "completed"})
				if done, err := e.completePendingConfirmation(ctx, r, r.StartedAt.Add(96*time.Second)); err != nil || !done {
					t.Fatal(done, err)
				}
				want = "acceptance"
			}
			if len(rows) != 1 || rows[0].Outcome != want || rows[0].AttemptID == "" {
				t.Fatal(rows)
			}
		})
	}
}
