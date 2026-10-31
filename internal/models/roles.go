// Role table for Section 19.1.
//
// Configuration assigns each model role a primary endpoint plus an
// ordered fallback chain. Code never names concrete models: callers
// pass a ModelRole and the gateway resolves the endpoint. This file
// describes the roles, lists them, and validates a configured table.
package models

import (
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// RoleProfile describes one model role: its use and the wanted
// model profile. The concrete model ids live in configuration.
type RoleProfile struct {
	Role    types.ModelRole `json:"role"`
	Use     string          `json:"use"`
	Profile string          `json:"profile"`
}

// RoleProfiles is the Section 19.1 table: role, use, wanted profile.
func RoleProfiles() []RoleProfile {
	return []RoleProfile{
		{Role: types.RoleInterlocutor, Use: "master", Profile: "cheap, fast, good dialogue and simple tool use"},
		{Role: types.RoleExecutor, Use: "children and parents of normal tasks", Profile: "cheap, good tool use and code"},
		{Role: types.RolePlanner, Use: "specification, macro plan, replanning", Profile: "intermediate or strong; few calls per task"},
		{Role: types.RoleExpert, Use: "expert consults on hard steps", Profile: "strong; sporadic use with a compact context"},
		{Role: types.RoleVerifier, Use: "independent verifier and contrast with the request", Profile: "different family than executor when possible"},
		{Role: types.RoleExtractor, Use: "memory extraction, compaction summaries, templates", Profile: "cheapest with reliable structured output"},
		{Role: types.RoleDecider, Use: "bounded questions with probability", Profile: "decision model (19.4)"},
		{Role: types.RoleVision, Use: "vision", Profile: "per Sections 10.8, 14.4, 14.5"},
		{Role: types.RoleEmbeddings, Use: "embeddings", Profile: "per Section 10.8"},
		{Role: types.RoleReranker, Use: "reranking", Profile: "per Section 10.7"},
		{Role: types.RoleSTT, Use: "speech to text", Profile: "per Section 14.5"},
		{Role: types.RoleTTS, Use: "text to speech", Profile: "per Section 14.5"},
	}
}

// AllRoles lists every role of Section 19.1 in table order.
func AllRoles() []types.ModelRole {
	profiles := RoleProfiles()
	out := make([]types.ModelRole, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Role)
	}
	return out
}

// CoreRoles are the roles every deployment configures. The media
// roles (vision, embeddings, reranker, stt, tts) are optional until
// their providers land.
func CoreRoles() []types.ModelRole {
	return []types.ModelRole{
		types.RoleInterlocutor,
		types.RoleExecutor,
		types.RolePlanner,
		types.RoleExpert,
		types.RoleVerifier,
		types.RoleExtractor,
		types.RoleDecider,
	}
}

// ValidateRoles checks a configured role table: every entry needs a
// primary provider and model, fallbacks need the same, and the
// reasoning effort stays fixed per role (it is read from the entry,
// never mutated per call). Unknown roles are rejected so typos
// surface at startup instead of at the first call.
func ValidateRoles(roles map[types.ModelRole]RoleEntry) error {
	known := map[types.ModelRole]bool{}
	for _, r := range AllRoles() {
		known[r] = true
	}
	for role, entry := range roles {
		if !known[role] {
			return fmt.Errorf("models: unknown role %q", string(role))
		}
		if err := validateRef(role, "primary", entry.Primary); err != nil {
			return err
		}
		for i, fb := range entry.Fallbacks {
			if err := validateRef(role, fmt.Sprintf("fallback[%d]", i), fb); err != nil {
				return err
			}
		}
	}
	return nil
}

// RequireRoles reports which of the wanted roles have no entry. The
// gateway returns a typed error for a missing role at call time;
// this helper lets startup fail fast with the full missing list.
func RequireRoles(roles map[types.ModelRole]RoleEntry, wanted []types.ModelRole) []types.ModelRole {
	var missing []types.ModelRole
	for _, r := range wanted {
		e, ok := roles[r]
		if !ok || e.Primary.Provider == "" || e.Primary.Model == "" {
			missing = append(missing, r)
		}
	}
	return missing
}

// VerifierSharesFamily reports whether the verifier primary looks
// like the same model family as the executor primary. Section 19.1
// wants a different family when possible; the check is a cheap
// prefix comparison on the model id, not a provider query.
func VerifierSharesFamily(roles map[types.ModelRole]RoleEntry) bool {
	ex, ok := roles[types.RoleExecutor]
	if !ok {
		return false
	}
	ver, ok := roles[types.RoleVerifier]
	if !ok {
		return false
	}
	return sameFamily(ex.Primary.Model, ver.Primary.Model)
}

func validateRef(role types.ModelRole, where string, ref ModelRef) error {
	if ref.Provider == "" {
		return fmt.Errorf("models: role %q %s: empty provider", string(role), where)
	}
	if ref.Model == "" {
		return fmt.Errorf("models: role %q %s: empty model", string(role), where)
	}
	return nil
}

// sameFamily compares model ids up to the first version/dash
// separator. It is a heuristic for the "different family" guidance,
// not an exact provider taxonomy.
func sameFamily(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	family := func(s string) string {
		for i := 0; i < len(s); i++ {
			if s[i] == '-' || s[i] == '_' || s[i] == '/' || s[i] == ':' {
				return s[:i]
			}
		}
		return s
	}
	return family(a) == family(b)
}
