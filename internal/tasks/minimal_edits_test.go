package tasks

import (
	"strings"
	"testing"
)

func TestMinimalValueEditsVerifier(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	small := VerifierSmallContext{Request: "Replace `amber` with `<violet>` in settings.txt."}
	for _, generalize := range []bool{true, false} {
		cfg.EnableGeneralizeChecks = generalize
		t.Setenv("EXCALIBUR_MINIMAL_VALUE_EDITS", "")
		on := StrongVerifierPromptWithConfig(cfg, small, nil, "")
		guidance := MinimalValueEditsReviewGuidance()
		if !strings.Contains(on, "For replacements or renames, check that only the requested values changed.") || len(guidance) > 80 {
			t.Fatal("missing or oversized minimal value edits review rule", guidance)
		}
		t.Setenv("EXCALIBUR_MINIMAL_VALUE_EDITS", "off")
		off := StrongVerifierPromptWithConfig(cfg, small, nil, "")
		if strings.Contains(off, "only the requested values changed") || len(on)-len(off) != len(guidance) {
			t.Fatal("review switch did not remove exactly the new rule")
		}
		t.Logf("minimal edits verifier generalize=%v: off=%d on=%d delta=%+d bytes", generalize, len(off), len(on), len(on)-len(off))
	}
}
