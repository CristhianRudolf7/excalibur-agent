package skills

import (
	"fmt"
	"strings"
)

// Section is one headed block of a skill body: the level-2 heading
// text and the Markdown content that follows it, up to the next
// level-1 or level-2 heading. Text before the first heading is the
// preamble (Section Heading is empty).
type Section struct {
	// Heading is the "##" heading text, or "" for the preamble.
	Heading string
	// Body is the raw Markdown content of the section.
	Body string
}

// SectionsOf splits a skill body into the preamble plus one Section
// per level-2 ("##") heading. Level-3 and deeper headings stay inside
// their parent section. The split is deterministic and purely
// textual, so load_skill can serve a focused slice without changing
// the stored body (the cache prefix never moves: results still land
// in L4 history).
func SectionsOf(body string) []Section {
	var out []Section
	var cur *Section
	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimRight(cur.Body, "\n")
			out = append(out, *cur)
			cur = nil
		}
	}
	start := func(heading string) {
		flush()
		cur = &Section{Heading: heading}
	}
	// The preamble owns text before the first "##" heading.
	pre := &Section{}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") || t == "##" {
			head := strings.TrimSpace(strings.TrimPrefix(t, "##"))
			start(head)
			continue
		}
		if cur != nil {
			cur.Body += line + "\n"
		} else {
			pre.Body += line + "\n"
		}
	}
	flush()
	pre.Body = strings.TrimRight(pre.Body, "\n")
	if strings.TrimSpace(pre.Body) != "" || len(out) == 0 {
		out = append([]Section{*pre}, out...)
	}
	return out
}

// SectionNames returns the "##" heading texts of body in order.
func SectionNames(body string) []string {
	var out []string
	for _, s := range SectionsOf(body) {
		if s.Heading != "" {
			out = append(out, s.Heading)
		}
	}
	return out
}

// normHeading lowercases and trims a heading or query for
// section matching.
func normHeading(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// matchSection finds the section whose heading equals the query
// (case-insensitive) or uniquely contains it. It returns false when
// nothing matches or the query is ambiguously contained in several
// headings.
func matchSection(sections []Section, query string) (Section, bool) {
	q := normHeading(query)
	if q == "" {
		return Section{}, false
	}
	for _, s := range sections {
		if s.Heading != "" && normHeading(s.Heading) == q {
			return s, true
		}
	}
	var cand *Section
	for i, s := range sections {
		if s.Heading != "" && strings.Contains(normHeading(s.Heading), q) {
			if cand != nil {
				return Section{}, false
			}
			c := sections[i]
			cand = &c
		}
	}
	if cand == nil {
		return Section{}, false
	}
	return *cand, true
}

// LoadSection implements the focused slice of load_skill(name,
// section): it returns the body of one "##" section instead of the
// whole SKILL.md, so a small model pays only for the slice it needs.
// An unknown or ambiguous section is an error listing the available
// headings; archived skills stay unloadable.
func (ix *Index) LoadSection(name, section string) (Loaded, error) {
	loaded, err := ix.Load(name)
	if err != nil {
		return Loaded{}, err
	}
	sections := SectionsOf(loaded.Body)
	s, ok := matchSection(sections, section)
	if !ok {
		return Loaded{}, &Diagnostic{
			Skill:  name,
			Code:   CodeBadSection,
			Detail: fmt.Sprintf("unknown section %q: use one of: %s", section, strings.Join(SectionNames(loaded.Body), "; ")),
		}
	}
	loaded.Section = s.Heading
	loaded.Sections = SectionNames(loaded.Body)
	if s.Heading != "" {
		loaded.Body = "## " + s.Heading + "\n\n" + strings.Trim(s.Body, "\n") + "\n"
	} else {
		loaded.Body = strings.Trim(s.Body, "\n") + "\n"
	}
	return loaded, nil
}

// LoadRef implements the references leg of load_skill(name, ref): it
// returns the content of one references/ file of the skill instead
// of SKILL.md. SKILL.md bodies link their references by file name;
// ref matches the file name with or without the .md suffix
// (case-insensitive). An unknown ref is an error listing the files.
func (ix *Index) LoadRef(name, ref string) (Loaded, error) {
	ix.mu.RLock()
	e, ok := ix.entries[name]
	if !ok {
		ix.mu.RUnlock()
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeMissingFile, Detail: "unknown skill: use search_skills to see what exists"}
	}
	if e.State == StateArchived {
		ix.mu.RUnlock()
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeBadState, Detail: "skill is archived and cannot be loaded"}
	}
	refs := append([]string(nil), e.Refs...)
	entry := e.clone()
	ix.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(ref))
	q = strings.TrimSuffix(q, ".md")
	found := ""
	for _, r := range refs {
		if strings.TrimSuffix(strings.ToLower(r), ".md") == q {
			found = r
			break
		}
	}
	if found == "" {
		return Loaded{}, &Diagnostic{
			Skill:  name,
			Code:   CodeBadRef,
			Detail: fmt.Sprintf("unknown reference %q: use one of: %s", ref, strings.Join(refs, ", ")),
		}
	}
	raw, err := ix.readRef(entry, found)
	if err != nil {
		return Loaded{}, &Diagnostic{Skill: name, Code: CodeMissingFile, Detail: fmt.Sprintf("cannot read reference %q: %v", found, err)}
	}
	return Loaded{
		Name: name, Group: entry.Group, Version: entry.Version, Hash: entry.Hash,
		Body:         string(raw),
		Tools:        append([]string(nil), entry.Tools...),
		AllowedTools: append([]string(nil), entry.AllowedTools...),
		Requires:     entry.Requires,
		RefName:      found,
		Sections:     SectionNames(entry.Body),
		Refs:         refs,
	}, nil
}
