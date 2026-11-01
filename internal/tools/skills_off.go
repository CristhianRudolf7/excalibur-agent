package tools

import (
	"os"
	"strings"
)

// SkillToolDisabled enforces the explicit skills ablation at discovery and
// execution boundaries. Unset keeps the ordinary assistant surface unchanged.
func SkillToolDisabled(name string) bool {
	return (name == "search_skills" || name == "load_skill") &&
		strings.EqualFold(strings.TrimSpace(os.Getenv("EXCALIBUR_SKILLS")), "off")
}

// WithoutDisabledSkills returns a fresh list respecting EXCALIBUR_SKILLS.
func WithoutDisabledSkills(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !SkillToolDisabled(name) {
			out = append(out, name)
		}
	}
	return out
}
