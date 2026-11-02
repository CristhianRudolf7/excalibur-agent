package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestTimeReserveAdvisoryAndImmediateFinish(t *testing.T) {
	for _, timeOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "wall-only"}[timeOnly], func(t *testing.T) {
			e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), GateFunc(func(*Run) tasks.GateReport {
				return tasks.GateReport{Pendings: []string{"required artifact missing: /app/output.txt"}}
			}))
			e.deps.SelfVerify = selfVerifyOffCfg()
			e.deps.Artifacts = artifactOffCfg()
			run := explicitRun()
			run.Profile.TimeBudgetOnly = timeOnly
			run.Tracker.Limit = tasks.Limit{MaxSeconds: 100}
			startRun(t, e, run, js)
			args := FinishArgs{Status: "completed", Summary: "delivered", Evidence: "available work checked"}
			e.deps.Clock = func() time.Time { return run.StartedAt.Add(69 * time.Second) }
			if e.validateFinish(finishTestContext(t), run, args, args.Evidence) == "" {
				t.Fatal("early refusal lost")
			}
			e.deps.Clock = func() time.Time { return run.StartedAt.Add(70 * time.Second) }
			for i := 0; i < 3; i++ {
				if reason := e.validateFinish(finishTestContext(t), run, args, args.Evidence); reason != "" {
					t.Fatalf("reserve refusal: %s", reason)
				}
			}
			if countContaining(run.VolatileNotes(), timeReserveNote) != 1 {
				t.Fatal(run.VolatileNotes())
			}
			if text := e.confirmationText(run); !strings.Contains(text, "Advisory: required artifact missing") {
				t.Fatal(text)
			}
			e.deps.Clock = func() time.Time { return run.StartedAt.Add(85 * time.Second) }
			// No gate or verifier should run at the immediate-accept threshold.
			e.deps.Gate = GateFunc(func(*Run) tasks.GateReport { t.Fatal("late gate ran"); return tasks.GateReport{} })
			done, err := e.execFinishBatch(finishTestContext(t), run, toolCall("finish", "finish", finishArgs("delivered", "checked")), "")
			if err != nil || !done {
				t.Fatalf("done=%v err=%v", done, err)
			}
			mustState(t, run, types.RunCompleted)
		})
	}
}

func TestTimeReservePendingConfirmationAndSwitch(t *testing.T) {
	for _, enabled := range []string{"1", "0"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv("EXCALIBUR_FINISH_TIME_RESERVE", enabled)
			t.Setenv("EXCALIBUR_FINISH_CONFIRM_PROGRESS", "0")
			e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
			run := explicitRun()
			run.Profile.TimeBudgetOnly = true
			run.Tracker.Limit = tasks.Limit{MaxSeconds: 100}
			startRun(t, e, run, js)
			run.armConfirm(FinishArgs{Summary: "done", Evidence: "checked"})
			run.confirmProgress.changed = true
			now := run.StartedAt.Add(85 * time.Second)
			done, err := e.completePendingConfirmation(finishTestContext(t), run, now)
			if err != nil || done != (enabled == "1") {
				t.Fatalf("done=%v err=%v", done, err)
			}
		})
	}
}
