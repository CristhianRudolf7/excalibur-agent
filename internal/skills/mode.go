package skills

import (
	"fmt"
	"os"
	"strings"
)

// LibraryMode is the ablation switch for the skills library: off
// removes the library from the run, on wires the catalog with
// search and load but no automatic suggestions, auto adds the
// task-start auto-suggest on top (Section 12.4, step 1), and cards
// wires search and load with no catalog and no suggestions injected:
// the model pulls short cards on demand and the harness appends one
// compact card only when a concrete tool error matches it. The
// default is cards: paired measurements show injected catalogs and
// suggestions cost small models more than they help (see
// docs/development/skills.md), so the safe default injects nothing.
// Every mechanism in this milestone reads this switch, so trials
// can measure off vs on vs on+auto-suggest vs cards.
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
	// LibraryCards wires search and load with nothing injected by
	// default: no L1 catalog, no task-start suggestions. Retrieval
	// is pull-only (search_skills/load_skill) plus error-triggered
	// cards (see cards.go) appended to failed tool results.
	LibraryCards LibraryMode = "cards"
)

// EnvSkillsMode is the environment variable carrying the switch.
const EnvSkillsMode = "EXCALIBUR_SKILLS"

// EnvSkillsErrorCards enables the error-triggered card excerpt in
// cards mode: EXCALIBUR_SKILLS_ERROR_CARDS=0 (or false/no/off)
// disables it. It has no effect in other modes. The default is on:
// a card fired by a concrete error costs a few hundred chars only
// when the run already failed a step.
const EnvSkillsErrorCards = "EXCALIBUR_SKILLS_ERROR_CARDS"

// DefaultLibraryMode returns the default switch position.
func DefaultLibraryMode() LibraryMode { return LibraryCards }

// normalizeLibraryMode maps "" and unknown values to cards (the
// zero value stays the helpful default: inject nothing rather than
// guessing) and lowercases the rest.
func normalizeLibraryMode(s string) LibraryMode {
	switch LibraryMode(strings.ToLower(strings.TrimSpace(s))) {
	case LibraryOff:
		return LibraryOff
	case LibraryOn:
		return LibraryOn
	case LibraryAuto:
		return LibraryAuto
	case LibraryCards, "":
		return LibraryCards
	default:
		return LibraryCards
	}
}

// ParseLibraryMode parses s into a LibraryMode, defaulting empty
// to cards and rejecting unknown values with the valid set.
func ParseLibraryMode(s string) (LibraryMode, error) {
	m := LibraryMode(strings.ToLower(strings.TrimSpace(s)))
	switch m {
	case LibraryOff, LibraryOn, LibraryAuto, LibraryCards:
		return m, nil
	case "":
		return LibraryCards, nil
	default:
		return LibraryCards, fmt.Errorf("skills: unknown library mode %q (want off, on, auto or cards)", s)
	}
}

// LibraryModeFromEnv reads the switch from the environment:
// EXCALIBUR_SKILLS=off|on|auto|cards, defaulting to cards when
// unset or invalid. getenv is os.Getenv in production and a stub in
// tests.
func LibraryModeFromEnv(getenv func(string) string) LibraryMode {
	if getenv == nil {
		getenv = os.Getenv
	}
	v := strings.TrimSpace(getenv(EnvSkillsMode))
	if v == "" {
		return LibraryCards
	}
	return normalizeLibraryMode(v)
}

// ErrorCardsFromEnv reports whether error-triggered cards are
// enabled: EXCALIBUR_SKILLS_ERROR_CARDS=0|false|no|off disables,
// anything else (including unset) enables.
func ErrorCardsFromEnv(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	v := strings.ToLower(strings.TrimSpace(getenv(EnvSkillsErrorCards)))
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// CatalogEnabled reports whether the L1 catalog and the skill tools
// are wired under this mode. Cards wires the tools (pull-only
// search and load) but never renders the injected catalog: see
// RenderCatalogForMode.
func (m LibraryMode) CatalogEnabled() bool {
	return normalizeLibraryMode(string(m)) != LibraryOff
}

// AutoSuggestEnabled reports whether task-start auto-suggest runs
// under this mode.
func (m LibraryMode) AutoSuggestEnabled() bool {
	return normalizeLibraryMode(string(m)) == LibraryAuto
}

// CardsEnabled reports whether the pull-only cards behavior applies:
// nothing injected by default, retrieval on demand plus
// error-triggered excerpts.
func (m LibraryMode) CardsEnabled() bool {
	return normalizeLibraryMode(string(m)) == LibraryCards
}

// RenderCatalogForMode renders the L1 catalog, or "" when the mode
// disables it (off) or injects nothing (cards). The return value
// feeds ctxcompiler's SkillCatalog verbatim, so off and cards stay
// out of the prompt; in cards mode the caller renders CardsPointer
// instead.
func (ix *Index) RenderCatalogForMode(cfg CatalogConfig, mode LibraryMode) string {
	if !mode.CatalogEnabled() || mode.CardsEnabled() {
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
