package engine

import (
	"encoding/json"
	"fmt"
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
			Seq:       int64(i + 1),
			Role:      providers.RoleTool,
			Content:   h.Output,
			ToolCalls: []providers.ToolCall{{ID: fmt.Sprint(i), Name: h.Name, Arguments: jsonRaw(h.Args)}},
			ToolResult: &providers.ToolResult{
				CallID: fmt.Sprint(i), Name: h.Name, Output: h.Output, IsError: h.IsError,
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
	for _, want := range []string{"strict blocked", "no edit, write or install"} {
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
		{Name: "write_file", Args: `{"path":"/tmp/client.txt"}`, Output: "ok"},
		{Name: "exec", Args: `{"command":"curl -f https://example.com/data.csv"}`, Output: "curl: (6) Could not resolve host: example.com", IsError: true},
		{Name: "exec", Args: `{"command":"curl -f https://example.com/data.csv"}`, Output: "curl: (6) Could not resolve host: example.com", IsError: true},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "curl -f https://example.com/data.csv failed: curl: (6) Could not resolve host: example.com"
	if refusal := e.strictBlockedRefusal(run, reason); refusal != "" {
		t.Fatalf("network impossibility must be accepted, got refusal: %s", refusal)
	}
}

// Permission denial with history evidence is accepted.
func TestStrictBlockedAcceptsPermissionDenial(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "write_file", Args: `{"path":"/tmp/client.txt"}`, Output: "ok"},
		{Name: "exec", Args: `{"command":"cat /root/secret"}`, Output: "exit: 1\ncat: /root/secret: Permission denied", IsError: true},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "cat /root/secret failed: cat: /root/secret: Permission denied"
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
	reason := "apt-get install -y r-base-core failed: E: Unable to fetch archives: network is unreachable"
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
	if refusal := e.strictBlockedRefusal(run, reason); !strings.Contains(refusal, "no edit, write or install") {
		t.Fatalf("missing-data claim without an attempt must refuse, got: %s", refusal)
	}
}

// Closing mode (no time remains): blocked is never accepted (W10
// run-end fix). Near the time limit the run must end with `finish`
// status=completed and the best deliverable in place, never blocked.
func TestStrictBlockedAcceptsWhenNoTimeRemains(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"make test"}`, Output: "FAIL", IsError: true},
	}
	e, run := strictRunWithHistory(history, 100)
	run.HeartbeatAt = run.StartedAt.Add(100 * time.Second)
	reason := "implementation fails validation: make test FAIL"
	if refusal := e.strictBlockedRefusal(run, reason); refusal == "" {
		t.Fatal("closing mode must refuse blocked (must complete with best deliverable)")
	} else if !strings.Contains(strings.ToLower(refusal), "closing mode") {
		t.Fatalf("closing refusal must name closing mode, got: %s", refusal)
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

// W10 regression from v9-default qemu-alpine-ssh: the agent started
// QEMU, waited, killed it itself (`kill 366`), then called `finish`
// status=blocked claiming the VM and SSH were unavailable. The old
// policy accepted with 0 refusals because the reason carried no
// validation keywords. It must be refused: self-caused
// unavailability is never external.
func TestStrictBlockedRefusesSelfStoppedQemu(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"qemu-system-x86_64 -machine accel=tcg -m 512 -cdrom /app/alpine.iso -boot d -nographic -monitor none -serial stdio -netdev user,id=n1,hostfwd=tcp::2222-:22 -device e1000,netdev=n1"}`, Output: "exit: running session: sess-3 alive: true", IsError: false},
		{Name: "exec", Args: `{"command":"ps -eo pid,args | grep '[q]emu-system'"}`, Output: "366 qemu-system-x86_64 -machine accel=tcg", IsError: false},
		{Name: "exec", Args: `{"command":"kill 366; sleep 1;"}`, Output: "exit: 0", IsError: false},
	}
	e, run := strictRunWithHistory(history, 3600)
	reason := "Alpine live ISO only reached its boot prompt and port 2222 was not serving SSH (connection timed out); QEMU was stopped, so the requested persistent VM and password-authenticated shell are not available."
	if refusal := e.strictBlockedRefusal(run, reason); refusal == "" {
		t.Fatal("self-stopped QEMU blocked must be refused (was accepted with 0 refusals)")
	} else if !strings.Contains(strings.ToLower(refusal), "strict blocked") {
		t.Fatalf("refusal must name strict blocked, got: %s", refusal)
	}
	if !isSelfCausedUnavailability(reason, history) {
		t.Fatal("qemu shape must be detected as self-caused")
	}
}

// A blocked claim is refused while a named deliverable can still be
// created and no call has touched it (D7 rule b). The rule is general:
// it reads the instruction's output path and the history, nothing else.
func TestStrictBlockedRefusesUntouchedDeliverable(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	dir := t.TempDir()
	history := []tasks.HistoryTool{
		{Name: "exec", Args: `{"command":"ls /app"}`, Output: "exit: 0  alive: false"},
		{Name: "exec", Args: `{"command":"mkdir -p /tmp/build"}`, Output: "exit: 0", IsError: false},
	}
	e, run := strictRunWithHistory(history, 3600)
	run.Request = "Create /tmp/strict-untouched-report.txt with the summary."
	run.Workdir = dir
	reason := "the environment could not produce the report"
	refusal := e.strictBlockedRefusal(run, reason)
	if !strings.Contains(refusal, "/tmp/strict-untouched-report.txt") {
		t.Fatalf("untouched creatable deliverable must refuse blocked, got %q", refusal)
	}
}

// End to end: validation-failure blocked is refused, counted, then an
// external impossibility is accepted and ends blocked.
func TestStrictBlockedEndToEndCountsRefusals(t *testing.T) {
	t.Setenv(StrictBlockedSwitch, "on")
	ctx := finishTestContext(t)
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("w0", "write_file", `{"path":"/tmp/client.txt"}`)),
		toolResp(toolCall("c1", "exec", `{"command":"cat /root/secret"}`)),
		toolResp(toolCall("f1", "finish", finishBlockedArgs("stuck", "tried", "implementation fails validation: test FAIL"))),
		toolResp(toolCall("c2", "exec", `{"command":"cat /root/secret"}`)),
		toolResp(toolCall("f2", "finish", finishBlockedArgs("stuck", "tried cat", "cat /root/secret failed: cat: /root/secret: Permission denied"))),
	}}
	runner := scriptedRunner()
	runner.add("write_file", types.EffectWriteLocal, types.IdemIdempotent)
	runner.add("exec", types.EffectReadOnly, types.IdemIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "exit: 1\ncat: /root/secret: Permission denied"})
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
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }
