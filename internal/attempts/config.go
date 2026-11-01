package attempts

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// MaxAttempts caps the fan-out so a misconfigured environment cannot
// explode the container budget.
const MaxAttempts = 8

// DefaultAttempts is the off state: a single attempt, the legacy path.
const DefaultAttempts = 1

// Config controls the best-of-N fan-out.
type Config struct {
	// N is the number of independent attempts (1 means off).
	N int
	// MaxConcurrency bounds simultaneous attempts.
	MaxConcurrency int
}

// Enabled reports whether the fan-out applies.
func (c Config) Enabled() bool { return c.N > 1 }

// ConfigFromEnv reads EXCALIBUR_ATTEMPTS (1 means off, the default
// until measured). Unset, unparseable or out-of-range values fall back
// to a single attempt; values above MaxAttempts clamp down.
func ConfigFromEnv() Config {
	raw, ok := os.LookupEnv("EXCALIBUR_ATTEMPTS")
	if !ok || strings.TrimSpace(raw) == "" {
		return Config{N: DefaultAttempts, MaxConcurrency: 1}
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return Config{N: DefaultAttempts, MaxConcurrency: 1}
	}
	if n > MaxAttempts {
		n = MaxAttempts
	}
	return Config{N: n, MaxConcurrency: concurrencyFor(n)}
}

// ConfigFromValue builds the same config from an explicit N, clamping
// to the valid range. Tests use it to avoid environment coupling.
func ConfigFromValue(n int) Config {
	if n < 1 {
		n = 1
	}
	if n > MaxAttempts {
		n = MaxAttempts
	}
	return Config{N: n, MaxConcurrency: concurrencyFor(n)}
}

func concurrencyFor(n int) int {
	if n <= 1 {
		return 1
	}
	cpus := runtime.NumCPU()
	if cpus < 1 {
		cpus = 1
	}
	if cpus > n {
		cpus = n
	}
	return cpus
}

// DivideSteps shares the task step budget across attempts: each
// attempt receives ceil(effective/N) with a floor of 1. A
// non-positive parent means the engine default (25).
//
// It is kept for the legacy split path (EXCALIBUR_ATTEMPTS_FULL_BUDGET=0).
// The default path gives every attempt the full budget so each runs to
// its own verification instead of pausing early on a slice.
func DivideSteps(parentMaxSteps, n int) int {
	if n < 1 {
		n = 1
	}
	eff := parentMaxSteps
	if eff <= 0 {
		eff = 25
	}
	per := (eff + n - 1) / n
	if per < 1 {
		per = 1
	}
	return per
}

// DivideBudget shares an explicit USD budget across attempts. A nil
// parent stays nil (uncapped); otherwise each attempt is capped at
// parent/N. The sum of attempt caps never exceeds the parent.
//
// It is kept for the legacy split path (EXCALIBUR_ATTEMPTS_FULL_BUDGET=0).
// The default path gives every attempt the full budget.
func DivideBudget(parent *float64, n int) *float64 {
	if parent == nil || n <= 1 {
		return parent
	}
	per := *parent / float64(n)
	out := per
	return &out
}

// FullBudgetSwitch gates giving every attempt the full parent budget
// (steps and explicit USD) so each runs to its own verification.
// Default ON: the legacy split starves attempts (parent 25 with N=3
// gives 9 steps each, pausing before verification; observed as half
// of real trials ending paused with empty outer tool records).
// Set EXCALIBUR_ATTEMPTS_FULL_BUDGET=0/off/false/no to restore the
// legacy ceil(parent/N) split for ablation.
const FullBudgetSwitch = "EXCALIBUR_ATTEMPTS_FULL_BUDGET"

// FullBudgetEnabled reports whether attempts get the full budget.
func FullBudgetEnabled() bool { return switchDefaultOn(FullBudgetSwitch) }

// MergeRecordsSwitch gates merging every attempt's model/tool calls
// into the outer trial records tagged by attempt. Default ON: without
// it the outer excalibur/tool_calls.jsonl and model_calls.jsonl stay
// empty (all calls live only under attempts/attempt-i), so standard
// evaluation and auditing sees zero work. Set
// EXCALIBUR_ATTEMPTS_MERGE_RECORDS=0/off/false/no to keep the legacy
// outer-only files for ablation.
const MergeRecordsSwitch = "EXCALIBUR_ATTEMPTS_MERGE_RECORDS"

// MergeRecordsEnabled reports whether the fan-out merges per-attempt
// records into the outer recorder.
func MergeRecordsEnabled() bool { return switchDefaultOn(MergeRecordsSwitch) }

// DeliverableSwitch gates the deliverable hard checks in self-selection:
// an attempt whose named outputs (paths, executables, outputs with
// content, from the same extraction the completion gate uses) are
// missing from its own copy does not win over one that has them.
// Default ON. EXCALIBUR_ATTEMPTS_DELIVERABLE=0 selects on the
// verified-check and change signals only.
const DeliverableSwitch = "EXCALIBUR_ATTEMPTS_DELIVERABLE"

// DeliverableEnabled reports whether selection runs the deliverable checks.
func DeliverableEnabled() bool { return switchDefaultOn(DeliverableSwitch) }

// FallbackSwitch gates the recovery path: when the winner's changes
// cannot be promoted into the real directory (apply error, verification
// failure, or "applied 0 file(s)" for a winner that changed files), the
// run falls back to one normal attempt in the real directory with the
// remaining time. Default ON. EXCALIBUR_ATTEMPTS_FALLBACK=0 keeps the
// failure as the run's outcome.
const FallbackSwitch = "EXCALIBUR_ATTEMPTS_FALLBACK"

// FallbackEnabled reports whether a failed promotion falls back to a
// single attempt in the real directory.
func FallbackEnabled() bool { return switchDefaultOn(FallbackSwitch) }

// PathMapSwitch gates the workdir mapping: absolute paths under the
// real directory, relative paths and shell commands in an attempt's
// tool calls resolve to that attempt's copy. Default ON. With it off
// the copies still isolate the attempts' own relative work, but
// absolute paths reach the real directory again (ablation only).
const PathMapSwitch = "EXCALIBUR_ATTEMPTS_PATH_MAP"

// PathMapEnabled reports whether attempts map tool paths onto their copy.
func PathMapEnabled() bool { return switchDefaultOn(PathMapSwitch) }

// switchDefaultOn reads an on/off switch that defaults to ON. The
// values 0, off, false and no turn it off; anything else (or unset)
// keeps it on.
func switchDefaultOn(name string) bool {
	v, ok := os.LookupEnv(name)
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

// StepsForAttempt returns the step cap for one attempt: the full
// parent budget when FullBudgetEnabled (default), else the legacy
// split. A non-positive parent means the engine default (25).
func StepsForAttempt(parentMaxSteps, n int) int {
	if n < 1 {
		n = 1
	}
	if !FullBudgetEnabled() {
		return DivideSteps(parentMaxSteps, n)
	}
	eff := parentMaxSteps
	if eff <= 0 {
		eff = 25
	}
	return eff
}

// BudgetForAttempt returns the USD cap for one attempt: the full
// parent budget when FullBudgetEnabled (default), else the legacy
// split. A nil parent stays nil (uncapped).
func BudgetForAttempt(parent *float64, n int) *float64 {
	if parent == nil || n <= 1 {
		return parent
	}
	if !FullBudgetEnabled() {
		return DivideBudget(parent, n)
	}
	out := *parent
	return &out
}
