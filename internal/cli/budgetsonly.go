package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
)

// EffectiveBudgets is the resolved run-level budget for one
// non-interactive run (W8): wall guidance, provenance and hard safety caps.
// It is recorded in profile.json (so profile.sha256 moves with it) and
// in budgets.json next to the other trial artifacts.
type EffectiveBudgets struct {
	Deadline       bool    `json:"deadline"`
	TimeBudgetOnly bool    `json:"time_budget_only"`
	StrictBlocked  bool    `json:"strict_blocked"`
	WallSecs       int64   `json:"wall_secs"`
	WallSource     string  `json:"wall_source"`
	HardWallSecs   int64   `json:"hard_wall_secs"`
	MaxSteps       int     `json:"max_steps"`
	CostCapUSD     float64 `json:"cost_cap_usd"`
	CostSource     string  `json:"cost_source"`
	InputTokenCap  int64   `json:"input_token_cap"`
	SpendCaps      bool    `json:"spend_caps"`
	MaxTokens      int     `json:"max_tokens"`
	MaxToolCalls   int     `json:"max_tool_calls"`
	Profile        string  `json:"profile"`
}

// EffectiveBudgetsFor resolves the run-level budgets for opts/profile.
// In non-interactive runs (exec, delegate, eval) with
// EXCALIBUR_TIME_BUDGET_ONLY=on (default) the wall guidance comes from
// --timeout, else EXCALIBUR_EVAL_WALL_SECS, else an estimated 900 seconds.
// HardWallSecs is the enforced bound (4x for the fallback estimate).
// Steps are a safety cap (2000). The spend caps apply: USD cost
// (EXCALIBUR_COST_CAP_USD, default 20) and input tokens
// (EXCALIBUR_INPUT_TOKEN_CAP, default 8,000,000), unless
// EXCALIBUR_SPEND_CAP is off. Otherwise the legacy limits apply (steps 25
// default, explicit --max-steps wins, no cost cap unless --budget-usd is
// set).
func EffectiveBudgetsFor(opts ExecOptions, profile ProfileName) (result EffectiveBudgets) {
	defer func() {
		result.Deadline = engine.DeadlineEnabled()
		result.HardWallSecs = engine.HardWallSeconds(result.WallSecs, result.WallSource)
	}()
	// Every profile here is a non-interactive exec run, including
	// the default and delegate profiles. Master turns do not use this resolver.
	timeOnly := engine.TimeBudgetOnlyEnabled(true)
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
			wallSource = "explicit"
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
		InputTokenCap: engine.InputTokenCap(), SpendCaps: engine.SpendCapEnabled(),
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

// wallContext uses the hard safety bound, leaving WallSecs as model guidance.
// A parent deadline (the outer runner) always remains authoritative.
func (b EffectiveBudgets) wallContext(ctx context.Context, started time.Time) (context.Context, context.CancelFunc) {
	if b.HardWallSecs <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, started.Add(time.Duration(b.HardWallSecs)*time.Second))
}
