package skills

import (
	"fmt"
	"strings"
)

// Diagnostic is one validation finding for a skill directory
// (Section 12.1: an invalid skill is excluded and its diagnosis
// stays visible).
type Diagnostic struct {
	// Skill is the directory (candidate skill) name.
	Skill string
	// Path is the directory holding (or meant to hold) SKILL.md.
	Path string
	// Code is a short machine-readable reason.
	Code string
	// Detail explains the finding in English.
	Detail string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("skills: %s (%s): %s", d.Skill, d.Code, d.Detail)
}

// Diagnosis codes.
const (
	CodeMissingFile    = "missing_file"
	CodeBadFrontmatter = "bad_frontmatter"
	CodeBadName        = "bad_name"
	CodeNameMismatch   = "name_mismatch"
	CodeBadDescription = "bad_description"
	CodeBadCompat      = "bad_compatibility"
	CodeBadGroup       = "bad_group"
	CodeBadState       = "bad_state"
	CodeBadTool        = "bad_tool"
	CodeEmptyBody      = "empty_body"
)

// validName reports whether name follows Section 12.1: 1-64
// characters, lowercase letters, digits and hyphens, with no
// leading, trailing or consecutive hyphens.
func validName(name string) bool {
	if len(name) < 1 || len(name) > MaxNameLength {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	if strings.Contains(name, "--") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

// Validate checks a parsed skill file against Section 12.1. dirName
// is the name of the directory holding SKILL.md and must match the
// frontmatter name. groupOK reports whether a group id exists;
// toolOK reports whether a declared tool name exists (nil means the
// tool cross-check is skipped). It returns every diagnosis found;
// an empty list means valid.
func Validate(dirName, dirPath string, sf SkillFile, groupOK, toolOK func(string) bool) []Diagnostic {
	var diags []Diagnostic
	add := func(code, detail string) {
		diags = append(diags, Diagnostic{Skill: dirName, Path: dirPath, Code: code, Detail: detail})
	}
	fm := sf.Frontmatter
	if !validName(fm.Name) {
		add(CodeBadName, fmt.Sprintf("name %q must be 1-64 chars of lowercase letters, digits and hyphens, without leading, trailing or repeated hyphens", fm.Name))
	} else if fm.Name != dirName {
		add(CodeNameMismatch, fmt.Sprintf("frontmatter name %q does not match directory %q", fm.Name, dirName))
	}
	desc := strings.TrimSpace(fm.Description)
	if len(desc) < 1 || len(fm.Description) > MaxDescriptionLength {
		add(CodeBadDescription, fmt.Sprintf("description must be 1-%d characters, saying what the skill does and when to use it", MaxDescriptionLength))
	}
	if len(fm.Compatibility) > MaxCompatLength {
		add(CodeBadCompat, fmt.Sprintf("compatibility must be at most %d characters", MaxCompatLength))
	}
	if groupOK != nil && !groupOK(fm.Metadata.Excalibur.Group) {
		add(CodeBadGroup, fmt.Sprintf("unknown group %q: a skill belongs to exactly one group of the catalog", fm.Metadata.Excalibur.Group))
	}
	if st := fm.Metadata.Excalibur.State; st != "" && !ValidState(st) {
		add(CodeBadState, fmt.Sprintf("unknown state %q: want one of active, draft, obsolete, archived", string(st)))
	}
	if toolOK != nil {
		for _, t := range fm.Metadata.Excalibur.Tools {
			if !toolOK(t) {
				add(CodeBadTool, fmt.Sprintf("unknown tool %q in metadata.excalibur.tools", t))
			}
		}
		for _, t := range fm.AllowedTools {
			if !toolOK(t) {
				add(CodeBadTool, fmt.Sprintf("unknown tool %q in allowed-tools", t))
			}
		}
	}
	if strings.TrimSpace(sf.Body) == "" {
		add(CodeEmptyBody, "SKILL.md body is empty: the body holds the instructions")
	}
	return diags
}

// ShortDescription truncates d to the L1 budget of MaxShortDescription
// characters, cutting at a word boundary with an ellipsis marker.
func ShortDescription(d string) string {
	d = strings.Join(strings.Fields(strings.TrimSpace(d)), " ")
	if len(d) <= MaxShortDescription {
		return d
	}
	cut := MaxShortDescription - 1
	for cut > 0 && d[cut] != ' ' {
		cut--
	}
	if cut <= 0 {
		return d[:MaxShortDescription-1] + "…"
	}
	return d[:cut] + "…"
}
