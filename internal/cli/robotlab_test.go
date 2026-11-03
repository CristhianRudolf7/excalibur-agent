package cli

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

func TestRobotLabProfileSurface(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "off")
	for _, profile := range []ProfileName{ProfileDefault, ProfileDelegate, ProfileEval} {
		core, deferred := EffectiveTools(profile)
		if slices.Contains(core, "robot_lab") {
			t.Fatal("Robot Lab entered the fixed prefix")
		}
		if slices.Contains(deferred, "robot_lab") != (profile != ProfileEval) {
			t.Fatalf("wrong surface for %s", profile)
		}
		gate := newProfileGatedRunner(profile, nil)
		for name, args := range map[string]json.RawMessage{"robot_lab": json.RawMessage(`{"action":"scenarios"}`), "call_tool": json.RawMessage(`{"name":"robot_lab","arguments":{"action":"scenarios"}}`)} {
			err := gate.check(name, args)
			if (err != nil) != (profile == ProfileEval) {
				t.Fatalf("profile=%s name=%s error=%v", profile, name, err)
			}
		}
	}
}

func TestEmbeddedRobotLabRuntimeIsLazyAndExcludedFromEval(t *testing.T) {
	t.Setenv("EXCALIBUR_HOME", t.TempDir())
	t.Setenv("EXCALIBUR_ROBOTLAB", "on")
	for _, profile := range []ProfileName{ProfileDefault, ProfileDelegate, ProfileEval} {
		tool, closeLab, err := embeddedRobotLab(profile, nil)
		if err != nil {
			t.Fatal(err)
		}
		if profile == ProfileEval {
			if tool != nil {
				t.Fatal("eval gained a runtime")
			}
		} else {
			if tool == nil || tool.Sidecar == nil || tool.Vision == nil || tool.Publisher == nil {
				t.Fatal("runtime not composed")
			}
			if tool.Sidecar.Status(context.Background()).Running {
				t.Fatal("runtime started eagerly")
			}
		}
		closeLab()
	}
}
