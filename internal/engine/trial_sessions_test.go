package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func delayedTrial(t *testing.T, e *Engine, r *Run, check providers.ToolCall, output string) {
	t.Helper()
	running := tools.Result{Status: tools.StatusOK, Output: "exit: running  session: sess-trial  alive: true\n", Sessions: []tools.SessionReceipt{{ID: "sess-trial", Alive: true}}}
	calls := r.trialCalls([]providers.ToolCall{check}, []tools.Result{running})
	before := e.captureTrialObservation(r, calls)
	e.observeProbeCopies(r, calls, []tools.Result{running}, before)
	e.observeManualTrials(r, calls, []tools.Result{running}, before)
	poll := trialCall("exec_io", map[string]string{"action": "read", "session": "sess-trial"})
	calls = r.trialCalls([]providers.ToolCall{poll}, nil)
	before = e.captureTrialObservation(r, calls)
	code := 1
	if strings.Contains(output, "0 warnings") {
		code = 0
	}
	result := tools.Result{Status: tools.StatusOK, Output: output, Sessions: []tools.SessionReceipt{{ID: "sess-trial", ExitCode: &code}}}
	calls = r.trialCalls([]providers.ToolCall{poll}, []tools.Result{result})
	e.observeProbeCopies(r, calls, []tools.Result{result}, before)
	e.observeManualTrials(r, calls, []tools.Result{result}, before)
}
func TestTrialSessionsDelayedChecks(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	check := trialCall("exec", map[string]string{"command": "python3 validate.py " + copy})
	delayedTrial(t, e, r, check, "4 warnings\n")
	editTrialCopy(t, e, r, copy, "alpha\nbest\n")
	delayedTrial(t, e, r, check, "3 warnings\n")
	e.probeSyncCheck(r, r.StartedAt)
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("delayed check improvement missing", probeNotes(r))
	}
	if len(r.trialSessions) != 0 {
		t.Fatal("completed trial session retained")
	}
}
func TestTrialSessionsDelayedManualTrials(t *testing.T) {
	e, r, path, _ := probeRun(t)
	check := trialCall("exec", map[string]string{"command": "python3 validate.py " + path})
	for i := 0; i < 5; i++ {
		if i > 0 {
			manualEdit(t, e, r, path, fmt.Sprintf("choice=%d", i))
		}
		delayedTrial(t, e, r, check, "4 warnings\n")
	}
	if !strings.Contains(probeNotes(r), manualTrialsNote) {
		t.Fatal("delayed manual checks not counted", probeNotes(r))
	}
}
func TestTrialSessionsIgnoreWritesAndUnownedReceipts(t *testing.T) {
	_, r, path, _ := probeRun(t)
	check := trialCall("exec", map[string]string{"command": "python3 validate.py " + path})
	running := tools.Result{Status: tools.StatusOK, Output: "exit: running  alive: true", Sessions: []tools.SessionReceipt{{ID: "sess-trial", Alive: true}}}
	r.trialCalls([]providers.ToolCall{check}, []tools.Result{running})
	write := trialCall("exec_io", map[string]string{"action": "write", "session": "sess-trial", "text": "change"})
	calls := r.trialCalls([]providers.ToolCall{write}, []tools.Result{{Status: tools.StatusOK}})
	if calls[0].Name != "exec_io" || len(r.trialSessions) != 0 {
		t.Fatal("write treated as old checker")
	}
	poll := trialCall("exec_io", map[string]string{"action": "read", "session": "sess-other"})
	calls = r.trialCalls([]providers.ToolCall{poll}, []tools.Result{{Status: tools.StatusOK, Output: "exit: 0\n"}})
	if calls[0].Name != "exec_io" {
		t.Fatal("unowned poll treated as checker")
	}
}

func TestTrialSessionsBatchHookUsesOriginatingChecker(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	editTrialCopy(t, e, r, copy, "alpha\nbest\n")
	runner := scriptedRunner()
	code := 0
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "exit: running  alive: true", Sessions: []tools.SessionReceipt{{ID: "sess-trial", Alive: true}}})
	runner.add("exec_io", types.EffectReadOnly, types.IdemPure, tools.Result{Status: tools.StatusOK, Output: "0 warnings\n", Sessions: []tools.SessionReceipt{{ID: "sess-trial", ExitCode: &code}}})
	e.deps.Tools = runner
	_, js := testEngine(&fakeEndpoint{}, runner, passingGate())
	startRun(t, e, r, js)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, tc := range []providers.ToolCall{
		trialCall("exec", map[string]string{"command": "python3 validate.py " + copy}),
		trialCall("exec_io", map[string]string{"action": "read", "session": "sess-trial"}),
	} {
		if _, err := e.execBatch(ctx, r, []providers.ToolCall{tc}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("delayed batch check lost command identity", probeNotes(r))
	}
}
