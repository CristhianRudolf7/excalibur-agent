package agents

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestMasterSetHasNoWorkTools(t *testing.T) {
	set := map[string]bool{}
	for _, tool := range MasterTools() {
		set[tool] = true
	}
	for _, banned := range []string{"read_file", "write_file", "edit_file", "apply_patch", "exec", "exec_io"} {
		if set[banned] {
			t.Fatalf("master holds work tool %q", banned)
		}
	}
	for _, want := range []string{"subagent", "status", "task_db", "clock"} {
		if !set[want] {
			t.Fatalf("master misses %q", want)
		}
	}
}

func TestSubagentBaseDropsMasterOnlyTools(t *testing.T) {
	set := map[string]bool{}
	for _, tool := range SubagentBaseTools() {
		set[tool] = true
	}
	if set["send_message"] || set["settings"] {
		t.Fatal("subagent base holds send_message or settings")
	}
	if !set["ack_directive"] || !set["exec"] || !set["read_file"] {
		t.Fatal("subagent base misses ack_directive, exec or read_file")
	}
}

func TestLevel2LosesSubagent(t *testing.T) {
	for name, p := range Builtins {
		if name == ProfileMaster {
			continue
		}
		if !p.Has("subagent", 1) && name == ProfileCoordinator {
			t.Fatalf("coordinator at level 1 must hold subagent")
		}
		if p.Has("subagent", 2) {
			t.Fatalf("profile %q at level 2 must not hold subagent", name)
		}
		if MaySpawn(p, 2) {
			t.Fatalf("profile %q at level 2 must not spawn", name)
		}
	}
}

func TestProfileRemovals(t *testing.T) {
	explorer, err := Lookup(ProfileExplorer)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"write_file", "edit_file", "apply_patch", "exec", "exec_io"} {
		if explorer.Has(banned, 2) {
			t.Fatalf("explorer holds %q", banned)
		}
	}
	verifier, _ := Lookup(ProfileVerifier)
	if !verifier.Has("exec", 2) {
		t.Fatal("verifier must keep exec to run checks")
	}
	if verifier.Has("write_file", 2) {
		t.Fatal("verifier must not write files")
	}
	browser, _ := Lookup(ProfileBrowser)
	if browser.Has("exec", 2) || browser.Has("exec_io", 2) {
		t.Fatal("browser-operator must have no terminal")
	}
	planner, _ := Lookup(ProfilePlanner)
	if planner.Has("edit_file", 2) {
		t.Fatal("planner must not edit code")
	}
}

func TestClampReducesNeverExpands(t *testing.T) {
	impl, _ := Lookup(ProfileImplementer)
	full, err := ClampTools(impl, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) == 0 {
		t.Fatal("empty clamp of empty request")
	}
	reduced, err := ClampTools(impl, 2, []string{"read_file", "grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reduced) != 2 {
		t.Fatalf("want reduced set of 2, got %v", reduced)
	}
	explorer, _ := Lookup(ProfileExplorer)
	if _, err := ClampTools(explorer, 2, []string{"exec", "read_file"}); err == nil {
		t.Fatal("card must not widen the explorer profile with exec")
	}
}

func TestMasterCreatesParentWithWorkTools(t *testing.T) {
	// Section 15.9: the master has no file or terminal tools, yet it
	// creates a parent that does; tools depend on the delegate
	// profile, not on the delegator.
	master, _ := Lookup(ProfileMaster)
	if master.Has("read_file", 0) {
		t.Fatal("master must not hold read_file")
	}
	coord, _ := Lookup(ProfileCoordinator)
	if !coord.Has("read_file", 1) || !coord.Has("exec", 1) {
		t.Fatal("coordinator parent must hold file and terminal tools")
	}
}

func TestIdentityLevels(t *testing.T) {
	scope := types.Scope{Kind: types.ScopeProject, ID: "p1"}
	masterProfile, _ := Lookup(ProfileMaster)
	master, err := NewIdentity("Master", "talks to the user", masterProfile, nil, scope)
	if err != nil {
		t.Fatal(err)
	}
	if master.Level != 0 || master.ParentID != nil {
		t.Fatalf("bad master identity: %+v", master)
	}
	coordProfile, _ := Lookup(ProfileCoordinator)
	parent, err := NewIdentity("Pia", "coordinates task", coordProfile, &master, scope)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Level != 1 {
		t.Fatalf("want level 1, got %d", parent.Level)
	}
	implProfile, _ := Lookup(ProfileImplementer)
	child, err := NewIdentity("Lena", "implements module X", implProfile, &parent, scope)
	if err != nil {
		t.Fatal(err)
	}
	if child.Level != 2 {
		t.Fatalf("want level 2, got %d", child.Level)
	}
	if _, err := ChildLevel(child); err == nil {
		t.Fatal("level-2 child must not spawn")
	}
	// A level-2 profile under the master is a level mismatch.
	if _, err := NewIdentity("Nope", "bad", implProfile, &master, scope); err == nil {
		t.Fatal("implementer under master must fail level check")
	}
	// Root of embedded mode: level 1 without a parent is allowed.
	if _, err := NewIdentity("Root", "embedded root", coordProfile, nil, scope); err != nil {
		t.Fatalf("embedded level-1 root must be allowed: %v", err)
	}
	if err := ValidateIdentity(child); err != nil {
		t.Fatal(err)
	}
}

func TestCustomAgent(t *testing.T) {
	p, err := CustomAgent("lens-custom", ProfileExplorer, []string{"web_search"}, "Custom lens identity.")
	if err != nil {
		t.Fatal(err)
	}
	if p.Has("web_search", 2) {
		t.Fatal("custom removal must apply")
	}
	if _, err := Lookup("lens-custom"); err != nil {
		t.Fatalf("custom profile must be discoverable: %v", err)
	}
	if _, err := CustomAgent("coordinator", ProfileExplorer, nil, "x"); err == nil {
		t.Fatal("must not shadow a built-in profile")
	}
	if _, err := CustomAgent("empty-doc", ProfileExplorer, nil, ""); err == nil {
		t.Fatal("custom profile needs an identity document")
	}
}

func TestLookupUnknown(t *testing.T) {
	if _, err := Lookup("nope"); err == nil {
		t.Fatal("unknown profile must fail")
	}
}
