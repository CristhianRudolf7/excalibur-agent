package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if b.WallSecs != 900 || b.WallSource != "default" || engine.DefaultEvalWallSecs != 900 {
		t.Fatalf("fallback wall = %d/%s, want 900/default", b.WallSecs, b.WallSource)
	}
	if !b.SpendCaps || b.InputTokenCap != engine.DefaultInputTokenCap {
		t.Fatalf("spend caps = %v/%d, want on/30000000", b.SpendCaps, b.InputTokenCap)
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
	// The Harbor operator variable no longer sets the budget: the adapter
	// passes its timeout as --timeout, and the default applies otherwise.
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "1000")
	b3 := EffectiveBudgetsFor(ExecOptions{}, ProfileEval)
	if b3.WallSecs != 900 || b3.WallSource != "default" {
		t.Fatalf("harbor variable must not set the wall: %d/%s", b3.WallSecs, b3.WallSource)
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
	// All embedded exec profiles default to wall-clock-only budgets.
	t.Setenv(engine.TimeBudgetOnlySwitch, "")
	b2 := EffectiveBudgetsFor(ExecOptions{}, ProfileDefault)
	if !b2.TimeBudgetOnly {
		t.Fatal("default exec must use the wall clock")
	}
}

func TestBudgetsRecordedInProfileAndFiles(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "on")
	t.Setenv(engine.CostCapSwitch, "20")
	t.Setenv("EXCALIBUR_EVAL_WALL_SECS", "1000")
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "on")
	t.Setenv("HARBOR_AGENT_TIMEOUT_SEC", "")
	t.Setenv("EXCALIBUR_TIMEOUT_SEC", "")
	// Thirty alternating core-tool turns test the step limit without shell
	// startup variance. Unique file contents supply real progress; interleaved
	// clocks keep repetition detection independent of the limit under test.
	dir := t.TempDir()
	fake := providerfake.New("past-25")
	for i := 0; i < 30; i++ {
		if i%2 == 0 {
			path := filepath.Join(dir, fmt.Sprintf("input-%02d.txt", i))
			if err := os.WriteFile(path, []byte(fmt.Sprintf("observation %d", i)), 0600); err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(map[string]string{"path": path})
			fake.Responses = append(fake.Responses, providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("read-%d", i), Name: "read_file", Arguments: args}}}})
		} else {
			fake.Responses = append(fake.Responses, clockCallResponse(fmt.Sprintf("clock-%d", i)))
		}
	}
	fake.Responses = append(fake.Responses, doneImmediately()...)
	logs := t.TempDir()
	out, err := RunExec(ctx, ExecOptions{
		Request: "run many checks then finish", LogsDir: logs,
		Profile: ProfileEval, ProjectDir: dir, Provider: fake, TimeoutSecs: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State == types.RunPaused {
		t.Fatalf("time-only eval must not pause on the legacy step cap (past-25 must complete), got paused: %s", out.Summary)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed past 25 steps (%s)", out.State, out.Summary)
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

// Exercise the complete embedded loop, tool runner, context builder and
// records: progress must continue beyond both historical cutoff candidates.
func TestExecExceedsHistoricalCallLimits(t *testing.T) {
	t.Setenv(engine.TimeBudgetSwitch, "1")
	t.Setenv(engine.TimeBudgetOnlySwitch, "")
	for _, profile := range []ProfileName{ProfileDefault, ProfileDelegate, ProfileEval} {
		t.Run(string(profile), func(t *testing.T) {
			dir := t.TempDir()
			fake := providerfake.New("long-progress")
			for i := 0; i < 52; i++ {
				target := filepath.Join(dir, fmt.Sprintf("part-%03d.txt", i))
				args, _ := json.Marshal(map[string]string{"path": target, "content": fmt.Sprint(i)})
				fake.Responses = append(fake.Responses, providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: fmt.Sprint(i), Name: "write_file", Arguments: args}}}})
				readArgs, _ := json.Marshal(map[string]string{"path": target})
				fake.Responses = append(fake.Responses, providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("read-%d", i), Name: "read_file", Arguments: readArgs}}}})
			}
			fake.Responses = append(fake.Responses, execCallResponse("verify", "test $(find . -name 'part-*.txt' | wc -l) -eq 52"))
			fake.Responses = append(fake.Responses, finishScript("created all parts", "exec test confirmed 52 files")...)
			out, err := RunExec(context.Background(), ExecOptions{Request: "Create numbered text files and verify their count", ProjectDir: dir, LogsDir: t.TempDir(), Profile: profile, Provider: fake, TimeoutSecs: 600})
			if err != nil {
				t.Fatal(err)
			}
			if out.State != types.RunCompleted {
				t.Fatalf("state = %s summary = %s calls = %d", out.State, out.Summary, len(fake.ChatRequests))
			}
			if len(fake.ChatRequests) != 107 {
				t.Fatalf("model calls = %d", len(fake.ChatRequests))
			}
			for i, req := range fake.ChatRequests {
				if req.ToolChoice == "none" {
					t.Fatalf("early closing at step %d", i+1)
				}
			}
			raw, err := os.ReadFile(filepath.Join(out.Dir, "budgets.json"))
			if err != nil {
				t.Fatal(err)
			}
			var budgets EffectiveBudgets
			if err := json.Unmarshal(raw, &budgets); err != nil {
				t.Fatal(err)
			}
			if !budgets.TimeBudgetOnly || budgets.WallSecs != 600 {
				t.Fatalf("budgets = %+v", budgets)
			}
			// Per-trial tool records are written for the eval profile only;
			// other exec profiles keep the single custom trajectory.
			if profile != ProfileEval {
				return
			}
			raw, err = os.ReadFile(filepath.Join(out.Dir, "excalibur", "tool_calls.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if n := len(strings.Split(strings.TrimSpace(string(raw)), "\n")); n < 105 {
				t.Fatalf("recorded calls = %d", n)
			}
		})
	}
}
