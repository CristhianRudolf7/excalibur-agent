package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func strictRunWithHistory(history []tasks.HistoryTool, wallSecs int64) (*Engine, *Run) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := NewRun("r1", "t1", Profile{
		Limit:          tasks.Limit{MaxSteps: SafetyMaxSteps, MaxSeconds: wallSecs},
		TimeBudgetOnly: wallSecs > 0, ExplicitFinish: true,
	})
	for i, h := range history {
		run.History = append(run.History, ctxcompiler.ActivityEvent{
			Seq:     int64(i + 1),
			Role:    providers.RoleTool,
			Content: h.Output,
			ToolResult: &providers.ToolResult{
				CallID: "c", Name: h.Name, Output: h.Output, IsError: h.IsError,
			},
		})
	}
	return e, run
}

func TestStrictBlockedSwitchDefaults(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "")
	if !StrictBlockedEnabled() {
		t.Fatal("strict blocked default must be ON")
	}
	t.Setenv(StrictBlockedSwitch, "0")
	if StrictBlockedEnabled() {
		t.Fatal("explicit off must disable")
	}
	t.Setenv(StrictBlockedSwitch, "off")
	if StrictBlockedEnabled() {
		t.Fatal("off must disable")
	}
}

// Validation failures while time remains are refused with a structured
// recovery prompt. This reproduces the measured premature blocked
// (21-22/74 runs end blocked after a single failed validation with
// most of the time unused; only 1 passed).
func TestStrictBlockedRefusesValidationFailure(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"Rscript -e 'source(\"/app/ars.R\"); print(test())'"}`, Output: "Error: hull fails for normal points", IsError: true},
		{Name: "exec", Args: `{"command":"Rscript -e 'print(.ars_hull(...))'"}`, Output: "Error: degenerate", IsError: true},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "Created the requested R source and attempted runtime verification, but the ARS hull implementation fails for normal-distribution support points. No valid result or sample files were generated."
	refusal := e.strictBlockedRefusal(run, reason)
	if refusal == "" {
		t.Fatal("validation-failure blocked must be refused while time remains")
	}
	for _, want := range []string{"strict blocked", "Failing check", "three materially different", "pick one", "Do not declare blocked"} {
		if !strings.Contains(refusal, want) {
			t.Fatalf("recovery prompt must contain %q, got: %s", want, refusal)
		}
	}
}

// Network impossibility after retries is accepted: reason names the
// host/404 and the history shows repeated network failures.
func TestStrictBlockedAcceptsNetworkAfterRetries(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"curl -f https://example.com/data.csv"}`, Output: "curl: (6) Could not resolve host: example.com", IsError: true},
		{Name: "exec", Args: `{"command":"curl -f https://example.com/data.csv"}`, Output: "curl: (6) Could not resolve host: example.com", IsError: true},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "Required network resource https://example.com/data.csv unreachable after retries: Could not resolve host on two attempts."
	if refusal := e.strictBlockedRefusal(run, reason); refusal != "" {
		t.Fatalf("network impossibility must be accepted, got refusal: %s", refusal)
	}
}

// Permission denial with history evidence is accepted.
func TestStrictBlockedAcceptsPermissionDenial(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"cat /root/secret"}`, Output: "cat: /root/secret: Permission denied", IsError: true},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "Required file /root/secret unreadable: Permission denied outside the agent's control."
	if refusal := e.strictBlockedRefusal(run, reason); refusal != "" {
		t.Fatalf("permission denial must be accepted, got: %s", refusal)
	}
}

// Missing tool with a failed install attempt is accepted.
func TestStrictBlockedAcceptsMissingWithFailedInstall(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"Rscript --version"}`, Output: "Rscript: command not found", IsError: true},
		{Name: "exec", Args: `{"command":"apt-get install -y r-base-core"}`, Output: "E: Unable to fetch archives: network is unreachable", IsError: true},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "Required tool Rscript missing (command not found) and not installable: apt-get failed, network is unreachable."
	if refusal := e.strictBlockedRefusal(run, reason); refusal != "" {
		t.Fatalf("missing+failed-install must be accepted, got: %s", refusal)
	}
}

// Operator-missing-data claims without validation language stay
// accepted (legacy compatibility).
func TestStrictBlockedAcceptsOperatorMissingData(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	e, run := strictRunWithHistory(nil, 3600)
	reason := "need /data/secret.csv from the operator"
	if refusal := e.strictBlockedRefusal(run, reason); refusal != "" {
		t.Fatalf("operator missing-data must stay accepted, got: %s", refusal)
	}
}

// No time remaining: even validation failures may block honestly.
func TestStrictBlockedAcceptsWhenNoTimeRemains(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"make test"}`, Output: "FAIL", IsError: true},
	}
	e, run := strictRunWithHistory(history, 100)
	run.HeartbeatAt = run.StartedAt.Add(100 * time.Second)
	reason := "implementation fails validation: make test FAIL"
	if refusal := e.strictBlockedRefusal(run, reason); refusal != "" {
		t.Fatalf("no time left must accept blocked, got: %s", refusal)
	}
}

// Switch off restores the legacy path-exists-only check.
func TestStrictBlockedSwitchOff(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "0")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"make test"}`, Output: "FAIL", IsError: true},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "implementation fails validation: make test FAIL"
	if refusal := e.strictBlockedRefusal(run, reason); refusal != "" {
		t.Fatalf("switch off must accept, got: %s", refusal)
	}
}

// End to end: validation-failure blocked is refused, counted, then an
// external impossibility is accepted and ends blocked.
func TestStrictBlockedEndToEndCountsRefusals(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("c1", "exec", `{"command":"cat /root/secret"}`)),
		toolResp(toolCall("f1", "finish", finishBlockedArgs("stuck", "tried", "implementation fails validation: test FAIL"))),
		toolResp(toolCall("f2", "finish", finishBlockedArgs("stuck", "tried cat", "Required file /root/secret unreadable: Permission denied outside the agent's control."))),
	}}
	runner := scriptedRunner()
	runner.add("exec", types.EffectReadOnly, types.IdemIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "cat: /root/secret: Permission denied"})
	// The finish tool itself runs through the normal pipeline.
	runner.add("finish", types.EffectReadOnly, types.IdemIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "ok"})
	e, js := testEngine(model, runner, passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	run.Profile.Limit = tasks.Limit{MaxSteps: SafetyMaxSteps, MaxSeconds: 3600}
	run.Profile.TimeBudgetOnly = true
	run.Tracker.Limit = run.Profile.Limit
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunBlocked)
	if run.FinishRefusals == 0 {
		t.Fatal("refusals must be counted")
	}
	if run.StrictBlockedRefusals == 0 {
		t.Fatal("strict refusals must be counted separately")
	}
	var _ = json.Marshal
}
