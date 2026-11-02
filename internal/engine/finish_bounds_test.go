package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestFinishSameRefusalBlocksOnlyTwice(t *testing.T) {
	for _, enabled := range []string{"1", "0"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv("EXCALIBUR_FINISH_REFUSAL_CAP", enabled)
			runner := scriptedRunner()
			runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusToolError, Output: "exit: 7\nvalidation failed"})
			turns := []providers.ChatResponse{toolResp(toolCall("check", "exec", `{"command":"python3 verify.py"}`))}
			for i := 0; i < 4; i++ {
				turns = append(turns, toolResp(toolCall(fmt.Sprint(i), "finish", finishArgs("delivered", "check attempted"))))
			}
			e, js := testEngine(&fakeEndpoint{responses: turns}, runner, passingGate())
			e.deps.SelfVerify = selfVerifyOffCfg()
			e.deps.Artifacts = artifactOffCfg()
			run := explicitRun()
			startRun(t, e, run, js)
			// Exercise each finish directly so the disabled policy cannot consume
			// an unbounded model sequence after all scripted turns are exhausted.
			if _, err := e.execBatch(finishTestContext(t), run, []providers.ToolCall{toolCall("check", "exec", `{"command":"python3 verify.py"}`)}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 4; i++ {
				done, err := e.execFinishBatch(finishTestContext(t), run, toolCall(fmt.Sprint(i), "finish", finishArgs("delivered", "check attempted")), "")
				if err != nil {
					t.Fatal(err)
				}
				if enabled == "1" && i == 2 && !strings.Contains(e.confirmationText(run), "Advisory:") {
					t.Fatal("missing advisory checklist item")
				}
				if done && i != 3 {
					t.Fatalf("ended at finish %d", i+1)
				}
			}
			if enabled == "1" {
				mustState(t, run, types.RunCompleted)
				if run.FinishRefusals != 2 {
					t.Fatalf("refusals=%d", run.FinishRefusals)
				}
			} else if run.FinishRefusals != 4 {
				t.Fatalf("disabled refusals=%d", run.FinishRefusals)
			}
		})
	}
}

func TestUnverifiedChangesShareRunRefusalBudget(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	for i := 0; i < 2; i++ {
		e.countBlockingReasons(run, []string{fmt.Sprintf("changes since the last check are unverified; run check %d", i)})
	}
	if hard := e.hardFinishReasons(run, []string{"changes since the last check are unverified; run another check"}); len(hard) != 0 {
		t.Fatalf("third freshness refusal: %v", hard)
	}
	if hard := e.hardFinishReasons(run, []string{"a different problem"}); len(hard) != 1 {
		t.Fatalf("unrelated item lost: %v", hard)
	}
}

func TestServiceProgramDoesNotNeedToExit(t *testing.T) {
	history := []tasks.HistoryTool{
		{Name: "write_file", Args: `{"path":"/app/service.py"}`, Output: "written"},
		{Name: "exec", Args: `{"command":"python3 /app/service.py"}`, Output: "exit: running\nsession: sess-1 alive: true"},
		{Name: "exec", Args: `{"command":"curl --fail http://localhost:8765/status"}`, Output: "exit: 0\nready"},
	}
	files := []deliverable{{Path: "/app/service.py", Base: "service.py", Program: true}}
	if reasons := unexecutedProgramReasons("Create a service", history, files); len(reasons) != 0 {
		t.Fatal(reasons)
	}
	t.Setenv("EXCALIBUR_SERVICE_EXERCISE", "0")
	if reasons := unexecutedProgramReasons("Create a service", history, files); len(reasons) == 0 {
		t.Fatal("legacy policy accepted a live program")
	}
}
