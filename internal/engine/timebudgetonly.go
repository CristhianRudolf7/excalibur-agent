package engine

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// Time-based run budgets and strict blocked policy.
//
// In non-interactive runs (exec, delegate, eval) the binding budget is
// the wall-clock limit of the run; token and step limits are safety
// caps sized so they cannot bind before the wall clock. The spend caps
// (USD cost and input tokens) are the exception: they are enforced in
// time-only runs as well, with a closing note at 100 percent and a
// paused stop at 125 percent. A run never ends paused except on the
// wall clock or a spend cap. The strict blocked policy accepts
// `finish status=blocked` only with deterministic evidence of an
// external impossibility.

// TimeBudgetOnlySwitch gates the wall-clock-only binding budget.
const TimeBudgetOnlySwitch = "EXCALIBUR_TIME_BUDGET_ONLY"

// CostCapSwitch configures the per-trial spend cap in USD.
const CostCapSwitch = "EXCALIBUR_COST_CAP_USD"

// SpendCapSwitch gates the spend caps in time-only runs: the USD cost
// cap and the input-token cap (default ON).
const SpendCapSwitch = "EXCALIBUR_SPEND_CAP"

// InputTokenCapSwitch configures the per-run input-token cap.
const InputTokenCapSwitch = "EXCALIBUR_INPUT_TOKEN_CAP"

// StrictBlockedSwitch gates the strict blocked policy.
const StrictBlockedSwitch = "EXCALIBUR_STRICT_BLOCKED"

// Safety caps and budget defaults.
const (
	// SafetyMaxSteps is the non-binding step safety cap in time-only mode.
	SafetyMaxSteps = 2000
	// DefaultCostCapUSD is the default per-trial spend cap.
	DefaultCostCapUSD = 20.0
	// DefaultInputTokenCap is the default per-run input-token cap.
	DefaultInputTokenCap = 30_000_000
	// DefaultEvalWallSecs is the wall budget of a non-interactive run
	// with neither an explicit --timeout nor EXCALIBUR_EVAL_WALL_SECS:
	// a guidance estimate when the outer deadline is unknown.
	DefaultEvalWallSecs = 900
	// SpendCloseShare is the share of a spend cap at which the agent
	// is asked to finish within a few turns.
	SpendCloseShare = 1.0
	// SpendStopShare is the share of a spend cap at which the run
	// stops as paused with the budget motive.
	SpendStopShare = 1.25
)

// EvalWallEnvNames are the variables that set the wall budget of a
// non-interactive run when no --timeout is given; the first positive
// value wins.
var EvalWallEnvNames = []string{"EXCALIBUR_EVAL_WALL_SECS"}

// TimeOnlyClosingNote is the volatile note injected once when the wall
// clock enters closing mode: leave the best deliverable and finish.
const TimeOnlyClosingNote = "closing mode: time is almost exhausted; leave the best deliverable in place, verify what exists quickly, and call `finish` with evidence now. Do not start new work."

// SafetyCapNote is injected when a step safety cap would have paused
// the run in legacy mode: the wall clock still binds, so the run keeps
// working toward the wall edge instead of pausing.
const SafetyCapNote = "budget note: a safety cap (steps) is reached but the wall clock still binds; keep working toward the wall edge, leave the best deliverable in place, and call `finish` with evidence."

// SpendCloseNote is injected once when a spend cap (USD or input
// tokens) reaches its closing share.
const SpendCloseNote = "budget note: a spend cap is reached; finish within the next three turns. Leave the best deliverable in place, verify it, and call `finish` with evidence. Do not start new work."

// TimeBudgetOnlyEnabled reports whether wall-clock-only binding
// applies. An explicit env value wins; otherwise non-interactive runs
// default ON and interactive turns default OFF. When the time-budget
// plumbing itself is disabled (EXCALIBUR_TIME_BUDGET=0), time-only
// stays off so ablations restore the legacy silent baseline.
func TimeBudgetOnlyEnabled(nonInteractive bool) bool {
	if !TimeBudgetEnabled() {
		return false
	}
	if raw, ok := os.LookupEnv(TimeBudgetOnlySwitch); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return nonInteractive
}

// StrictBlockedEnabled reports whether the strict blocked policy
// applies. Default ON; set EXCALIBUR_STRICT_BLOCKED=0/off/false/no to
// restore the legacy path-exists-only check.
func StrictBlockedEnabled() bool {
	raw, ok := os.LookupEnv(StrictBlockedSwitch)
	if !ok || strings.TrimSpace(raw) == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// CostCapUSD parses EXCALIBUR_COST_CAP_USD; unset, unparseable or
// non-positive values fall back to DefaultCostCapUSD.
func CostCapUSD() float64 {
	raw, ok := os.LookupEnv(CostCapSwitch)
	if !ok || strings.TrimSpace(raw) == "" {
		return DefaultCostCapUSD
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || f <= 0 {
		return DefaultCostCapUSD
	}
	return f
}

// InputTokenCap parses EXCALIBUR_INPUT_TOKEN_CAP; unset, unparseable or
// non-positive values fall back to DefaultInputTokenCap.
func InputTokenCap() int64 {
	raw, ok := os.LookupEnv(InputTokenCapSwitch)
	if !ok || strings.TrimSpace(raw) == "" {
		return DefaultInputTokenCap
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return DefaultInputTokenCap
	}
	return n
}

// SpendCapEnabled reports whether the time-only spend caps (USD cost
// and input tokens) apply. Default ON; EXCALIBUR_SPEND_CAP=0 turns them
// off so only the wall clock binds.
func SpendCapEnabled() bool { return envDefaultOn(SpendCapSwitch) }

// envDefaultOn reads one default-ON switch: unset, empty or any value
// other than 0, false, no or off keeps the mechanism on.
func envDefaultOn(name string) bool {
	raw, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(raw) == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// EvalWallEnvSecs returns the wall budget set through EvalWallEnvNames
// in seconds, or 0 when none is set to a positive value.
func EvalWallEnvSecs() int64 {
	for _, key := range EvalWallEnvNames {
		raw, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		if secs, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && secs > 0 {
			return secs
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && f > 0 {
			return int64(f)
		}
	}
	return 0
}

// EvalWallSecs resolves the wall budget of a non-interactive run and
// its source, in this order: the explicit --timeout given to the
// process, EXCALIBUR_EVAL_WALL_SECS, then
// DefaultEvalWallSecs. Source is explicit (flag or valid environment) or
// default. The fallback is guidance; HardWallSeconds resolves its safety cap.
func EvalWallSecs(explicit int64) (int64, string) {
	if explicit > 0 {
		return explicit, "explicit"
	}
	if secs := EvalWallEnvSecs(); secs > 0 {
		return secs, "explicit"
	}
	return DefaultEvalWallSecs, "default"
}

// timeOnlyStatus evaluates only the wall clock: OK, closing below 10%
// remainder, or exhausted at zero. Safety caps never bind in this mode.
func timeOnlyStatus(limit tasks.Limit, elapsed time.Duration) tasks.Status {
	if limit.MaxSeconds <= 0 {
		return tasks.StatusOK
	}
	total := time.Duration(limit.MaxSeconds) * time.Second
	if elapsed >= total {
		return tasks.StatusExhausted
	}
	left := total - elapsed
	if left < time.Duration(tasks.ClosingFraction*float64(limit.MaxSeconds)*float64(time.Second)) {
		return tasks.StatusClosing
	}
	return tasks.StatusOK
}

// timeOnlyExhaustedBy names only the wall-clock exhaustion for the
// honest paused report in time-only mode.
func timeOnlyExhaustedBy(limit tasks.Limit, elapsed time.Duration) []string {
	if limit.MaxSeconds > 0 && elapsed >= time.Duration(limit.MaxSeconds)*time.Second {
		return []string{"time"}
	}
	return nil
}

// spendLevel reports how far the run is into its spend caps in
// time-only mode: 0 below the closing share, 1 at or above it, 2 at or
// above the stop share. over names the caps at the stop share ("cost",
// "input_tokens"). It is 0 with the spend caps switched off or outside
// time-only mode. Callers hold no lock.
func (r *Run) spendLevel() (level int, over []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Profile.TimeBudgetOnly || !SpendCapEnabled() {
		return 0, nil
	}
	check := func(name string, used, capacity float64) {
		if capacity <= 0 {
			return
		}
		ratio := used / capacity
		if ratio >= SpendCloseShare && level < 1 {
			level = 1
		}
		if ratio >= SpendStopShare {
			over = append(over, name)
			level = 2
		}
	}
	check("cost", r.Tracker.Used.CostUSD, r.Tracker.Limit.MaxCostUSD)
	// A finite, computable cost cap is the primary spend guard. The
	// token cap is a fallback for unpriced usage or an unavailable cost cap.
	primary, set := envSwitch("EXCALIBUR_COST_PRIMARY_SPEND")
	cost := r.Tracker.Used.CostUSD
	cap := r.Tracker.Limit.MaxCostUSD
	priced := !r.unpricedUsage && cost > 0 && !math.IsInf(cost, 0) && !math.IsNaN(cost)
	computable := cap > 0 && !math.IsInf(cap, 0) && !math.IsNaN(cap)
	if (set && !primary) || !priced || !computable {
		check("input_tokens", float64(r.Tracker.Used.InputTokens), float64(r.inputTokenCap))
	}
	return level, over
}

// TimeOnlyForRecord reports the run's time-only flag for trial
// records. Callers hold no lock; the run is quiescent when drivers
// read it.
func (r *Run) TimeOnlyForRecord() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Profile.TimeBudgetOnly
}

// StrictBlockedRefusalsForRecord reports strict-blocked refusals.
func (r *Run) StrictBlockedRefusalsForRecord() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.StrictBlockedRefusals
}

// ClosingNotedForRecord reports whether the one-time wall-closing or
// safety-cap note fired.
func (r *Run) ClosingNotedForRecord() (closing, safety bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.timeOnlyClosingNoted, r.safetyCapNoted
}

// observeUsageLocked tracks missing prices across calls; a later priced call
// cannot make earlier unpriced consumption known. The caller holds r.mu.
func (r *Run) observeUsageLocked(usage tasks.Usage) {
	if (usage.InputTokens > 0 || usage.OutputTokens > 0) && (usage.CostUSD <= 0 || math.IsNaN(usage.CostUSD) || math.IsInf(usage.CostUSD, 0)) {
		r.unpricedUsage = true
	}
	r.Tracker.Observe(usage)
}
