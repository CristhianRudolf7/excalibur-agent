package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestFinishAfterClockRepeatRefusal(t *testing.T) {
	for _, advancing := range []bool{false, true} {
		t.Run(fmt.Sprint(advancing), func(t *testing.T) {
			t.Setenv("EXCALIBUR_FINISH_EXECUTED_ERRORS", "1")
			t.Setenv("EXCALIBUR_LOOP_GUARD_V2", "1")
			responses := repeatCalls(7, "clock", `{}`)
			responses = append(responses, toolResp(toolCall("f1", "finish", finishArgs("checked", "clock inspected"))), toolResp(toolCall("f2", "finish", finishArgs("checked", "clock inspected"))))
			model := &fakeEndpoint{responses: responses}
			runner := scriptedRunner()
			for i := 0; i < 7; i++ {
				second := 0
				if advancing {
					second = i
				}
				runner.add("clock", types.EffectReadOnly, types.IdemPure, tools.Result{Status: tools.StatusOK, Output: time.Unix(100+int64(second), 0).UTC().Format(time.RFC3339)})
			}
			e, js := testEngine(model, runner, passingGate())
			e.deps.SelfVerify = selfVerifyOffCfg()
			e.deps.Artifacts = artifactOffCfg()
			run := explicitRun()
			startRun(t, e, run, js)
			e.deps.Clock = func() time.Time { return run.StartedAt.Add(time.Second) }
			if err := e.Run(finishTestContext(t), run); err != nil {
				t.Fatal(err)
			}
			mustState(t, run, types.RunCompleted)
			if run.FinishRefusals != 0 {
				t.Fatalf("finish refusals = %d", run.FinishRefusals)
			}
			want := 9 // seven clock calls and two finish calls
			if !advancing {
				want--
			} // the repeat refusal did not execute
			if runner.ncalls() != want {
				t.Fatalf("executions = %d, want %d", runner.ncalls(), want)
			}
			refused := countContaining(historyContents(run), "refused: you ran this exact call")
			if (!advancing && refused != 1) || (advancing && refused != 0) {
				t.Fatalf("repeat refusals = %d", refused)
			}
		})
	}
}

func TestRepeatRefusalPreservesExecutedFailure(t *testing.T) {
	for _, v2 := range []string{"0", "1"} {
		t.Run(v2, func(t *testing.T) {
			t.Setenv("EXCALIBUR_FINISH_EXECUTED_ERRORS", "1")
			t.Setenv("EXCALIBUR_LOOP_GUARD_V2", v2)
			runner := constantRunner("exec", types.EffectExec, types.IdemNonIdempotent, "exit: 7  alive: false\nvalidation failed")
			for i := range runner.results["exec"] {
				runner.results["exec"][i].Status = tools.StatusToolError
			}
			e, js := testEngine(&fakeEndpoint{}, runner, passingGate())
			e.deps.SelfVerify = selfVerifyOffCfg()
			e.deps.Artifacts = artifactOffCfg()
			run := explicitRun()
			startRun(t, e, run, js)
			calls := 7
			if v2 == "0" {
				calls = 2
			}
			for i := 0; i < calls; i++ {
				run.Steps++
				call := toolCall(fmt.Sprint(i), "exec", `{"command":"validate"}`)
				if _, err := e.execBatch(finishTestContext(t), run, []providers.ToolCall{call}); err != nil {
					t.Fatal(err)
				}
			}
			h, found := lastNonFinishEntry(run)
			if !found || !h.IsError || !strings.Contains(h.Output, "exit: 7") {
				t.Fatalf("last executed result = %+v", h)
			}
			args := FinishArgs{Summary: "checked", Evidence: "validation attempted"}
			if reason := e.validateFinish(finishTestContext(t), run, args, args.Evidence); !strings.Contains(reason, "exit 7") {
				t.Fatalf("real failure lost: %s", reason)
			}
			t.Setenv("EXCALIBUR_FINISH_EXECUTED_ERRORS", "0")
			h, _ = lastNonFinishEntry(run)
			if !strings.Contains(h.Output, "refused:") && !strings.Contains(h.Output, "blocked: exact repeat") {
				t.Fatalf("legacy result = %+v", h)
			}
		})
	}
}

func TestFinishVerificationExcludesUnexecutedRepeat(t *testing.T) {
	for _, enabled := range []string{"1", "0"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv("EXCALIBUR_FINISH_EXECUTED_ERRORS", enabled)
			t.Setenv("EXCALIBUR_LOOP_GUARD_V2", "1")
			runner := constantRunner("exec", types.EffectExec, types.IdemNonIdempotent, "exit: 0  alive: false\nchecks passed")
			e, js := testEngine(&fakeEndpoint{}, runner, passingGate())
			e.deps.SelfVerify = selfVerifyOffCfg()
			run := explicitRun()
			startRun(t, e, run, js)
			for i := 0; i < 7; i++ {
				run.Steps++
				call := toolCall(fmt.Sprint(i), "exec", `{"command":"python3 verify.py"}`)
				run.History = append(run.History, ctxcompiler.ActivityEvent{Seq: run.nextHistorySeqLocked(), Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{call}})
				if _, err := e.execBatch(finishTestContext(t), run, []providers.ToolCall{call}); err != nil {
					t.Fatal(err)
				}
			}
			args := FinishArgs{Summary: "checked", Evidence: "verification passed"}
			dec := e.decideFinish(finishTestContext(t), run, args, args.Evidence)
			if enabled == "1" && len(dec.reasons) != 0 {
				t.Fatalf("unexecuted repeat failed verification: %v", dec.reasons)
			}
			if enabled == "0" && len(dec.reasons) == 0 {
				t.Fatal("legacy refusal no longer fails verification")
			}
			// The original error result remains present for model context/auditing.
			last := run.History[len(run.History)-1].ToolResult
			if last == nil || !last.IsError || !strings.HasPrefix(last.Output, "refused:") {
				t.Fatalf("lost refusal: %+v", last)
			}
		})
	}
}
