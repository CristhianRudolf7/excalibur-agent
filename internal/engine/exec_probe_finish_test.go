package engine

import (
	"context"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// A probe whose answer is "no" (test -f on a missing file) exits 1 and
// is an error in the record, but it is not the agent's failed check:
// finish must not be refused as "your last command errored".
func TestExplicitFinishNotRefusedAfterProbeFailure(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"test -f /nope/out.txt"}`)),
		toolResp(toolCall("f1", "finish", finishArgs("checked", "the file is absent as expected"))),
		toolResp(toolCall("f2", "finish", finishArgs("checked", "the file is absent as expected"))),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusToolError, Output: "exit: 1  duration: 0.0s  session: sess-1  alive: false\nnote: exit 1 here usually means \"not found / no match\", not a failure\n"})
	e, js := testEngine(model, runner, passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if run.FinishRefusals != 0 {
		t.Fatalf("probe failure must not refuse finish: refusals = %d", run.FinishRefusals)
	}
}
