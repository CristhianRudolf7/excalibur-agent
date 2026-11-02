package cli

import (
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
	"testing"
)

func TestLibraryToolsProfileGating(t *testing.T) {
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	eval := newProfileGatedRunner(ProfileEval, nil)
	for _, name := range []string{"library_list", "library_view", "library_grep", "library_status", "library_search"} {
		if err := eval.check(name, json.RawMessage(`{}`)); err == nil {
			t.Fatalf("eval allows %s", name)
		}
		wrapped, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{}})
		if err := eval.check("call_tool", wrapped); err == nil {
			t.Fatalf("eval allows wrapped %s", name)
		}
		for _, profile := range []ProfileName{ProfileDefault, ProfileDelegate} {
			if err := newProfileGatedRunner(profile, nil).check(name, json.RawMessage(`{}`)); err != nil {
				t.Fatalf("%s denies %s: %v", profile, name, err)
			}
		}
		if name != "library_search" {
			tool, err := emb.Registry.Get(name)
			if err != nil {
				t.Fatal(err)
			}
			def := tool.Definition()
			if len(def.Profiles) != 2 || def.Profiles[0] != "default" || def.Profiles[1] != "delegate" {
				t.Fatalf("profiles: %+v", def)
			}
		}
	}
	pruneDeferredToProfile(emb.Catalog, eval.allowed)
	for _, name := range []string{"library_list", "library_view", "library_grep", "library_status"} {
		if emb.Catalog.IsDeferred(name) {
			t.Fatalf("eval discovers %s", name)
		}
	}
	t.Setenv("EXCALIBUR_LIBRARY", "off")
	disabled, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.Registry.Get("library_list"); err == nil {
		t.Fatal("disabled tool registered")
	}
}
