package ctxcompiler_test

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

func TestUnreproducibleExamplesMethod(t *testing.T) {
	t.Setenv(ctxcompiler.EnvMethodV2, "on")
	t.Setenv("EXCALIBUR_EXAMPLE_FORMAT", "")
	on := ctxcompiler.WorkingMethod()
	for _, want := range []string{"example values cannot be reproduced", "format-only", "offsets, scaling or renaming", "second, different input you create"} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q", want)
		}
	}
	t.Setenv("EXCALIBUR_EXAMPLE_FORMAT", "off")
	off := ctxcompiler.WorkingMethod()
	if strings.Contains(off, "example values cannot be reproduced") {
		t.Fatal("disabled guidance present")
	}
	t.Logf("O2 method bytes: off=%d on=%d delta=%+d", len(off), len(on), len(on)-len(off))
}
