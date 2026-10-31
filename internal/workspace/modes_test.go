package workspace

import (
	"testing"
)

func TestModeTable(t *testing.T) {
	cases := []struct {
		name string
		req  ModeRequest
		want WorkMode
	}{
		{"default single shared", ModeRequest{}, ModeShared},
		{"brief task shared", ModeRequest{ParallelImplementers: 4, HasGit: true, BriefTask: true}, ModeShared},
		{"parallel git worktrees", ModeRequest{ParallelImplementers: 3, HasGit: true}, ModeWorktrees},
		{"parallel no git shared", ModeRequest{ParallelImplementers: 3}, ModeShared},
		{"submodules force shared", ModeRequest{ParallelImplementers: 3, HasGit: true, HasSubmodules: true}, ModeShared},
		{"single implementer git shared", ModeRequest{ParallelImplementers: 1, HasGit: true}, ModeShared},
	}
	for _, c := range cases {
		if got := ModeFor(c.req); got != c.want {
			t.Errorf("%s: want %s, got %s", c.name, c.want, got)
		}
	}
}

func TestModeOverride(t *testing.T) {
	var s ModeStore
	if got := s.Resolve("p", ModeRequest{ParallelImplementers: 3, HasGit: true}); got != ModeWorktrees {
		t.Fatalf("want worktrees, got %s", got)
	}
	if err := s.SetOverride("p", ModeShared); err != nil {
		t.Fatal(err)
	}
	if got := s.Resolve("p", ModeRequest{ParallelImplementers: 3, HasGit: true}); got != ModeShared {
		t.Fatalf("override must win, got %s", got)
	}
	// Safety rails: overrides that violate Section 16.4 fall back.
	if err := s.SetOverride("q", ModeWorktrees); err != nil {
		t.Fatal(err)
	}
	if got := s.Resolve("q", ModeRequest{HasSubmodules: true}); got != ModeShared {
		t.Fatalf("submodules must force shared, got %s", got)
	}
	if got := s.Resolve("q", ModeRequest{}); got != ModeShared {
		t.Fatalf("no git must force shared, got %s", got)
	}
	if err := s.SetOverride("q", "bogus"); err == nil {
		t.Fatal("bad mode must be rejected")
	}
}

// Section 16.6 row 3: overlapping file sets warn the parent.
func TestAssignOverlapWarnsParent(t *testing.T) {
	modules := map[string][]string{
		"auth": {"auth/login.go", "auth/shared.go"},
		"api":  {"api/handler.go", "auth/shared.go"},
	}
	var got *OverlapNotice
	assign, notice := AssignFileSets(
		[]string{"c1", "c2"},
		modules, nil,
		FuncNotifier(func(n OverlapNotice) { got = &n }),
	)
	if notice == nil || got == nil {
		t.Fatal("overlap must produce a notice")
	}
	found := false
	for _, f := range notice.Files {
		if f == "auth/shared.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("notice must name the shared file: %+v", notice)
	}
	if len(assign) != 2 {
		t.Fatalf("want 2 assignments, got %d", len(assign))
	}
	// Single-owner option resolves the overlap deterministically.
	owner := SharedFileOwner("auth/shared.go", assign)
	if owner == "" {
		t.Fatal("shared file needs an owner")
	}
}

// Disjoint modules stay disjoint: no notice.
func TestAssignDisjoint(t *testing.T) {
	modules := map[string][]string{
		"a": {"a/1.go"},
		"b": {"b/1.go"},
		"c": {"c/1.go"},
		"d": {"d/1.go"},
	}
	assign, notice := AssignFileSets([]string{"c1", "c2"}, modules, nil, nil)
	if notice != nil {
		t.Fatalf("no overlap expected: %+v", notice)
	}
	if len(assign) != 2 {
		t.Fatalf("want 2 assignments, got %d", len(assign))
	}
}

// Direct module dependencies travel with their module (16.3).
func TestAssignKeepsDepsTogether(t *testing.T) {
	modules := map[string][]string{
		"api": {"api/h.go"},
		"db":  {"db/q.go"},
	}
	deps := map[string][]string{"api": {"db"}}
	assign, _ := AssignFileSets([]string{"c1"}, modules, deps, nil)
	if len(assign) != 1 || len(assign[0].Files) != 2 {
		t.Fatalf("deps must travel together: %+v", assign)
	}
}

func TestAssignNoChildren(t *testing.T) {
	if a, n := AssignFileSets(nil, map[string][]string{"a": {"x"}}, nil, nil); a != nil || n != nil {
		t.Fatal("no children must yield nothing")
	}
}
