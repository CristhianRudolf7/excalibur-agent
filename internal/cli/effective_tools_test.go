package cli

import (
	"sort"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

// legacyCoreTools is the eval core table before the item-41 trim,
// plus the friction-repair tool that the switch appends. The
// assistant and delegate profiles must keep exactly this surface
// (plus their own extras), so their tool hashes do not move.
var legacyCoreTools = []string{
	"read_file", "write_file", "edit_file", "exec", "task_db",
	"history_search", "get_tool_output", "pin_observation",
	"leave_continuation_note", "ack_directive", "status", "clock",
	"search_skills", "load_skill", "search_tools", "call_tool", "subagent",
	"exec_io",
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func containsTool(list []string, name string) bool {
	for _, t := range list {
		if t == name {
			return true
		}
	}
	return false
}

// The eval profile no longer exposes the four tools that cannot act
// in a single autonomous run.
func TestEvalSurfaceTrimsNonActingTools(t *testing.T) {
	core, _ := EffectiveTools(ProfileEval)
	for _, name := range eval.TrimmedFromEvalTools() {
		if containsTool(core, name) {
			t.Errorf("eval surface still exposes %s", name)
		}
	}
	for _, want := range []string{"exec", "search_skills", "load_skill", "exec_io"} {
		if !containsTool(core, want) {
			t.Errorf("eval surface lost %s", want)
		}
	}
}

// Assistant and delegate surfaces keep the legacy core set exactly,
// so their ToolsHash values do not change with the eval trim.
func TestNonEvalSurfacesKeepLegacyCore(t *testing.T) {
	for _, profile := range []ProfileName{ProfileDelegate, ProfileDefault} {
		core, _ := EffectiveTools(profile)
		want := append([]string(nil), legacyCoreTools...)
		if profile == ProfileDefault {
			want = append(want, "send_message", "question_answer")
		}
		got, wantSorted := sortedCopy(core), sortedCopy(want)
		if len(got) != len(wantSorted) {
			t.Fatalf("%s core = %v, want %v", profile, got, wantSorted)
		}
		for i := range got {
			if got[i] != wantSorted[i] {
				t.Fatalf("%s core = %v, want %v", profile, got, wantSorted)
			}
		}
	}
}

func TestSkillsOffEffectiveSurface(t *testing.T) {
	t.Setenv("EXCALIBUR_SKILLS", "off")
	for _, profile := range []ProfileName{ProfileEval, ProfileDefault, ProfileDelegate} {
		core, deferred := EffectiveTools(profile)
		for _, name := range []string{"search_skills", "load_skill"} {
			if containsTool(core, name) || containsTool(deferred, name) {
				t.Fatalf("%s exposes %s with skills off", profile, name)
			}
		}
	}
}
