package engine

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// Time-based run budgets and strict blocked policy (director's W8 design).
//
// In non-interactive profiles (exec, delegate, eval) the binding budget
// is the task wall-clock limit with closing mode at 90%; token and step
// limits are safety caps sized so they cannot bind before the wall
// clock. A run must never end paused in eval except on the hard wall
// clock. The strict blocked policy accepts `finish status=blocked` only
// with deterministic evidence of an external impossibility.

// TimeBudgetOnlySwitch gates the wall-clock-only binding budget.
const TimeBudgetOnlySwitch = "EXCALIBUR_TIME_BUDGET_ONLY"

// CostCapSwitch configures the per-trial safety cost cap in USD.
const CostCapSwitch = "EXCALIBUR_COST_CAP_USD"

// StrictBlockedSwitch gates the strict blocked policy.
const StrictBlockedSwitch = "EXCALIBUR_STRICT_BLOCKED"

// Safety caps: sized so they cannot bind before the wall clock under
// normal pricing and pacing.
const (
	// SafetyMaxSteps is the non-binding step safety cap in time-only mode.
	SafetyMaxSteps = 2000
	// DefaultCostCapUSD is the default per-trial safety cost cap.
	DefaultCostCapUSD = 20.0
	// DefaultEvalWallSecs is the arm wall budget when neither an
	// explicit --timeout nor a Harbor/operator override is known.
	DefaultEvalWallSecs = 3600
)

// EvalWallEnvNames configures the arm wall fallback.
var EvalWallEnvNames = []string{"EXCALIBUR_EVAL_WALL_SECS", "EXCALIBUR_ARM_TIMEOUT_SEC", "EXCALIBUR_ARM_WALL_SECS"}

// TimeOnlyClosingNote is the volatile note injected once when the wall
// clock enters closing mode: leave the best deliverable and finish.
const TimeOnlyClosingNote = "closing mode: time is almost exhausted; leave the best deliverable in place, verify what exists quickly, and call `finish` with evidence now. Do not start new work."

// SafetyCapNote is injected when a safety cap (steps/cost) would have
// paused the run in legacy mode: the wall clock still binds, so the run
// keeps working toward the wall edge instead of pausing.
const SafetyCapNote = "budget note: a safety cap (steps/cost) is reached but the wall clock still binds; keep working toward the wall edge, leave the best deliverable in place, and call `finish` with evidence."

// TimeBudgetOnlyEnabled reports whether wall-clock-only binding
// applies. An explicit env value wins; otherwise eval defaults ON and
// every other profile defaults OFF. When the time-budget plumbing
// itself is disabled (EXCALIBUR_TIME_BUDGET=0), time-only stays off so
// ablations restore the legacy silent baseline.
func TimeBudgetOnlyEnabled(isEval bool) bool {
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
	return isEval
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

// EvalWallFallback returns the configured arm wall budget in seconds:
// EXCALIBUR_EVAL_WALL_SECS (aliases EXCALIBUR_ARM_TIMEOUT_SEC,
// EXCALIBUR_ARM_WALL_SECS) when positive, else DefaultEvalWallSecs.
func EvalWallFallback() int64 {
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
	return DefaultEvalWallSecs
}

// EvalWallSecs resolves the effective eval wall budget and its source:
// the explicit flag first, else the Harbor/operator override (already
// the 90% closing value from ResolveMaxSeconds), else the configured
// arm fallback. Source is one of explicit, harbor, arm_default.
func EvalWallSecs(explicit int64) (int64, string) {
	if explicit > 0 {
		return explicit, "explicit"
	}
	if secs := ResolveMaxSeconds(0); secs > 0 {
		return secs, "harbor"
	}
	return EvalWallFallback(), "arm_default"
}

// timeOnlyStatus evaluates only the wall clock: OK, closing below 10%
// remainder, or exhausted at zero. Safety caps (steps/cost/tokens)
// never bind in this mode.
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
