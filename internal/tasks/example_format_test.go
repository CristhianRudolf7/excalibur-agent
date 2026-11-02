package tasks

import (
	"strings"
	"testing"
)

func TestUnreproducibleExamplesVerifier(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	small := VerifierSmallContext{Request: "Create a program that works with different input files using the shown output format."}
	for _, generalize := range []bool{true, false} {
		cfg.EnableGeneralizeChecks = generalize
		t.Setenv("EXCALIBUR_EXAMPLE_FORMAT", "")
		on := StrongVerifierPromptWithConfig(cfg, small, nil, "")
		for _, want := range []string{"example values cannot be reproduced", "format-only", "offsets, scaling or renaming", "second, different input created by the executor"} {
			if !strings.Contains(on, want) {
				t.Errorf("generalize=%v missing %q", generalize, want)
			}
		}
		t.Setenv("EXCALIBUR_EXAMPLE_FORMAT", "off")
		off := StrongVerifierPromptWithConfig(cfg, small, nil, "")
		if strings.Contains(off, "example values cannot be reproduced") {
			t.Fatal("disabled guidance present")
		}
		t.Logf("O2 verifier generalize=%v bytes: off=%d on=%d delta=%+d", generalize, len(off), len(on), len(on)-len(off))
	}
}
