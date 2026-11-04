package attempts

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// WorkdirMap maps the real working directory onto one attempt's copy.
// Absolute paths under Real, relative path arguments (resolved against
// the attempt's working directory) and shell commands are rewritten
// through it when an attempt calls a tool. The instruction text the
// model reads is never rewritten, so text that merely mentions the
// real path is left alone.
type WorkdirMap struct {
	// Real is the absolute real working directory (for example /app).
	Real string
	// Attempt is the absolute copy the attempt works in.
	Attempt string
}

// Enabled reports whether the map rewrites anything. The filesystem
// root is never mapped.
func (m WorkdirMap) Enabled() bool {
	if m.Real == "" || m.Attempt == "" || !filepath.IsAbs(m.Real) || !filepath.IsAbs(m.Attempt) {
		return false
	}
	if sameWorkdir(m.Real, m.Attempt) {
		return false
	}
	return filepath.Dir(m.Real) != m.Real
}

// Path maps an absolute path under Real onto Attempt. Any other path
// (relative, outside Real, or a sibling sharing the prefix such as
// /app2 for /app) is returned unchanged.
func (m WorkdirMap) Path(p string) string {
	if !m.Enabled() || p == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, ok := relWithin(m.Real, filepath.Clean(p))
	if !ok {
		rel, ok = relWithin(platform.CanonicalPath(m.Real), platform.CanonicalPath(p))
	}
	if !ok {
		return p
	}
	return filepath.Join(m.Attempt, rel)
}

// Resolve returns the attempt path for a path an attempt names:
// absolute paths map through Path, relative paths resolve under
// Attempt. Empty paths and disabled maps are returned unchanged.
func (m WorkdirMap) Resolve(p string) string {
	if !m.Enabled() || p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return m.Path(p)
	}
	return filepath.Join(m.Attempt, p)
}

// Command rewrites occurrences of the real directory inside a shell
// command. An occurrence counts only at a word boundary: /app matches
// in "cd /app && make", "cp /app/x y" and "'/app'", but not in
// "/application" or "/app.bak".
func (m WorkdirMap) Command(cmd string) string {
	if !m.Enabled() || commandPathIndex(cmd, m.Real) < 0 {
		return cmd
	}
	var b strings.Builder
	i := 0
	for {
		j := commandPathIndex(cmd[i:], m.Real)
		if j < 0 {
			break
		}
		start := i + j
		end := start + len(m.Real)
		if boundaryBefore(cmd, start) && boundaryAfter(cmd, end) {
			b.WriteString(cmd[i:start])
			b.WriteString(m.Attempt)
		} else {
			b.WriteString(cmd[i:end])
		}
		i = end
	}
	b.WriteString(cmd[i:])
	return b.String()
}

// MapArgs returns the tool arguments with every path-valued field and
// shell command rewritten through the map. Path-valued keys are the
// directory keys (dir, directory, root, workdir, cwd) and every key
// ending in "path" or "paths", at any depth. A "command" value is a
// shell command. For the exec tool a missing workdir becomes the
// attempt directory, so commands run in the copy by default.
//
// Arguments that are not a JSON object, and arguments with nothing to
// rewrite, come back byte-identical.
func (m WorkdirMap) MapArgs(tool string, args json.RawMessage) json.RawMessage {
	if !m.Enabled() || len(args) == 0 {
		return args
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return args
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return args
	}
	changed := m.mapObject(obj)
	if tool == "exec" {
		if wd, _ := obj["workdir"].(string); wd == "" {
			obj["workdir"] = m.Attempt
			changed = true
		}
	}
	if !changed {
		return args
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return args
	}
	return out
}

func (m WorkdirMap) mapObject(obj map[string]any) bool {
	changed := false
	for k, v := range obj {
		if nv, c := m.mapValue(k, v); c {
			obj[k] = nv
			changed = true
		}
	}
	return changed
}

func (m WorkdirMap) mapValue(key string, v any) (any, bool) {
	switch t := v.(type) {
	case string:
		if key == "command" {
			out := m.Command(t)
			return out, out != t
		}
		if isPathKey(key) {
			out := m.Resolve(t)
			return out, out != t
		}
	case []any:
		changed := false
		for i, e := range t {
			if ne, c := m.mapValue(key, e); c {
				t[i] = ne
				changed = true
			}
		}
		return t, changed
	case map[string]any:
		return t, m.mapObject(t)
	}
	return v, false
}

// isPathKey names argument keys whose string values are filesystem
// paths. Keys are matched case-insensitively.
func isPathKey(key string) bool {
	k := strings.ToLower(key)
	switch k {
	case "dir", "directory", "root", "workdir", "cwd":
		return true
	}
	return strings.HasSuffix(k, "path") || strings.HasSuffix(k, "paths")
}

// relWithin returns p relative to root when p is root or lies under
// it lexically.
func relWithin(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// isWithinPath reports whether p is root or lies under it, comparing
// symlink-resolved paths (temp directories are often symlinks).
func isWithinPath(root, p string) bool {
	_, ok := relWithin(resolvePath(root), resolvePath(p))
	return ok
}

func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	return resolveAttemptPath(abs)
}

// boundaryBefore reports whether the byte before at (if any) ends a
// word, so that a match does not start in the middle of a path.
func boundaryBefore(s string, at int) bool {
	if at == 0 {
		return true
	}
	c := s[at-1]
	return c != '/' && !isPathByte(c)
}

// boundaryAfter reports whether the byte at at (if any) ends the path
// word. A slash continues the path, so it counts as a boundary.
func boundaryAfter(s string, at int) bool {
	if at == len(s) {
		return true
	}
	c := s[at]
	return c == '/' || !isPathByte(c)
}

func isPathByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("_.-+@%~", c) >= 0
}
