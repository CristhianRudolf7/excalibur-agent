package ctxcompiler

import (
	"strings"
	"testing"
)

func TestVisualInspectionMethodGuidance(t *testing.T) {
	t.Setenv("EXCALIBUR_METHOD_V2", "1")
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "0")
	off := WorkingMethod()
	if strings.Contains(off, "inspect_image") {
		t.Fatal("disabled guidance present")
	}
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "1")
	on := WorkingMethod()
	for _, want := range []string{"capture an image and use inspect_image", "blank/unchanged screen means the goal is not reached", "change configuration rather than re-check processes", "before/after and use compare_to"} {
		if !strings.Contains(on, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if delta := len(on) - len(off); delta != len(visualInspectionGuidance) || delta > 300 {
		t.Fatalf("method delta %d bytes", delta)
	}
	t.Logf("method prompt: off=%d on=%d delta=%d bytes", len(off), len(on), len(on)-len(off))
}
