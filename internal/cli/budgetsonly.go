package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
)

// EffectiveBudgets is the resolved run-level budget for one
// non-interactive run (W8): the binding wall clock plus safety caps.
// It is recorded in profile.json (so profile.sha256 moves with it) and
// in budgets.json next to the other trial artifacts.
type EffectiveBudgets struct {
	Deadline       bool    `json:"deadline"`
	TimeBudgetOnly bool    `json:"time_budget_only"`
	StrictBlocked  bool    `json:"strict_blocked"`
	WallSecs       int64   `json:"wall_secs"`
	WallSource     string  `json:"wall_source"`
	MaxSteps       int     `json:"max_steps"`
	CostCapUSD     float64 `json:"cost_cap_usd"`
	CostSource     string  `json:"cost_source"`
	MaxTokens      int     `json:"max_tokens"`
	MaxToolCalls   int     `json:"max_tool_calls"`
	Profile        string  `json:"profile"`
}

// EffectiveBudgetsFor resolves the run-level budgets for opts/profile:
// in eval with EXCALIBUR_TIME_BUDGET_ONLY=on (default) the binding
// budget is the task wall clock (explicit --timeout, else the Harbor
// 90% override, else the configured arm fallback) with safety caps
// (steps 2000, cost EXCALIBUR_COST_CAP_USD default 20); otherwise the
// legacy limits apply (steps 25 default, explicit --max-steps wins, no
// cost cap unless --budget-usd is set).
func EffectiveBudgetsFor(opts ExecOptions, profile ProfileName) (result EffectiveBudgets) {
	defer func() {
		result.Deadline = engine.DeadlineEnabled()
		result.WallSecs, result.WallSource = engine.ResolveDeadline(opts.TimeoutSecs, opts.Request, result.WallSecs, result.WallSource)
	}()
	isEval := profile == ProfileEval
	timeOnly := engine.TimeBudgetOnlyEnabled(isEval)
	strict := engine.StrictBlockedEnabled()
	if !timeOnly {
		steps := 25
		if opts.MaxSteps > 0 {
			steps = opts.MaxSteps
		}
		var cost float64
		costSource := "none"
		if opts.BudgetUSD != nil && *opts.BudgetUSD > 0 {
			cost = *opts.BudgetUSD
			costSource = "explicit"
		}
		var wall int64
		wallSource := "none"
		if opts.TimeoutSecs > 0 {
			wall = opts.TimeoutSecs
			wallSource = "explicit"
		} else if w := engine.ResolveMaxSeconds(0); w > 0 {
			wall = w
			wallSource = "harbor"
		}
		return EffectiveBudgets{
			TimeBudgetOnly: false, StrictBlocked: strict,
			WallSecs: wall, WallSource: wallSource,
			MaxSteps: steps, CostCapUSD: cost, CostSource: costSource,
			MaxTokens: 0, MaxToolCalls: 0, Profile: string(profile),
		}
	}
	// Time-only mode: wall binds, steps/cost are safety caps that
	// cannot bind before the wall clock.
	wall, wallSource := engine.EvalWallSecs(opts.TimeoutSecs)
	steps := engine.SafetyMaxSteps
	if opts.MaxSteps > steps {
		steps = opts.MaxSteps
	}
	var cost float64
	costSource := "default"
	if opts.BudgetUSD != nil && *opts.BudgetUSD > 0 {
		cost = *opts.BudgetUSD
		costSource = "explicit"
	} else {
		cost = engine.CostCapUSD()
		if raw, ok := os.LookupEnv(engine.CostCapSwitch); ok && strings.TrimSpace(raw) != "" {
			// Only credit the env when it actually parses positive;
			// junk/empty falls back to the default source.
			if f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && f > 0 {
				costSource = "env"
			}
		}
	}
	return EffectiveBudgets{
		TimeBudgetOnly: true, StrictBlocked: strict,
		WallSecs: wall, WallSource: wallSource,
		MaxSteps: steps, CostCapUSD: cost, CostSource: costSource,
		MaxTokens: 0, MaxToolCalls: 0, Profile: string(profile),
	}
}

// writeBudgetsRecord persists the effective budgets for the ablation
// (budgets.json next to the other trial artifacts). A missing dir
// skips the write; failures never fail the run.
func writeBudgetsRecord(dir string, b EffectiveBudgets) {
	if dir == "" {
		return
	}
	buf, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "budgets.json"), append(buf, '\n'), 0o644)
}
