package attempts

import (
	"testing"
)

func TestConfigFromEnvDefaultOff(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "")
	cfg := ConfigFromEnv()
	if cfg.N != 1 || cfg.Enabled() {
		t.Fatalf("empty env = %+v, want N=1 disabled", cfg)
	}
}

func TestConfigFromEnvParsesAndClamps(t *testing.T) {
	t.Setenv("EXCALIBUR_ATTEMPTS", "3")
	if cfg := ConfigFromEnv(); cfg.N != 3 || !cfg.Enabled() {
		t.Fatalf("N=3 = %+v", cfg)
	}
	t.Setenv("EXCALIBUR_ATTEMPTS", "0")
	if cfg := ConfigFromEnv(); cfg.N != 1 || cfg.Enabled() {
		t.Fatalf("N=0 must disable, got %+v", cfg)
	}
	t.Setenv("EXCALIBUR_ATTEMPTS", "bogus")
	if cfg := ConfigFromEnv(); cfg.N != 1 || cfg.Enabled() {
		t.Fatalf("bogus must disable, got %+v", cfg)
	}
	t.Setenv("EXCALIBUR_ATTEMPTS", "99")
	if cfg := ConfigFromEnv(); cfg.N != MaxAttempts {
		t.Fatalf("large N must clamp to %d, got %+v", MaxAttempts, cfg)
	}
}

func TestDivideSteps(t *testing.T) {
	if got := DivideSteps(25, 3); got != 9 {
		t.Fatalf("25/3 = %d, want 9", got)
	}
	if got := DivideSteps(0, 3); got != 9 {
		t.Fatalf("default 25/3 = %d, want 9", got)
	}
	if got := DivideSteps(1, 3); got != 1 {
		t.Fatalf("1/3 floors at 1, got %d", got)
	}
}

func TestDivideBudget(t *testing.T) {
	if DivideBudget(nil, 3) != nil {
		t.Fatal("nil budget stays nil")
	}
	b := 9.0
	per := DivideBudget(&b, 3)
	if per == nil || *per != 3.0 {
		t.Fatalf("9/3 = %v", per)
	}
	if b != 9.0 {
		t.Fatal("parent must not mutate")
	}
}

func TestNewSwitchesDefaultOnAndDisable(t *testing.T) {
	for _, name := range []string{DeliverableSwitch, FallbackSwitch, PathMapSwitch, FullBudgetSwitch, MergeRecordsSwitch} {
		t.Setenv(name, "")
	}
	if !DeliverableEnabled() || !FallbackEnabled() || !PathMapEnabled() {
		t.Fatal("deliverable, fallback and path mapping default ON")
	}
	for _, off := range []string{"0", "off", "false", "no", " NO "} {
		t.Setenv(DeliverableSwitch, off)
		t.Setenv(FallbackSwitch, off)
		t.Setenv(PathMapSwitch, off)
		if DeliverableEnabled() || FallbackEnabled() || PathMapEnabled() {
			t.Fatalf("%q must disable the switches", off)
		}
	}
	t.Setenv(PathMapSwitch, "1")
	if !PathMapEnabled() {
		t.Fatal("explicit on value must enable")
	}
}
