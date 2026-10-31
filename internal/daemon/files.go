package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// Heavy directories always collapse in listings (Section 3.5): they
// are never enumerated inline.
var heavyDirs = map[string]bool{
	"node_modules": true, ".git": true, "target": true,
	"dist": true, "build": true, ".excalibur": true,
	"__pycache__": true, ".venv": true, "vendor": true,
}

// ListProjectFiles serves files.list: a lazy paginated directory
// listing scoped to one registered project. It honors the project
// .gitignore (shared with the watcher through workspace.LoadIgnore),
// collapses heavy directories, and filters to agent-touched files
// from the edit history when requested (Sections 3.5, 13.7).
//
// Cursor is a decimal entry offset; the result carries entries and
// next_cursor (empty at the end).
func ListProjectFiles(reg *workspace.Registry, history *workspace.History, p api.FilesListParams) (map[string]any, error) {
	if reg == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "no project registry"}
	}
	proj, err := reg.Get(types.ProjectID(p.ProjectID))
	if err != nil {
		return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
	}
	if len(proj.Roots) == 0 {
		return map[string]any{"entries": []string{}, "next_cursor": ""}, nil
	}
	root := proj.Roots[0]
	rel := filepath.Clean(p.Path)
	if rel == "." || rel == "" {
		rel = ""
	}
	dir := filepath.Join(root, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
	}
	ignore := workspace.LoadIgnore(root)
	var touched map[string]bool
	if p.AgentTouched && history != nil {
		touched = map[string]bool{}
		for _, tp := range history.Paths() {
			if rp, rerr := filepath.Rel(root, tp); rerr == nil && !strings.HasPrefix(rp, "..") {
				touched[filepath.Clean(rp)] = true
			}
		}
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".well-known" {
			continue
		}
		relName := filepath.Join(rel, name)
		if e.IsDir() && heavyDirs[name] {
			names = append(names, relName+"/…")
			continue
		}
		if ignore.Excluded(filepath.ToSlash(relName), e.IsDir()) {
			continue
		}
		if touched != nil && !touched[filepath.Clean(relName)] {
			continue
		}
		if e.IsDir() {
			names = append(names, relName+"/")
		} else {
			names = append(names, relName)
		}
	}
	sort.Strings(names)
	offset := 0
	if p.Cursor != "" {
		if n, cerr := strconv.Atoi(p.Cursor); cerr == nil && n >= 0 && n <= len(names) {
			offset = n
		}
	}
	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	end := offset + limit
	if end > len(names) {
		end = len(names)
	}
	page := names[offset:end]
	next := ""
	if end < len(names) {
		next = strconv.Itoa(end)
	}
	return map[string]any{"entries": page, "next_cursor": next}, nil
}
