package ctxcompiler_test

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

func TestDisagreementMethodGuidance(t *testing.T) {
	t.Setenv(ctxcompiler.EnvMethodV2, "on")
	t.Setenv("EXCALIBUR_DISAGREEMENT_PROTOCOL", "")
	on := ctxcompiler.WorkingMethod()
	for _, want := range []string{"concrete samples", "literal request", "before changing", "oracle"} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q", want)
		}
	}
	t.Setenv("EXCALIBUR_DISAGREEMENT_PROTOCOL", "off")
	off := ctxcompiler.WorkingMethod()
	if strings.Contains(off, "concrete samples") {
		t.Fatal("disabled guidance present")
	}
	t.Logf("O1 method bytes: off=%d on=%d delta=%+d", len(off), len(on), len(on)-len(off))
}
