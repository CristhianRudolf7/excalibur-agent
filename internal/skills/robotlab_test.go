package skills

import "testing"

func TestRobotLabExcludedFromEval(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index *Index
		want  bool
	}{{"builtin", NewBuiltinIndex(), true}, {"eval", NewEvalBuiltinIndex(), false}} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.index.Scan(); err != nil {
				t.Fatal(err)
			}
			_, ok := tc.index.Get("robot-lab")
			if ok != tc.want {
				t.Fatalf("robot-lab present=%v", ok)
			}
		})
	}
	if !KnownTool("robot_lab") {
		t.Fatal("Robot Lab tool must resolve when loading the skill")
	}
}

func TestAppUIQualityExcludedFromEval(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index *Index
		want  bool
	}{{"builtin", NewBuiltinIndex(), true}, {"eval", NewEvalBuiltinIndex(), false}} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.index.Scan(); err != nil {
				t.Fatal(err)
			}
			if _, ok := tc.index.Get("app-ui-quality"); ok != tc.want {
				t.Fatalf("app-ui-quality present=%v", ok)
			}
		})
	}
}
