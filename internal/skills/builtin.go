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
