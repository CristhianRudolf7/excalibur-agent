package platform

import (
	"math"
	"testing"
)

func TestGOMEMLIMITForBudget(t *testing.T) {
	if got := GOMEMLIMITForBudget(1000); got != 900 {
		t.Fatalf("GOMEMLIMITForBudget(1000) = %d, want 900", got)
	}
	if got := GOMEMLIMITForBudget(0); got != math.MaxInt64 {
		t.Fatalf("zero budget = %d, want MaxInt64", got)
	}
	if got := GOMEMLIMITForBudget(-5); got != math.MaxInt64 {
		t.Fatalf("negative budget = %d, want MaxInt64", got)
	}
}

func TestSetGOMEMLIMITRoundtrip(t *testing.T) {
	prev := SetGOMEMLIMIT(math.MaxInt64)
	defer SetGOMEMLIMIT(prev)
	cur := SetGOMEMLIMIT(512 << 20)
	if cur != math.MaxInt64 {
		t.Fatalf("previous limit = %d, want MaxInt64 default", cur)
	}
	back := SetGOMEMLIMIT(math.MaxInt64)
	if back != 512<<20 {
		t.Fatalf("previous limit = %d, want 512MiB", back)
	}
}

func TestApplyBudgetRAM(t *testing.T) {
	prev := ApplyBudgetRAM(1000)
	defer SetGOMEMLIMIT(prev)
	if got := ApplyBudgetRAM(0); got != 900 {
		t.Fatalf("previous = %d, want 900", got)
	}
	SetGOMEMLIMIT(math.MaxInt64)
}
