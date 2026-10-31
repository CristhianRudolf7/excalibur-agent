package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ignore is the compiled .gitignore subset for one project root,
// shared with daemon-side surfaces (files.list) so listings and the
// watcher honor the same rules. It is intentionally dependency-free:
// blank lines and # comments skipped, trailing / marks directories,
// * and ** supported, ! negates, later rules win.
type Ignore struct {
	m *ignoreMatcher
}

// LoadIgnore reads root/.gitignore (missing file means no rules) and
// returns the compiled matcher.
func LoadIgnore(root string) *Ignore {
	m := &ignoreMatcher{}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return &Ignore{m: m}
	}
	for _, line := range strings.Split(string(data), "\n") {
		m.add(line)
	}
	return &Ignore{m: m}
}

// Excluded reports whether rel (slash or OS path, relative to the
// root) is ignored.
func (g *Ignore) Excluded(rel string, isDir bool) bool {
	if g == nil || g.m == nil {
		return false
	}
	return g.m.excluded(rel, isDir)
}

// Paths lists every path with at least one recorded version, oldest
// touch first. It backs the files.list agent_touched filter.
func (h *History) Paths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, v := range h.vers {
		if !seen[v.Path] {
			seen[v.Path] = true
			out = append(out, v.Path)
		}
	}
	sort.Strings(out)
	return out
}
