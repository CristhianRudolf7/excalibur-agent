package cli

import "testing"

func TestWhiteboardExcludedFromEval(t *testing.T) {
	for _, p := range []ProfileName{ProfileDefault, ProfileDelegate, ProfileEval} {
		core, deferred := EffectiveTools(p)
		found := false
		for _, name := range append(core, deferred...) {
			if name == "whiteboard" {
				found = true
			}
		}
		if found != (p != ProfileEval) {
			t.Fatalf("profile %s whiteboard=%v", p, found)
		}
	}
	t.Setenv("EXCALIBUR_WHITEBOARD", "off")
	core, deferred := EffectiveTools(ProfileDefault)
	for _, name := range append(core, deferred...) {
		if name == "whiteboard" {
			t.Fatal("disabled tool admitted")
		}
	}
}
