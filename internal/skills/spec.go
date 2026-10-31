package skills

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter field limits from Section 12.1.
const (
	MaxNameLength        = 64
	MaxDescriptionLength = 1024
	MaxCompatLength      = 500
	// MaxShortDescription is the per-skill budget inside a fully
	// listed L1 group (Section 12.3): name plus a short
	// description of at most 120 characters.
	MaxShortDescription = 120
)

// State is the lifecycle state of an indexed skill (Section 12.2).
type State string

// Skill states.
const (
	StateActive   State = "active"
	StateDraft    State = "draft"
	StateObsolete State = "obsolete"
	StateArchived State = "archived"
)

// ValidState reports whether s is a known skill state.
func ValidState(s State) bool {
	switch s {
	case StateActive, StateDraft, StateObsolete, StateArchived:
		return true
	}
	return false
}

// Requires names the external prerequisites a skill declares in
// metadata.excalibur.requires: binaries on PATH and environment
// variables (Section 12.1). Either form is accepted in SKILL.md:
//
//	requires:
//	  bin: [git, gh]
//	  env: [GITHUB_TOKEN]
//
// or the short list form (binaries): `requires: [git, gh]`.
type Requires struct {
	Bin []string `yaml:"bin"`
	Env []string `yaml:"env"`
}

// UnmarshalYAML accepts both the mapping and the short list form.
func (r *Requires) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.SequenceNode {
		var bins []string
		if err := node.Decode(&bins); err != nil {
			return err
		}
		r.Bin = bins
		return nil
	}
	type plain Requires
	return node.Decode((*plain)(r))
}

// Display holds the localized names and descriptions used to show a
// skill in the UI without translating the skill itself
// (Section 12.1: metadata.excalibur.display, Spanish and English).
type Display struct {
	NameEN string `yaml:"name_en"`
	NameES string `yaml:"name_es"`
	DescEN string `yaml:"desc_en"`
	DescES string `yaml:"desc_es"`
}

// ExcaliburMeta holds the Excalibur-owned metadata under the
// `metadata.excalibur` key permitted by the Agent Skills spec
// (Section 12.1). A skill belongs to exactly one group (12.3).
type ExcaliburMeta struct {
	Group     string   `yaml:"group"`
	Version   string   `yaml:"version"`
	Platforms []string `yaml:"platforms"`
	Requires  Requires `yaml:"requires"`
	Tools     []string `yaml:"tools"`
	Display   Display  `yaml:"display"`
	// State is the initial lifecycle state (Section 12.2). Empty
	// means active; the index manages later transitions.
	State State `yaml:"state"`
}

// Frontmatter is the YAML header of SKILL.md (Section 12.1):
// mandatory name and description, optional compatibility, plus the
// Agent Skills `metadata` block and the top-level `allowed-tools`
// restriction.
type Frontmatter struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	Compatibility string `yaml:"compatibility"`
	License       string `yaml:"license"`
	Metadata      struct {
		Excalibur ExcaliburMeta `yaml:"excalibur"`
	} `yaml:"metadata"`
	// AllowedTools, when present, is honored as an additional
	// restriction on the tool set while the skill is loaded
	// (Section 12.1). The engine applies it; the index parses,
	// validates and exposes it.
	AllowedTools []string `yaml:"allowed-tools"`
}

// SkillFile is a parsed SKILL.md: its frontmatter, Markdown body
// and the headings of the body (used for lexical suggestion,
// Section 12.4).
type SkillFile struct {
	Frontmatter Frontmatter
	Body        string
	Headings    []string
}

// ParseSkillFile parses raw SKILL.md content. The file must start
// with a `---` YAML fence holding at least name and description,
// followed by the Markdown body.
func ParseSkillFile(content []byte) (SkillFile, error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	if text != "---" && !strings.HasPrefix(text, "---\n") {
		return SkillFile{}, fmt.Errorf("skills: SKILL.md must start with a --- YAML fence")
	}
	after := text[len("---"):]
	// Find the closing fence: a line holding exactly ---.
	lines := strings.Split(after, "\n")
	if len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	closeAt := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "---" {
			closeAt = i
			break
		}
	}
	if closeAt < 0 {
		return SkillFile{}, fmt.Errorf("skills: SKILL.md frontmatter is not closed with ---")
	}
	head := strings.Join(lines[:closeAt], "\n")
	body := strings.TrimLeft(strings.Join(lines[closeAt+1:], "\n"), "\n")
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(head), &fm); err != nil {
		return SkillFile{}, fmt.Errorf("skills: bad YAML frontmatter: %w", err)
	}
	if strings.TrimSpace(body) == "" {
		return SkillFile{}, fmt.Errorf("skills: SKILL.md has an empty body after the frontmatter")
	}
	return SkillFile{Frontmatter: fm, Body: body, Headings: extractHeadings(body)}, nil
}

// extractHeadings returns the Markdown ATX heading texts of body,
// without the leading hashes.
func extractHeadings(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		t = strings.TrimSpace(strings.TrimLeft(t, "#"))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
