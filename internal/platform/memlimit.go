package platform

import (
	"math"
	"runtime/debug"
)

// defaultMemoryLimit restores the Go default (no GOMEMLIMIT target).
// It equals math.MaxInt64, the value debug.SetMemoryLimit treats as
// "no limit".
const defaultMemoryLimit = math.MaxInt64

// SetGOMEMLIMIT fixes the Go garbage-collector memory target to
// limitBytes (Section 3.2): the collector starts ahead of memory
// pressure instead of waiting for the OS to reclaim. Pass
// math.MaxInt64 to restore the default. It returns the previous
// limit.
//
// The limit only covers Go-managed heap memory: cgo allocations
// (SQLite page caches, tree-sitter) and external models are invisible
// to it, so the orchestrator budgets below the machine size and the
// RAM watchdog (Section 5.2) stays authoritative.
func SetGOMEMLIMIT(limitBytes int64) int64 {
	return debug.SetMemoryLimit(limitBytes)
}

// GOMEMLIMITForBudget derives the collector target from a daemon RAM
// budget: 90% of the budget, leaving headroom for cgo and spikes. A
// non-positive budget restores the default limit.
func GOMEMLIMITForBudget(budgetBytes int64) int64 {
	if budgetBytes <= 0 {
		return defaultMemoryLimit
	}
	return budgetBytes * 90 / 100
}

// ApplyBudgetRAM sets GOMEMLIMIT from a daemon RAM budget (90% of the
// budget) and returns the previous limit.
func ApplyBudgetRAM(budgetBytes int64) int64 {
	return SetGOMEMLIMIT(GOMEMLIMITForBudget(budgetBytes))
}
