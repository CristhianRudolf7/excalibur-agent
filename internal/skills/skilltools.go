package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Schemas for the skill tools (Section 13.2). search_skills and
// load_skill are read-only, idempotent and fast, so the master
// may use them too (Section 13.3). The query of search_skills is
// optional (empty lists the group) and load_skill accepts optional
// section/ref selectors, so small models that under-specify calls
// get guidance instead of errors.
var (
	searchSkillsSchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"group":{"type":"string"}},"additionalProperties":false}`)
	loadSkillSchema    = json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1},"section":{"type":"string"},"ref":{"type":"string"}},"required":["name"],"additionalProperties":false}`)
)

// SearchSkillsTool implements search_skills: search skills by text
// and group, returning name, description, group and path
// (Section 12.4, step 3).
type SearchSkillsTool struct {
	Index *Index
}

// Definition implements tools.Tool.
func (t *SearchSkillsTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "search_skills", Description: "Search skills by text and group; empty query lists the group; returns name, description, group and path",
		InputSchema: searchSkillsSchema,
		Effect:      "read_only", Idempotency: "idempotent",
		Timeout: 60 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Cancel: "immediate", Fast: true,
		Group: "skills", Tags: []string{"skills"}, Keywords: []string{"search", "skills"},
	}
}

// Invoke implements tools.Tool.
func (t *SearchSkillsTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Query string `json:"query"`
		Group string `json:"group"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	hits := t.Index.Search(a.Query, a.Group)
	if len(hits) == 0 {
		return tools.Result{Status: tools.StatusOK, Output: "no skills match"}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d skill(s):\n", len(hits))
	for _, h := range hits {
		fmt.Fprintf(&b, "- %s [%s] %s (%s)\n", h.Name, h.Group, h.Description, h.Path)
	}
	return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
}

// ResourceKeys implements tools.Tool.
func (t *SearchSkillsTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// LoadSkillTool implements load_skill: load a SKILL.md body and
// resolve its declared deferred tools (Section 12.4, steps 2
// and 4).
type LoadSkillTool struct {
	Index *Index
	// ResolveTools, when set, maps declared deferred tool names
	// to their schemas so the result carries them resolved with
	// the skill (Section 13.3). Nil means names are listed but
	// resolved by the tool catalog owner.
	ResolveTools func(names []string) []tools.Definition
}

// Definition implements tools.Tool.
func (t *LoadSkillTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "load_skill", Description: "Load a SKILL.md body and resolve its deferred tools; optional section loads one headed section, optional ref loads one references/ file",
		InputSchema: loadSkillSchema,
		Effect:      "read_only", Idempotency: "idempotent",
		Timeout: 60 * time.Second, OutputBudget: tools.DefaultOutputBudget,
		Cancel: "immediate", Fast: true,
		Group: "skills", Tags: []string{"skills"}, Keywords: []string{"load", "skill"},
	}
}

// Invoke implements tools.Tool.
func (t *LoadSkillTool) Invoke(_ context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Name    string `json:"name"`
		Section string `json:"section"`
		Ref     string `json:"ref"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if a.Name == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "name is required"}, nil
	}
	var loaded Loaded
	var err error
	switch {
	case a.Section != "":
		loaded, err = t.Index.LoadSection(a.Name, a.Section)
	case a.Ref != "":
		loaded, err = t.Index.LoadRef(a.Name, a.Ref)
	default:
		loaded, err = t.Index.Load(a.Name)
	}
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	var b strings.Builder
	locator := ""
	switch {
	case loaded.Section != "":
		locator = ", section " + loaded.Section
	case loaded.RefName != "":
		locator = ", ref " + loaded.RefName
	}
	fmt.Fprintf(&b, "# %s (group %s, version %s%s)\n\n", loaded.Name, loaded.Group, loaded.Version, locator)
	b.WriteString(loaded.Body)
	if !strings.HasSuffix(loaded.Body, "\n") {
		b.WriteString("\n")
	}
	// When serving a slice, point at the rest: a small model that
	// landed on one section or reference can reach the others with
	// another cheap call instead of loading the whole file.
	if loaded.Section != "" && len(loaded.Sections) > 1 {
		fmt.Fprintf(&b, "\nOther sections of %s: %s (load with section=...).\n", loaded.Name, strings.Join(loaded.Sections, "; "))
	}
	if loaded.RefName == "" && len(loaded.Refs) > 0 {
		fmt.Fprintf(&b, "\nDepth references of %s: %s (load with ref=...).\n", loaded.Name, strings.Join(loaded.Refs, ", "))
	}
	if len(loaded.Requires.Bin) > 0 || len(loaded.Requires.Env) > 0 {
		b.WriteString("\nRequires:")
		for _, bin := range loaded.Requires.Bin {
			fmt.Fprintf(&b, " binary %q", bin)
		}
		for _, env := range loaded.Requires.Env {
			fmt.Fprintf(&b, " env %q", env)
		}
		b.WriteString("\n")
	}
	if len(loaded.AllowedTools) > 0 {
		fmt.Fprintf(&b, "\nWhile this skill is active, only these tools are allowed in addition to the core set: %s\n", strings.Join(loaded.AllowedTools, ", "))
	}
	if len(loaded.Tools) > 0 {
		fmt.Fprintf(&b, "\nDeferred tools used by this skill: %s\n", strings.Join(loaded.Tools, ", "))
		if t.ResolveTools != nil {
			for _, d := range t.ResolveTools(loaded.Tools) {
				fmt.Fprintf(&b, "- %s: %s\n", d.Name, d.Description)
			}
		}
		fmt.Fprintf(&b, "Read their inputs with search_tools or call them with call_tool.\n")
	}
	return tools.Result{Status: tools.StatusOK, Output: b.String()}, nil
}

// ResourceKeys implements tools.Tool.
func (t *LoadSkillTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// SkillTools returns the live search_skills/load_skill tools bound
// to ix. resolve wires the deferred-tool resolution of 13.3.
func SkillTools(ix *Index, resolve func([]string) []tools.Definition) []tools.Tool {
	return []tools.Tool{
		&SearchSkillsTool{Index: ix},
		&LoadSkillTool{Index: ix, ResolveTools: resolve},
	}
}
