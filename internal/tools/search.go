package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Search budgets: summarized and bounded (Section 13.4).
const (
	maxGrepResults = 50
	maxGrepLines   = 20
	maxGlobResults = 200
)

var grepSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"pattern":     map[string]any{"type": "string", "minLength": 1},
		"paths":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"include":     map[string]any{"type": "string"},
		"literal":     map[string]any{"type": "boolean"},
		"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
	},
	"required":             []string{"pattern"},
	"additionalProperties": false,
})

var globSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"pattern":           map[string]any{"type": "string", "minLength": 1},
		"dir":               map[string]any{"type": "string"},
		"respect_gitignore": map[string]any{"type": "boolean"},
		"max_results":       map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
	},
	"required":             []string{"pattern"},
	"additionalProperties": false,
})

// GrepTool implements grep with summarized bounded output.
type GrepTool struct{}

func (t *GrepTool) Definition() Definition {
	return Definition{
		Name: "grep", Description: "Search file contents with regex, summarized and bounded",
		InputSchema: grepSchema, Effect: "read_only", Idempotency: "idempotent",
		Cancel: "immediate", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "files", Tags: []string{"search", "code"}, Keywords: []string{"grep", "search", "pattern", "regex"},
	}
}

func (t *GrepTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Pattern    string   `json:"pattern"`
		Paths      []string `json:"paths"`
		Include    string   `json:"include"`
		Literal    bool     `json:"literal"`
		MaxResults int      `json:"max_results"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	maxRes := a.MaxResults
	if maxRes <= 0 {
		maxRes = maxGrepResults
	}
	roots := a.Paths
	if len(roots) == 0 {
		roots = []string{"."}
	}
	var re *regexp.Regexp
	var err error
	if a.Literal {
		re, err = regexp.Compile(regexp.QuoteMeta(a.Pattern))
	} else {
		re, err = regexp.Compile(a.Pattern)
	}
	if err != nil {
		return Result{Status: StatusInputError, Output: fmt.Sprintf("bad pattern: %v", err)}, nil
	}
	type hit struct {
		file string
		line int
		text string
	}
	counts := map[string]int{}
	var hits []hit
	totalFiles := 0
	skippedBin := 0
	for _, root := range roots {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				base := filepath.Base(p)
				if base == ".git" || base == "node_modules" || base == "target" {
					return filepath.SkipDir
				}
				return nil
			}
			if a.Include != "" {
				ok, _ := filepath.Match(a.Include, filepath.Base(p))
				if !ok {
					return nil
				}
			}
			if isBinaryPath(p) {
				skippedBin++
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			totalFiles++
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			ln := 0
			for sc.Scan() {
				ln++
				line := sc.Text()
				if re.MatchString(line) {
					counts[p]++
					if len(hits) < maxRes {
						t := strings.TrimSpace(line)
						if len(t) > 240 {
							t = t[:240] + "…"
						}
						hits = append(hits, hit{file: p, line: ln, text: t})
					}
					if len(hits) >= maxRes && len(counts) > 0 {
						// Keep counting matches per file but stop
						// storing new lines beyond the budget.
					}
				}
				if ln > 20000 {
					break
				}
			}
			return nil
		})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) with matches, %d shown (scanned %d files", len(counts), len(hits), totalFiles)
	if skippedBin > 0 {
		fmt.Fprintf(&b, ", skipped %d binaries", skippedBin)
	}
	b.WriteString(")\n")
	files := make([]string, 0, len(counts))
	for f := range counts {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		fmt.Fprintf(&b, "%s: %d match(es)\n", f, counts[f])
	}
	shown := hits
	if len(shown) > maxGrepLines {
		shown = shown[:maxGrepLines]
	}
	for _, h := range shown {
		fmt.Fprintf(&b, "  %s:%d: %s\n", h.file, h.line, h.text)
	}
	if len(hits) >= maxRes {
		fmt.Fprintf(&b, "(bounded to %d results; refine the pattern)\n", maxRes)
	}
	return Result{Status: StatusOK, Output: b.String()}, nil
}

func (t *GrepTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func isBinaryPath(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	for _, c := range buf[:n] {
		if c == 0 {
			return true
		}
	}
	return false
}

// GlobTool implements glob respecting .gitignore.
type GlobTool struct{}

func (t *GlobTool) Definition() Definition {
	return Definition{
		Name: "glob", Description: "Find files by pattern respecting .gitignore",
		InputSchema: globSchema, Effect: "read_only", Idempotency: "idempotent",
		Cancel: "immediate", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "files", Tags: []string{"search", "file"}, Keywords: []string{"glob", "find", "files", "pattern"},
	}
}

func (t *GlobTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Pattern          string `json:"pattern"`
		Dir              string `json:"dir"`
		RespectGitignore *bool  `json:"respect_gitignore"`
		MaxResults       int    `json:"max_results"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	dir := a.Dir
	if dir == "" {
		dir = "."
	}
	respect := true
	if a.RespectGitignore != nil {
		respect = *a.RespectGitignore
	}
	maxRes := a.MaxResults
	if maxRes <= 0 {
		maxRes = maxGlobResults
	}
	ign := NewIgnoreMatcher(dir, respect)
	matches, truncated, err := globWalk(dir, a.Pattern, ign, maxRes)
	if err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d match(es) for %q in %s\n", len(matches), a.Pattern, dir)
	for _, m := range matches {
		b.WriteString(m + "\n")
	}
	if truncated {
		fmt.Fprintf(&b, "(bounded to %d; refine the pattern)\n", maxRes)
	}
	return Result{Status: StatusOK, Output: b.String()}, nil
}

func (t *GlobTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func globWalk(dir, pattern string, ign *IgnoreMatcher, maxRes int) ([]string, bool, error) {
	var out []string
	truncated := false
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(p)
			if base == ".git" || base == "node_modules" || base == "target" {
				return filepath.SkipDir
			}
			if ign.Ignored(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if ign.Ignored(rel, false) {
			return nil
		}
		ok, err := matchGlob(pattern, rel)
		if err != nil {
			return err
		}
		if ok {
			if len(out) >= maxRes {
				truncated = true
				return filepath.SkipAll
			}
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Strings(out)
	return out, truncated, nil
}

// matchGlob matches with ** support: split pattern and path into
// segments and match recursively.
func matchGlob(pattern, name string) (bool, error) {
	patSegs := strings.Split(filepath.ToSlash(pattern), "/")
	nameSegs := strings.Split(filepath.ToSlash(name), "/")
	return matchSegs(patSegs, nameSegs)
}

func matchSegs(pat, name []string) (bool, error) {
	if len(pat) == 0 {
		return len(name) == 0, nil
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			ok, err := matchSegs(pat[1:], name[i:])
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if len(name) == 0 {
		return false, nil
	}
	ok, err := filepath.Match(pat[0], name[0])
	if err != nil || !ok {
		return false, err
	}
	return matchSegs(pat[1:], name[1:])
}

// IgnoreMatcher applies .gitignore files from dir upward plus the
// built-in heavy-directory collapses.
type IgnoreMatcher struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	pat  string
	dir  bool
	neg  bool
	base string
}

func NewIgnoreMatcher(dir string, enabled bool) *IgnoreMatcher {
	m := &IgnoreMatcher{}
	if !enabled {
		return m
	}
	abs, _ := filepath.Abs(dir)
	// Collect .gitignore from abs upward to the filesystem root.
	var files []string
	cur := abs
	for {
		cand := filepath.Join(cur, ".gitignore")
		if _, err := os.Stat(cand); err == nil {
			files = append(files, cand)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	// Apply deepest last so nearer files win; negation handled in order.
	for i := len(files) - 1; i >= 0; i-- {
		m.addFile(files[i])
	}
	return m
}

func (m *IgnoreMatcher) addFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	base := filepath.Dir(path)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := false
		if strings.HasPrefix(line, "!") {
			neg = true
			line = line[1:]
		}
		isDir := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		line = strings.TrimPrefix(line, "/")
		m.patterns = append(m.patterns, ignorePattern{pat: line, dir: isDir, neg: neg, base: base})
	}
}

// Ignored reports whether rel (slash-separated, relative to the walk
// root) is ignored.
func (m *IgnoreMatcher) Ignored(rel string, isDir bool) bool {
	ignored := false
	relSlash := filepath.ToSlash(rel)
	for _, p := range m.patterns {
		match := false
		if strings.Contains(p.pat, "/") {
			ok, _ := matchGlob(p.pat, relSlash)
			match = ok
		} else {
			for _, seg := range strings.Split(relSlash, "/") {
				ok, _ := filepath.Match(p.pat, seg)
				if ok {
					match = true
					break
				}
			}
		}
		if match {
			if p.dir && !isDir {
				// dir-only pattern still ignores paths beneath it.
				match = true
			}
			if p.neg {
				ignored = false
			} else {
				ignored = true
			}
		}
	}
	return ignored
}
