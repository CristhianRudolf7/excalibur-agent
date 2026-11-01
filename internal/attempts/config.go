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
func DivideBudget(parent *float64, n int) *float64 {
	if parent == nil || n <= 1 {
		return parent
	}
	per := *parent / float64(n)
	out := per
	return &out
}
