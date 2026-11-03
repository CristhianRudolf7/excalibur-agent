package skills

import "testing"

func TestAvatarSkillDiscoveryAndEvalIsolation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index *Index
		want  bool
	}{
		{"builtin", NewBuiltinIndex(), true}, {"eval", NewEvalBuiltinIndex(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.index.Scan(); err != nil {
				t.Fatal(err)
			}
			skill, ok := tc.index.Get("agent-avatars")
			if ok != tc.want {
				t.Fatalf("agent-avatars present=%v, want %v", ok, tc.want)
			}
			if ok && (skill.Group != "creative-tools" || len(skill.Tools) != 1 || skill.Tools[0] != "design_avatar") {
				t.Fatalf("avatar skill tool resolution metadata: %+v", skill)
			}
		})
	}
	if !KnownTool("design_avatar") {
		t.Fatal("avatar skill tool must resolve")
	}
}
