package cli

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
)

// builtinSkillsForProfile keeps the updated assistant-only Office instructions
// out of eval while preserving every other entry in its existing skill catalog.
func builtinSkillsForProfile(profile ProfileName) *skills.Index {
	if profile == ProfileEval {
		return skills.NewEvalBuiltinIndex()
	}
	return skills.NewBuiltinIndex()
}
