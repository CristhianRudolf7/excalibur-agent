package engine

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Legacy time-budget hooks (Sections 7.3, 18.4, 23.2, Appendix B).
// Non-interactive time-only runs resolve their real wall budget through
// EvalWallSecs instead of the legacy closing factor below.
//
// Harbor never forwards the task timeout into the container (it kills
// the agent host-side), so the harness reads the operator override
// HARBOR_AGENT_TIMEOUT_SEC/EXCALIBUR_TIMEOUT_SEC and applies the
// Section 23.2 90% closing budget when no explicit --timeout is set.
// Every model call then carries a compact remaining-time line in L5
// (BudgetState), and the existing tracker trips closing mode below
// 10% and pauses with the budget motive at zero. All of it is gated
// by EXCALIBUR_TIME_BUDGET (default ON) for the ablation protocol.

// TimeBudgetSwitch gates the Harbor/config time-limit plumbing.
const TimeBudgetSwitch = "EXCALIBUR_TIME_BUDGET"

// HarborTimeoutEnvs carries the operator timeout override in probe
// order (docs/decisions/0037).
var HarborTimeoutEnvs = []string{"HARBOR_AGENT_TIMEOUT_SEC", "EXCALIBUR_TIMEOUT_SEC"}

// ClosingBudgetShare is the Section 23.2 share of the task timeout
// the harness may spend before closing (90%).
const ClosingBudgetShare = 0.9

// TimeBudgetEnabled reports whether the time-budget plumbing applies.
func TimeBudgetEnabled() bool {
	v, ok := os.LookupEnv(TimeBudgetSwitch)
	if !ok || strings.TrimSpace(v) == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// ResolveMaxSeconds returns the effective wall-time budget in seconds:
// the explicit flag first, else 90% of the Harbor/operator override,
// else 0 (no wall budget). Non-positive and unparseable values are
// ignored. With the switch off the whole plumbing is inert (0), so
// ablation runs fall back to the pre-existing context deadline only.
func ResolveMaxSeconds(explicit int64) int64 {
	if !TimeBudgetEnabled() {
		return 0
	}
	if explicit > 0 {
		return explicit
	}
	for _, key := range HarborTimeoutEnvs {
		raw, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		secs, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || secs <= 0 {
			continue
		}
		if eff := int64(secs * ClosingBudgetShare); eff > 0 {
			return eff
		}
	}
	return 0
}

// FormatRemaining renders a compact remaining-time fragment for L5:
// "3h12m left", "42m left", "95s left", "overdue 5s". Durations round
// down so the line never promises more time than exists.
func FormatRemaining(d time.Duration) string {
	if d < 0 {
		return "overdue " + formatSpan(-d)
	}
	return formatSpan(d) + " left"
}

func formatSpan(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d / time.Second)
	switch {
	case secs >= 3600:
		return fmt.Sprintf("%dh%dm", secs/3600, (secs%3600)/60)
	case secs >= 120:
		return fmt.Sprintf("%dm", secs/60)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// BudgetLine renders the compact one-line budget snapshot for L5
// (Volatile.BudgetState): steps, tool calls, remaining wall time and
// spend, omitting every segment without a cap. Empty when nothing is
// capped, so uncapped runs add no volatile bytes.
func (r *Run) BudgetLine(now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	used := r.Tracker.Used
	limit := r.Tracker.Limit
	var parts []string
	if limit.MaxSteps > 0 {
		parts = append(parts, fmt.Sprintf("steps %d/%d", used.Steps, limit.MaxSteps))
	}
	if limit.MaxToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("tools %d/%d", used.ToolCalls, limit.MaxToolCalls))
	}
	if limit.MaxTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens %d/%d", used.InputTokens+used.OutputTokens, limit.MaxTokens))
	}
	if limit.MaxCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("cost $%.4g/$%.4g", used.CostUSD, limit.MaxCostUSD))
	}
	if limit.MaxSeconds > 0 {
		elapsed := now.Sub(r.StartedAt)
		if elapsed < 0 {
			elapsed = 0
		}
		left := time.Duration(limit.MaxSeconds)*time.Second - elapsed
		parts = append(parts, fmt.Sprintf("time %s of %s", FormatRemaining(left), formatSpan(time.Duration(limit.MaxSeconds)*time.Second)))
	}
	return strings.Join(parts, ", ")
}
