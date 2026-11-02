package tasks

import (
	"strings"
	"testing"
)

func TestDisagreementVerifierPrompts(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	small := VerifierSmallContext{Request: "Apply the literal rule to the supplied records."}
	t.Setenv("EXCALIBUR_DISAGREEMENT_PROTOCOL", "")
	on := StrongVerifierPromptWithConfig(cfg, small, nil, "")
	for _, want := range []string{"concrete samples", "literal request", "before changing", "direct rule", "FRESH_INPUTS may be NA"} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(on, "<executed command, generated input and independent oracle;") {
		t.Fatal("schema still requires an oracle unconditionally")
	}
	pending := strings.Join(GeneralizationPendings("FRESH_INPUTS: FAIL\nOUTPUT_CONTRACT: NA"), "; ")
	if !strings.Contains(pending, "concrete samples") || strings.Contains(pending, "run a fresh generated-input check with an independent oracle") {
		t.Fatal(pending)
	}
	t.Setenv("EXCALIBUR_DISAGREEMENT_PROTOCOL", "off")
	off := StrongVerifierPromptWithConfig(cfg, small, nil, "")
	if strings.Contains(off, "FRESH_INPUTS may be NA") {
		t.Fatal("disabled guidance present")
	}
	if !strings.Contains(strings.Join(GeneralizationPendings("FRESH_INPUTS: FAIL\nOUTPUT_CONTRACT: NA"), "; "), "run a fresh generated-input check with an independent oracle") {
		t.Fatal("legacy refusal changed")
	}
	t.Logf("O1 verifier bytes: off=%d on=%d delta=%+d", len(off), len(on), len(on)-len(off))
}
