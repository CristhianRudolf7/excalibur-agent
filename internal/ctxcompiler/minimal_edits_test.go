package ctxcompiler_test

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

func TestMinimalValueEditsMethod(t *testing.T) {
	t.Setenv(ctxcompiler.EnvMethodV2, "on")
	t.Setenv("EXCALIBUR_MINIMAL_VALUE_EDITS", "")
	on := ctxcompiler.WorkingMethod()
	for _, want := range []string{"replace or rename values, change only those values", "surrounding text, quoting, escaping, whitespace and formatting byte-identical unless asked otherwise", "exact string substitution over rewriting lines"} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q", want)
		}
	}
	t.Setenv("EXCALIBUR_MINIMAL_VALUE_EDITS", "off")
	off := ctxcompiler.WorkingMethod()
	if strings.Contains(off, "exact string substitution") {
		t.Fatal("disabled guidance present")
	}
	if delta := len(on) - len(off); delta > 230 || delta < 1 {
		t.Fatalf("guidance byte budget: %d", delta)
	}
	t.Logf("minimal edits method: off=%d on=%d delta=%+d bytes", len(off), len(on), len(on)-len(off))
	t.Setenv(ctxcompiler.EnvMethodV2, "off")
	t.Setenv("EXCALIBUR_MINIMAL_VALUE_EDITS", "on")
	if strings.Contains(ctxcompiler.WorkingMethod(), "exact string substitution") {
		t.Fatal("legacy method must remain unchanged")
	}
}
