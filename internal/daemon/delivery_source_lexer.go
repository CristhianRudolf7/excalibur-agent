package daemon

import (
	"path/filepath"
	"strings"
)

// This is a conservative lexical readability heuristic, not a language parser.
// Preserve newlines for diagnostics while removing comments and literal bodies.
// Template interpolations remain code; they cannot hide compressed handlers.
func sourceCodeOnly(path, source string) string {
	var code strings.Builder
	ext := filepath.Ext(path)
	hashComments := ext == ".py" || ext == ".rb" || ext == ".sh"
	var scan func(int, bool) int
	scan = func(index int, interpolation bool) int {
		depth := 0
		for index < len(source) {
			char := source[index]
			if interpolation && char == '}' && depth == 0 {
				code.WriteByte('}')
				return index + 1
			}
			if hashComments && char == '#' || !hashComments && strings.HasPrefix(source[index:], "//") || ext == ".sql" && strings.HasPrefix(source[index:], "--") {
				for index < len(source) && source[index] != '\n' {
					index++
				}
				continue
			}
			if !hashComments && strings.HasPrefix(source[index:], "/*") || strings.HasPrefix(source[index:], "<!--") {
				end := "*/"
				if char == '<' {
					end = "-->"
				}
				index += 2
				for index < len(source) && !strings.HasPrefix(source[index:], end) {
					if source[index] == '\n' {
						code.WriteByte('\n')
					}
					index++
				}
				index = min(len(source), index+len(end))
				continue
			}
			if char == '\'' || char == '"' || char == '`' {
				quote := string(char)
				if ext == ".py" && strings.HasPrefix(source[index:], strings.Repeat(quote, 3)) {
					quote = strings.Repeat(quote, 3)
				}
				code.WriteString(`"`)
				index += len(quote)
				for index < len(source) && !strings.HasPrefix(source[index:], quote) {
					if char == '`' && ext != ".go" && strings.HasPrefix(source[index:], "${") {
						code.WriteString("{")
						index = scan(index+2, true)
						continue
					}
					if source[index] == '\n' {
						code.WriteByte('\n')
					}
					if source[index] == '\\' && !(char == '`' && ext == ".go") && index+1 < len(source) {
						index++
						if source[index] == '\n' {
							code.WriteByte('\n')
						}
					}
					index++
				}
				code.WriteString(`"`)
				index = min(len(source), index+len(quote))
				continue
			}
			if char == '{' {
				depth++
			} else if char == '}' {
				depth--
			}
			code.WriteByte(char)
			index++
		}
		return index
	}
	scan(0, false)
	return code.String()
}
