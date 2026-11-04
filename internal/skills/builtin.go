package skills

import (
	"embed"
	"io/fs"
)

//go:embed builtin
var builtinFS embed.FS

// BuiltinFS is the embedded catalog of built-in skill files,
// organized by group: builtin/<group>/<skill>/SKILL.md
// (Section 12.5). It backs the lowest-precedence index root.
func BuiltinFS() fs.FS {
	sub, err := fs.Sub(builtinFS, "builtin")
	if err != nil {
		return builtinFS
	}
	return sub
}

// NewBuiltinIndex returns an index over the embedded built-in
// catalog only, with the tool cross-check enabled.
func NewBuiltinIndex() *Index {
	return NewIndex(Roots{}, BuiltinFS(), WithToolCheck(KnownTool))
}

//go:embed frozen_eval
var evalFrozenSkills embed.FS

// evalBuiltinFS preserves the skill bytes used by existing eval records for
// every skill with a snapshot under frozen_eval. Other builtin files still
// come from the normal catalog.
type evalBuiltinFS struct{ fs.FS }

func (f evalBuiltinFS) Open(name string) (fs.File, error) {
	// Only regular files are substituted: directory listings always come
	// from the normal catalog so skills without a snapshot stay visible.
	if info, err := fs.Stat(evalFrozenSkills, "frozen_eval/"+name); err == nil && !info.IsDir() {
		return evalFrozenSkills.Open("frozen_eval/" + name)
	}
	return f.FS.Open(name)
}

// NewEvalBuiltinIndex returns the frozen eval catalog. Keep baseline skill
// content here when assistant-only guidance changes, so recorded trials and
// profile hashes retain the same meaning.
func NewEvalBuiltinIndex() *Index {
	return NewIndex(Roots{}, evalBuiltinFS{BuiltinFS()}, WithToolCheck(KnownTool), WithExcludedNames("whiteboard-teaching", "robot-lab", "agent-avatars", "app-ui-quality"))
}
