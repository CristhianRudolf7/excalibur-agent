package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func execCallResponse(id, command string) providers.ChatResponse {
	args, _ := json.Marshal(map[string]string{"command": command})
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
		ToolCalls: []providers.ToolCall{{ID: id, Name: "exec", Arguments: args}}}}
}

func clockCallResponse(id string) providers.ChatResponse {
	args, _ := json.Marshal(map[string]any{})
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
		ToolCalls: []providers.ToolCall{{ID: id, Name: "clock", Arguments: args}}}}
}

func TestEffectiveBudgetsEvalTimeOnly(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "on")
	t.Setenv(engine.CostCapSwitch, "")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_EVAL_WALL_SECS", "")
	b := EffectiveBudgetsFor(ExecOptions{}, ProfileEval)
	if !b.TimeBudgetOnly {
		t.Fatal("eval time-only must be ON by default")
	}
	if b.WallSecs != engine.DefaultEvalWallSecs || b.WallSource != "arm_default" {
		t.Fatalf("fallback wall = %d/%s, want %d/arm_default", b.WallSecs, b.WallSource, engine.DefaultEvalWallSecs)
	}
	if b.MaxSteps != engine.SafetyMaxSteps {
		t.Fatalf("safety steps = %d, want %d", b.MaxSteps, engine.SafetyMaxSteps)
	}
	if b.CostCapUSD != engine.DefaultCostCapUSD || b.CostSource != "default" {
		t.Fatalf("cost cap = %v/%s, want 20/default", b.CostCapUSD, b.CostSource)
	}
	// Explicit timeout wins with explicit source.
	b2 := EffectiveBudgetsFor(ExecOptions{TimeoutSecs: 600}, ProfileEval)
	if b2.WallSecs != 600 || b2.WallSource != "explicit" {
		t.Fatalf("explicit wall = %d/%s", b2.WallSecs, b2.WallSource)
	}
	// Deadline-aware Harbor override keeps the full external timeout.
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "1000")
	b3 := EffectiveBudgetsFor(ExecOptions{}, ProfileEval)
	if b3.WallSecs != 1000 || b3.WallSource != "HARBOR_AGENT_TIMEOUT_SEC" {
		t.Fatalf("harbor wall = %d/%s, want 1000/HARBOR_AGENT_TIMEOUT_SEC", b3.WallSecs, b3.WallSource)
	}
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "")
	// Cost env override.
	t.Setenv(engine.CostCapSwitch, "35")
	b4 := EffectiveBudgetsFor(ExecOptions{}, ProfileEval)
	if b4.CostCapUSD != 35 || b4.CostSource != "env" {
		t.Fatalf("cost env = %v/%s", b4.CostCapUSD, b4.CostSource)
	}
	// Explicit --budget-usd wins.
	v := 7.5
	b5 := EffectiveBudgetsFor(ExecOptions{BudgetUSD: &v}, ProfileEval)
	if b5.CostCapUSD != 7.5 || b5.CostSource != "explicit" {
		t.Fatalf("explicit cost = %v/%s", b5.CostCapUSD, b5.CostSource)
	}
	// Explicit large MaxSteps is kept; small explicit is raised to safety.
	b6 := EffectiveBudgetsFor(ExecOptions{MaxSteps: 5000}, ProfileEval)
	if b6.MaxSteps != 5000 {
		t.Fatalf("large explicit steps must be kept, got %d", b6.MaxSteps)
	}
}

func TestEffectiveBudgetsLegacyWhenSwitchOff(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "0")
	b := EffectiveBudgetsFor(ExecOptions{MaxSteps: 25}, ProfileEval)
	if b.TimeBudgetOnly {
		t.Fatal("switch off must disable time-only")
	}
	if b.MaxSteps != 25 {
		t.Fatalf("legacy steps = %d, want 25", b.MaxSteps)
	}
	if b.CostCapUSD != 0 {
		t.Fatalf("legacy cost must be uncapped, got %v", b.CostCapUSD)
	}
	// Non-eval defaults off.
	t.Setenv(engine.TimeBudgetOnlySwitch, "")
	b2 := EffectiveBudgetsFor(ExecOptions{}, ProfileDefault)
	if b2.TimeBudgetOnly {
		t.Fatal("non-eval default must be OFF")
	}
}

func TestBudgetsRecordedInProfileAndFiles(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "on")
	t.Setenv(engine.CostCapSwitch, "20")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "1000")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	fake := providerfake.New("budgets-record")
	fake.Responses = doneImmediately()
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "hi", LogsDir: logs, Profile: ProfileEval, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q", out.State)
	}
	// profile.json carries the effective budgets so profile.sha256 moves with them.
	raw, err := os.ReadFile(filepath.Join(logs, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["time_budget_only"] != true {
		t.Fatalf("profile.json must record time_budget_only=true, got %v", rec["time_budget_only"])
	}
	if rec["wall_secs"] != float64(1000) {
		t.Fatalf("profile wall_secs = %v, want 1000", rec["wall_secs"])
	}
	if rec["max_steps"] != float64(engine.SafetyMaxSteps) {
		t.Fatalf("profile max_steps = %v, want %d", rec["max_steps"], engine.SafetyMaxSteps)
	}
	// budgets.json next to the profile plus mirrored under excalibur/.
	for _, p := range []string{
		filepath.Join(logs, "budgets.json"),
		filepath.Join(logs, "excalibur", "budgets.json"),
	} {
		buf, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("budgets.json missing at %s: %v", p, err)
		}
		var b EffectiveBudgets
		if err := json.Unmarshal(buf, &b); err != nil {
			t.Fatal(err)
		}
		if !b.TimeBudgetOnly || b.WallSecs != 1000 || b.MaxSteps != engine.SafetyMaxSteps {
			t.Fatalf("budgets.json wrong: %+v", b)
		}
	}
	// explicit_finish.json counts strict refusals (zero here) and pins switches.
	efRaw, err := os.ReadFile(filepath.Join(out.Dir, "explicit_finish.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ef map[string]any
	if err := json.Unmarshal(efRaw, &ef); err != nil {
		t.Fatal(err)
	}
	if ef["time_budget_only"] != true {
		t.Fatalf("explicit_finish must pin time_budget_only, got %v", ef["time_budget_only"])
	}
	if ef["strict_blocked"] != true {
		t.Fatalf("explicit_finish must pin strict_blocked, got %v", ef["strict_blocked"])
	}
}

// Eval with time-only must not pause on the old 25-step cap: a script
// needing more than 25 model calls completes instead of pausing. This
// reproduces the measured early pause (25/74 paused after ~2 minutes).
func TestEvalTimeOnlySurvivesPast25Steps(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "on")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	// 30 alternating turns then finish: legacy 25-step cap would pause
	// at step 25, time-only safety (2000) lets it complete. Commands
	// cycle through materially different shapes so the near-repeat
	// and no-information loop rules stay silent.
	cmds := []string{
		"pwd; ls -la",
		"cat /etc/hosts",
		"uname -a; date -u",
		"echo alpha-$(date +%s%N)",
		"ls /tmp; df -h . | head",
	}
	fake := providerfake.New("past-25")
	for i := 0; i < 30; i++ {
		if i%2 == 0 {
			fake.Responses = append(fake.Responses, execCallResponse("cx"+strings.Repeat("x", i+1), cmds[(i/2)%len(cmds)]))
		} else {
			fake.Responses = append(fake.Responses, clockCallResponse("ck"+strings.Repeat("k", i+1)))
		}
	}
	fake.Responses = append(fake.Responses, doneImmediately()...)
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "run many checks then finish", LogsDir: logs,
		Profile: ProfileEval, ProjectDir: t.TempDir(), Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State == types.RunPaused {
		t.Fatalf("time-only eval must not pause on the legacy step cap (past-25 must complete), got paused: %s", out.Summary)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed past 25 steps", out.State)
	}
	sys := systemText(fake.ChatRequests[0])
	if !strings.Contains(sys, "Closing mode: when time is almost exhausted") {
		t.Fatalf("closing discipline must ride the prompt, got:\n%s", sys)
	}
	if !strings.Contains(sys, "Blocked policy:") {
		t.Fatalf("strict-blocked discipline must ride the prompt, got:\n%s", sys)
	}
}

// Premature blocked while time remains is refused: the agent fails a
// check, tries `finish blocked` with the validation failure, is sent
// back with three approaches, fixes, and completes. This reproduces
// the measured 21-22/74 blocked-after-one-validation runs.
func TestStrictBlockedRefusedThenCompleted(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "on")
	t.Setenv(engine.StrictBlockedSwitch, "on")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	fake := providerfake.New("strict-cli")
	// A probe failure is a normal answer, so the strict-blocked path is
	// still the one refusing the claim (a failing non-probe command
	// would be refused first by the last-command-errored check).
	failArgs, _ := json.Marshal(map[string]string{"command": "test -f /nonexistent/check-file"})
	okArgs, _ := json.Marshal(map[string]string{"command": "true"})
	blockedBad, _ := json.Marshal(map[string]string{
		"summary": "stuck", "evidence": "tried", "status": "blocked",
		"reason": "implementation fails validation: test FAIL",
	})
	blockedGood := finishScript("fixed", "true exit 0")[0].Message.ToolCalls[0].Arguments
	_ = blockedGood
	finishGood := finishScript("fixed check", "true exit 0")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "exec", Arguments: failArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "f1", Name: "finish", Arguments: blockedBad}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c2", Name: "exec", Arguments: okArgs}}}},
	}
	fake.Responses = append(fake.Responses, finishGood...)
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "fix the check", LogsDir: logs,
		Profile: ProfileEval, ProjectDir: t.TempDir(), Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("validation-failure blocked must be refused then complete, got %q (%s)", out.State, out.Summary)
	}
	raw, err := os.ReadFile(filepath.Join(out.Dir, "explicit_finish.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["strict_blocked_refusals"] == float64(0) || rec["strict_blocked_refusals"] == nil {
		t.Fatalf("strict refusals must be counted, got %v", rec)
	}
	// Switch off restores legacy acceptance (documents the before-fix outcome).
	t.Setenv(engine.StrictBlockedSwitch, "0")
	fake2 := providerfake.New("strict-cli-off")
	fake2.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "f1", Name: "finish", Arguments: blockedBad}}}},
	}
	out2, err := RunExec(context.Background(), ExecOptions{
		Request: "fix the check", LogsDir: t.TempDir(),
		Profile: ProfileEval, ProjectDir: t.TempDir(), Provider: fake2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out2.State != types.RunBlocked {
		t.Fatalf("switch off must accept the legacy blocked, got %q", out2.State)
	}
}
