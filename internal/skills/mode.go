package skills

import (
	"fmt"
	"os"
	"strings"
)

// LibraryMode is the ablation switch for the skills library: off
// removes the library from the run, on wires the catalog with
// search and load but no automatic suggestions, and auto adds the
// task-start auto-suggest on top (Section 12.4, step 1). The default
// is auto: the library helps small models most when it is present
// and proactively suggested. Every mechanism in this milestone reads
// this switch, so trials can measure off vs on vs on+auto-suggest.
type LibraryMode string

// Library modes.
const (
	// LibraryOff disables the library: empty catalog, no
	// suggestions, and the caller leaves the skill tools unwired.
	LibraryOff LibraryMode = "off"
	// LibraryOn wires catalog, search and load without auto-suggest.
	LibraryOn LibraryMode = "on"
	// LibraryAuto wires everything and injects top suggestions at
	// task start.
	LibraryAuto LibraryMode = "auto"
)

// EnvSkillsMode is the environment variable carrying the switch.
const EnvSkillsMode = "EXCALIBUR_SKILLS"

// DefaultLibraryMode returns the default switch position.
func DefaultLibraryMode() LibraryMode { return LibraryAuto }

// normalizeLibraryMode maps "" to auto (the zero value stays the
// helpful default) and lowercases the rest.
func normalizeLibraryMode(s string) LibraryMode {
	switch LibraryMode(strings.ToLower(strings.TrimSpace(s))) {
	case LibraryOff:
		return LibraryOff
	case LibraryOn:
		return LibraryOn
	case LibraryAuto, "":
		return LibraryAuto
	default:
		return LibraryAuto
	}
}

// ParseLibraryMode parses s into a LibraryMode, defaulting unknown
// and empty values to auto.
func ParseLibraryMode(s string) (LibraryMode, error) {
	m := LibraryMode(strings.ToLower(strings.TrimSpace(s)))
	switch m {
	case LibraryOff, LibraryOn, LibraryAuto:
		return m, nil
	case "":
		return LibraryAuto, nil
	default:
		return LibraryAuto, fmt.Errorf("skills: unknown library mode %q (want off, on or auto)", s)
	}
}

// LibraryModeFromEnv reads the switch from the environment:
// EXCALIBUR_SKILLS=off|on|auto, defaulting to auto when unset or
// invalid. getenv is os.Getenv in production and a stub in tests.
func LibraryModeFromEnv(getenv func(string) string) LibraryMode {
	if getenv == nil {
		getenv = os.Getenv
	}
	v := strings.TrimSpace(getenv(EnvSkillsMode))
	if v == "" {
		return LibraryAuto
	}
	return normalizeLibraryMode(v)
}

// CatalogEnabled reports whether the L1 catalog and the skill tools
// are wired under this mode.
func (m LibraryMode) CatalogEnabled() bool {
	return normalizeLibraryMode(string(m)) != LibraryOff
}

// AutoSuggestEnabled reports whether task-start auto-suggest runs
// under this mode.
func (m LibraryMode) AutoSuggestEnabled() bool {
	return normalizeLibraryMode(string(m)) == LibraryAuto
}

// RenderCatalogForMode renders the L1 catalog, or "" when the mode
// disables the library. The return value feeds ctxcompiler's
// SkillCatalog verbatim, so off stays off in the prompt.
func (ix *Index) RenderCatalogForMode(cfg CatalogConfig, mode LibraryMode) string {
	if !mode.CatalogEnabled() {
		return ""
	}
	return ix.RenderCatalog(cfg)
}

// SuggestForMode returns auto-suggest results, or nil when the mode
// disables them (off, or on without auto-suggest). On keeps search
// and load available without injecting suggestions.
func (ix *Index) SuggestForMode(query, model string, max int, mode LibraryMode) []Suggestion {
	if !mode.AutoSuggestEnabled() {
		return nil
	}
	return ix.Suggest(query, model, max)
}

// SuggestWithEnvForMode is SuggestWithEnv gated on the mode.
func (ix *Index) SuggestWithEnvForMode(query string, env EnvSignals, model string, max int, mode LibraryMode) []Suggestion {
	if !mode.AutoSuggestEnabled() {
		return nil
	}
	return ix.SuggestWithEnv(query, env, model, max)
}

// SearchForMode runs search_skills, or nil when the mode disables
// the library.
func (ix *Index) SearchForMode(query, group string, mode LibraryMode) []Hit {
	if !mode.CatalogEnabled() {
		return nil
	}
	return ix.Search(query, group)
}

// LoadForMode runs load_skill, or an error when the mode disables
// the library.
func (ix *Index) LoadForMode(name string, mode LibraryMode) (Loaded, error) {
	if !mode.CatalogEnabled() {
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeLibraryOff, Detail: "skills library is disabled (EXCALIBUR_SKILLS=off)"}
	}
	return ix.Load(name)
}
