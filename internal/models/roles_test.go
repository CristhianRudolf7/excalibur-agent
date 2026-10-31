// Role table tests (Section 19.1).
package models

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestRoleProfilesCoverAllRoles(t *testing.T) {
	profiles := RoleProfiles()
	if len(profiles) != 12 {
		t.Fatalf("profiles = %d, want 12 roles of 19.1", len(profiles))
	}
	seen := map[types.ModelRole]bool{}
	for _, p := range profiles {
		if p.Use == "" || p.Profile == "" {
			t.Fatalf("role %q needs use and profile", string(p.Role))
		}
		seen[p.Role] = true
	}
	for _, r := range AllRoles() {
		if !seen[r] {
			t.Fatalf("AllRoles has %q missing from profiles", string(r))
		}
	}
}

func TestValidateRolesRejectsEmptyAndUnknown(t *testing.T) {
	good := map[types.ModelRole]RoleEntry{
		types.RoleExecutor: {Primary: ModelRef{Provider: "p", Model: "cheap-1"}},
	}
	if err := ValidateRoles(good); err != nil {
		t.Fatalf("valid roles: %v", err)
	}
	bad := map[types.ModelRole]RoleEntry{
		types.RoleExecutor: {Primary: ModelRef{Provider: "p"}},
	}
	if err := ValidateRoles(bad); err == nil {
		t.Fatal("empty model should fail validation")
	}
	unknown := map[types.ModelRole]RoleEntry{
		"nope": {Primary: ModelRef{Provider: "p", Model: "m"}},
	}
	if err := ValidateRoles(unknown); err == nil {
		t.Fatal("unknown role should fail validation")
	}
}

func TestRequireRolesListsMissing(t *testing.T) {
	roles := map[types.ModelRole]RoleEntry{
		types.RoleExecutor: {Primary: ModelRef{Provider: "p", Model: "m"}},
	}
	missing := RequireRoles(roles, CoreRoles())
	if len(missing) != len(CoreRoles())-1 {
		t.Fatalf("missing = %v", missing)
	}
}

func TestVerifierDifferentFamily(t *testing.T) {
	same := map[types.ModelRole]RoleEntry{
		types.RoleExecutor: {Primary: ModelRef{Provider: "a", Model: "cheap-1"}},
		types.RoleVerifier: {Primary: ModelRef{Provider: "b", Model: "cheap-2"}},
	}
	if !VerifierSharesFamily(same) {
		t.Fatal("same prefix should report shared family")
	}
	diff := map[types.ModelRole]RoleEntry{
		types.RoleExecutor: {Primary: ModelRef{Provider: "a", Model: "cheap-1"}},
		types.RoleVerifier: {Primary: ModelRef{Provider: "b", Model: "strong-9"}},
	}
	if VerifierSharesFamily(diff) {
		t.Fatal("different prefix should not share family")
	}
}
