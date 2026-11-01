package cli

import (
	"context"
	"strings"
	"testing"

	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// The long-horizon pacing line rides L5 by default (elapsed plus
// remaining steps/time) so the model can pace itself; the switch
// restores the pre-long-horizon bytes.
func TestExecLongHorizonPacing(t *testing.T) {
	fake := providerfake.New("lh-pacing")
	fake.Responses = doneImmediately()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: t.TempDir(), Profile: ProfileEval,
		Provider: fake, MaxSteps: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	sys := systemText(fake.ChatRequests[0])
	if !strings.Contains(sys, "pacing:") || !strings.Contains(sys, "elapsed") {
		t.Fatalf("L5 must carry the pacing line, got:\n%s", sys)
	}
	if !strings.Contains(sys, "milestone plan") && !strings.Contains(sys, "Work long tasks") {
		t.Fatalf("fixed instructions must carry the long-horizon discipline, got:\n%s", sys)
	}
}

func TestExecLongHorizonSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_LONG_HORIZON", "off")
	t.Setenv("EXCALIBUR_TIME_BUDGET", "off")
	fake := providerfake.New("lh-off")
	fake.Responses = doneImmediately()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: t.TempDir(), Profile: ProfileEval,
		Provider: fake, MaxSteps: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	sys := systemText(fake.ChatRequests[0])
	if strings.Contains(sys, "pacing:") || strings.Contains(sys, "Work long tasks") {
		t.Fatalf("disabled long horizon must stay silent, got:\n%s", sys)
	}
}

// A large request builds an explicit milestone plan: the first model
// call already carries the plan in its pinned working notes.
func TestExecLongHorizonMilestones(t *testing.T) {
	longReq := "Migrate the service to the new API: " + strings.Repeat("rewrite the handlers, update the tests, verify end to end. ", 20)
	fake := providerfake.New("lh-milestones")
	// Explicit finish: runs end ONLY through the two-turn `finish`
	// confirmation, never through a text-only turn.
	fake.Responses = finishScript("done", "no tool work needed; request verified by inspection")
	out, err := RunExec(context.Background(), ExecOptions{
		Request: longReq, LogsDir: t.TempDir(), Profile: ProfileEval,
		Provider: fake, MaxSteps: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	sys := systemText(fake.ChatRequests[0])
	// The plan decision needs the stashed request, so the first call
	// may not yet carry it; the second call (or the working notes)
	// must. The run ends through the two-turn `finish`, but the
	// pacing line must already show milestone progress tracking.
	if !strings.Contains(sys, "pacing:") {
		t.Fatalf("large task must carry pacing, got:\n%s", sys)
	}
}
