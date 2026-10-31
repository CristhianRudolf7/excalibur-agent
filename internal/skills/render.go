package skills

import (
	"fmt"
	"sort"
	"strings"
)

// Catalog budget policy (Section 12.3, Appendix B): at most 2% of
// the window or 4K tokens, whichever is smaller.
const (
	CatalogFraction = 0.02
	CatalogMax      = 4000
)

// EstimateTokens approximates token cost as chars/4 when no
// counter is available (the OpenClaw estimate cited in 12.3 is
// ~97 chars per skill).
func EstimateTokens(s string) int {
	n := len(s) / 4
	if len(s)%4 != 0 {
		n++
	}
	return n
}

// CatalogBudget returns the L1 skill-catalog token budget for a
// window of windowTokens tokens.
func CatalogBudget(windowTokens int) int {
	byFraction := int(float64(windowTokens) * CatalogFraction)
	if byFraction < CatalogMax {
		return byFraction
	}
	return CatalogMax
}

// CatalogConfig tunes RenderCatalog. Counter converts rendered text
// to tokens; nil means EstimateTokens. Compact lists skill names
// without descriptions for fully listed groups: fewer tokens per
// skill for small models that load details on demand with
// load_skill. The grouped shape and budget policy are unchanged.
type CatalogConfig struct {
	WindowTokens int
	Pertinent    []string
	Counter      func(string) int
	Compact      bool
}

// DefaultPertinentForRole maps a caller role/task kind to the
// pertinent groups with deterministic rules (Section 12.3): a
// programming task lists planning, software-engineering, debugging,
// testing and verification complete; a general conversation lists
// communication, memory and personal-assistant. Doubt goes to the
// decider (H12); this function is the deterministic leg.
func DefaultPertinentForRole(kind string) []string {
	switch kind {
	case "programming":
		return []string{"planning", "software-engineering", "debugging", "testing", "verification",
			"terminal", "build", "packaging", "languages", "networking", "containers", "databases", "data", "ml"}
	case "conversation":
		return []string{"communication", "memory", "personal-assistant"}
	default:
		return []string{"communication", "personal-assistant"}
	}
}

// RenderCatalog renders the L1 catalog grouped, not as a flat list
// (Section 12.3): pertinent groups list every skill with name and
// a short description (at most 120 chars); the rest appear only as
// a header with their skill count ("security — 11 skills; use
// search_skills"). The output respects the 2%-or-4K budget: fully
// listed groups keep priority order and later groups degrade to
// headers when the budget runs out.
func (ix *Index) RenderCatalog(cfg CatalogConfig) string {
	counter := cfg.Counter
	if counter == nil {
		counter = EstimateTokens
	}
	budget := CatalogBudget(cfg.WindowTokens)
	pertinent := map[string]bool{}
	var order []string
	for _, g := range cfg.Pertinent {
		if _, ok := GroupByID(g); !ok {
			continue
		}
		if !pertinent[g] {
			pertinent[g] = true
			order = append(order, g)
		}
	}
	all := ix.All()
	byGroup := map[string][]*Entry{}
	for _, e := range all {
		if e.State == StateArchived {
			continue
		}
		byGroup[e.Group] = append(byGroup[e.Group], e)
	}
	for _, list := range byGroup {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	}

	var b strings.Builder
	used := 0
	stopped := false
	// emitLine appends one line when it fits the remaining budget.
	// When nothing was emitted yet and the first line alone
	// exceeds the budget, it is truncated, so the output never
	// exceeds the budget (Section 12.3: at most 2% or 4K tokens).
	emitLine := func(line string) {
		if stopped {
			return
		}
		if used == 0 && counter(line) > budget {
			// Reserve one char for the trailing newline so the
			// truncated output stays within the token budget.
			maxChars := budget * 4
			if maxChars > 16 {
				maxChars--
			}
			runes := []rune(line)
			if len(runes) > maxChars {
				runes = runes[:maxChars]
			}
			b.WriteString(string(runes))
			if !strings.HasSuffix(string(runes), "\n") {
				b.WriteString("\n")
			}
			used = budget
			stopped = true
			return
		}
		if used+counter(line) > budget {
			stopped = true
			return
		}
		b.WriteString(line)
		used += counter(line)
	}
	emit := func(text string) bool {
		if stopped {
			return false
		}
		for _, ln := range strings.SplitAfter(text, "\n") {
			if ln == "" {
				continue
			}
			emitLine(ln)
			if stopped {
				// Budget ran out mid-chunk: a partially listed
				// group must not count as fully listed.
				return false
			}
		}
		return true
	}
	full := map[string]bool{}
	// Fully listed pertinent groups first.
	for _, g := range order {
		list := byGroup[g]
		if len(list) == 0 {
			continue
		}
		grp, _ := GroupByID(g)
		var gb strings.Builder
		fmt.Fprintf(&gb, "[%s] %s (%s): %d skills\n", grp.ID, grp.NameEN, grp.Origin, len(list))
		for _, e := range list {
			if cfg.Compact {
				fmt.Fprintf(&gb, "- %s\n", e.Name)
			} else {
				fmt.Fprintf(&gb, "- %s: %s\n", e.Name, ShortDescription(e.Description))
			}
		}
		if emit(gb.String()) {
			full[g] = true
		} else {
			break
		}
	}
	// Remaining groups as headers only.
	rest := []Group{}
	for _, g := range Groups() {
		if full[g.ID] {
			continue
		}
		if len(byGroup[g.ID]) == 0 {
			continue
		}
		rest = append(rest, g)
	}
	for _, g := range rest {
		n := len(byGroup[g.ID])
		line := fmt.Sprintf("[%s] %s — %d skills; use search_skills\n", g.ID, g.NameEN, n)
		if !emit(line) {
			break
		}
	}
	return b.String()
}

// CatalogHash returns a stable fingerprint of the current catalog
// content (names plus entry hashes): sessions compare it to detect
// drift without re-rendering.
func (ix *Index) CatalogHash() string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var b strings.Builder
	names := make([]string, 0, len(ix.entries))
	for n := range ix.entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b.WriteString(n + "=" + ix.entries[n].Hash + "\n")
	}
	return hashContent([]byte(b.String()))
}
