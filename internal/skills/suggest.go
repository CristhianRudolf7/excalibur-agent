package skills

import (
	"math"
	"sort"
	"strings"
)

// Suggestion limits and evidence thresholds (Sections 12.4,
// Appendix B).
const (
	// DefaultSuggestionsMax caps suggested skills per task: 2-3
	// focused skills help, 4 or more degrade the gain.
	DefaultSuggestionsMax = 3
	// minEvidenceTasks is the smallest outcome sample before the
	// selector trusts negative per-model evidence.
	minEvidenceTasks = 5
	// negativeGap disables a group for a model when tasks using
	// its skills succeed this much less often than tasks not
	// using them.
	negativeGap = 0.10
)

// Suggestion is one skill proposed for a task: the skill plus a
// one-line reason (Section 12.4, step 1: at most 3 enter L3).
type Suggestion struct {
	Name  string
	Group string
	Why   string
	Score float64
	Entry *Entry
}

// Hit is one search_skills match: name, description, group and
// path (Section 12.4, step 3).
type Hit struct {
	Name        string
	Description string
	Group       string
	Path        string
}

// Loaded is the result of load_skill: the SKILL.md body with the
// metadata the caller needs to resolve deferred tools and honor
// restrictions (Sections 12.4 steps 2 and 4, 12.1).
type Loaded struct {
	Name         string
	Group        string
	Version      string
	Hash         string
	Body         string
	Tools        []string
	AllowedTools []string
	Requires     Requires
}

// tokenize lowercases text into alphanumeric tokens.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

// fieldWeights score name matches above description above headings,
// mirroring "BM25 over name, description and body headings".
const (
	weightName = 3.0
	weightDesc = 2.0
	weightHead = 1.0
)

// lexicalScore is a deterministic BM25-style score of entry e
// against query terms: per-field term frequency saturated as
// tf/(tf+1), weighted per field, times an IDF computed over the
// candidate set.
func lexicalScore(terms []string, e *Entry, df map[string]int, n int) float64 {
	nameTok := tokenize(strings.ReplaceAll(e.Name, "-", " ") + " " + e.Name)
	descTok := tokenize(e.Description)
	headTok := tokenize(strings.Join(e.Headings, " "))
	tf := func(toks []string, t string) float64 {
		c := 0
		for _, x := range toks {
			if x == t {
				c++
			}
		}
		if c == 0 {
			return 0
		}
		return float64(c) / float64(c+1)
	}
	var score float64
	for _, t := range terms {
		idf := math.Log(1 + float64(n)/float64(1+df[t]))
		score += idf * (weightName*tf(nameTok, t) + weightDesc*tf(descTok, t) + weightHead*tf(headTok, t))
	}
	return score
}

// scoreEntries ranks entries against query with the lexical score
// plus the optional vector leg.
func (ix *Index) scoreEntries(query string, all []*Entry) []Suggestion {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	df := map[string]int{}
	seen := map[string]map[string]bool{}
	for _, e := range all {
		seen[e.Name] = map[string]bool{}
		for _, t := range append(append(tokenize(strings.ReplaceAll(e.Name, "-", " ")), tokenize(e.Description)...), tokenize(strings.Join(e.Headings, " "))...) {
			if !seen[e.Name][t] {
				seen[e.Name][t] = true
				df[t]++
			}
		}
	}
	ix.mu.RLock()
	vector := ix.vector
	ix.mu.RUnlock()
	var out []Suggestion
	for _, e := range all {
		s := lexicalScore(terms, e, df, len(all))
		if vector != nil {
			s += vector.Score(query, e)
		}
		if s > 0 {
			out = append(out, Suggestion{Name: e.Name, Group: e.Group, Score: s, Entry: e})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// candidates returns the suggestible entries: indexed, active, and
// not disabled for model by negative evidence.
func (ix *Index) candidates(model string) []*Entry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []*Entry
	for _, e := range ix.entries {
		if e.State == StateArchived {
			continue
		}
		if ix.disabled[model][e.Group] {
			continue
		}
		out = append(out, e.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Suggest consults the index with the task description and returns
// at most max suggestions with a one-line reason each
// (Section 12.4, step 1). max <= 0 means DefaultSuggestionsMax;
// the result never exceeds DefaultSuggestionsMax.
func (ix *Index) Suggest(query, model string, max int) []Suggestion {
	if max <= 0 || max > DefaultSuggestionsMax {
		max = DefaultSuggestionsMax
	}
	ranked := ix.scoreEntries(query, ix.candidates(model))
	if len(ranked) > max {
		ranked = ranked[:max]
	}
	for i := range ranked {
		ranked[i].Why = "matches " + strings.Join(topTerms(query, ranked[i].Entry), ", ")
	}
	return ranked
}

// topTerms picks up to 3 query terms that hit the entry for the
// one-line reason.
func topTerms(query string, e *Entry) []string {
	hay := strings.ToLower(strings.ReplaceAll(e.Name, "-", " ") + " " + e.Description + " " + strings.Join(e.Headings, " "))
	var out []string
	for _, t := range tokenize(query) {
		if strings.Contains(hay, t) {
			dup := false
			for _, o := range out {
				if o == t {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, t)
			}
		}
		if len(out) == 3 {
			break
		}
	}
	if len(out) == 0 {
		return []string{"the task"}
	}
	return out
}

// Search implements search_skills(query, group?): entries whose
// name, description, group or path match, optionally restricted to
// one group (Section 12.4, step 3). Empty query lists the group.
func (ix *Index) Search(query, group string) []Hit {
	ix.mu.RLock()
	all := make([]*Entry, 0, len(ix.entries))
	for _, e := range ix.entries {
		if e.State == StateArchived {
			continue
		}
		if group != "" && e.Group != group {
			continue
		}
		all = append(all, e.clone())
	}
	ix.mu.RUnlock()
	if strings.TrimSpace(query) == "" {
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		return hitsOf(all)
	}
	ranked := ix.scoreEntries(query, all)
	// Fall back to substring matches when no term scores: short
	// queries (e.g. "git") may not tokenize onto indexed text.
	if len(ranked) == 0 {
		q := strings.ToLower(query)
		for _, e := range all {
			hay := strings.ToLower(e.Name + " " + e.Description + " " + e.Group + " " + e.Path)
			if strings.Contains(hay, q) {
				ranked = append(ranked, Suggestion{Entry: e})
			}
		}
		sort.Slice(ranked, func(i, j int) bool { return ranked[i].Entry.Name < ranked[j].Entry.Name })
	}
	out := make([]Hit, 0, len(ranked))
	for _, s := range ranked {
		out = append(out, Hit{Name: s.Entry.Name, Description: s.Entry.Description, Group: s.Entry.Group, Path: s.Entry.Path})
	}
	return out
}

func hitsOf(all []*Entry) []Hit {
	out := make([]Hit, 0, len(all))
	for _, e := range all {
		out = append(out, Hit{Name: e.Name, Description: e.Description, Group: e.Group, Path: e.Path})
	}
	return out
}

// Load implements load_skill(name): it returns the SKILL.md body so
// the caller can append it as a tool result at the end of the
// history without touching the stable prefix (Section 12.4,
// step 2). References and scripts stay on disk for the normal file
// and execution tools. Loading resolves nothing by itself: the
// caller resolves the declared deferred tools (12.4 step 4, 13.3)
// and honors AllowedTools (12.1).
func (ix *Index) Load(name string) (Loaded, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	e, ok := ix.entries[name]
	if !ok {
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeMissingFile, Detail: "unknown skill: use search_skills to see what exists"}
	}
	if e.State == StateArchived {
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeBadState, Detail: "skill is archived and cannot be loaded"}
	}
	return Loaded{
		Name: name, Group: e.Group, Version: e.Version, Hash: e.Hash, Body: e.Body,
		Tools:        append([]string(nil), e.Tools...),
		AllowedTools: append([]string(nil), e.AllowedTools...),
		Requires:     e.Requires,
	}, nil
}

// RecordOutcome records one task result for the per-model skill
// effect measurement (Section 12.4, last paragraph): used tells
// whether the task used skill, success whether it succeeded. The
// selector disables suggestions of a group for a model when the
// evidence is negative.
func (ix *Index) RecordOutcome(skill, model string, used, success bool) {
	if skill == "" || model == "" {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ms := ix.stats[skill]
	if ms == nil {
		ms = map[string]*modelStats{}
		ix.stats[skill] = ms
	}
	st := ms[model]
	if st == nil {
		st = &modelStats{}
		ms[model] = st
	}
	if used {
		st.WithTotal++
		if success {
			st.WithSuccess++
		}
	} else {
		st.WithoutTotal++
		if success {
			st.WithoutSucc++
		}
	}
	// Recompute the group verdict for this model from the skills
	// in the same group.
	e, ok := ix.entries[skill]
	if !ok {
		return
	}
	var wt, ws, nt, ns int
	for name, per := range ix.stats {
		oe, ok := ix.entries[name]
		if !ok || oe.Group != e.Group {
			continue
		}
		if s, ok := per[model]; ok {
			wt += s.WithTotal
			ws += s.WithSuccess
			nt += s.WithoutTotal
			ns += s.WithoutSucc
		}
	}
	if ix.disabled[model] == nil {
		ix.disabled[model] = map[string]bool{}
	}
	if wt+nt >= minEvidenceTasks && wt > 0 && nt > 0 &&
		float64(ws)/float64(wt) < float64(ns)/float64(nt)-negativeGap {
		ix.disabled[model][e.Group] = true
	} else {
		delete(ix.disabled[model], e.Group)
	}
}

// SuggestDisabled reports whether a group's suggestions are
// disabled for model after negative evidence.
func (ix *Index) SuggestDisabled(model, group string) bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.disabled[model][group]
}
