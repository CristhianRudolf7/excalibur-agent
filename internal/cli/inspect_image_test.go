package cli

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestInspectImageProfileSurfaces(t *testing.T) {
	for _, enabled := range []string{"1", "0"} {
		t.Setenv("EXCALIBUR_INSPECT_IMAGE", enabled)
		for _, profile := range []ProfileName{ProfileEval, ProfileDefault, ProfileDelegate} {
			core, _ := EffectiveTools(profile)
			found := false
			for _, name := range core {
				if name == "inspect_image" {
					found = true
				}
			}
			if found != (enabled == "1") {
				t.Fatalf("profile %s switch %s: tool presence %t", profile, enabled, found)
			}
		}
	}
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "1")
	gate := newProfileGatedRunner(ProfileEval, nil)
	if err := gate.check("inspect_image", nil); err != nil {
		t.Fatal(err)
	}
	if !tools.InspectImageEnabled() {
		t.Fatal("switch did not enable tool")
	}
}
