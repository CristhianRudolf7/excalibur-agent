package repoexplore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Brief is one deterministic exploration pass. Lines holds the
// rendered brief (at most Config.MaxBriefLines lines, each a single
// line without embedded newlines); Truncated reports budget capping.
type Brief struct {
	Workdir   string   `json:"workdir"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
	Empty     bool     `json:"empty"`
}

// Render joins the brief lines for pinning into L3 working notes as a
// single note: one note slot carries the whole ~60-line brief, so the
// WorkingNotes cap (12 notes) keeps it intact instead of truncating
// it to its tail. The output carries no timestamps so every snapshot
// of the run renders identical bytes (Section 11.1 rule 1).
func (b Brief) Render() string {
	if len(b.Lines) == 0 {
		return ""
	}
	out := strings.Join(b.Lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// Collector gathers one bounded read-only pass.
type Collector struct {
	cfg Config
}

// NewCollector returns a collector with normalized config.
func NewCollector(cfg Config) *Collector {
	return &Collector{cfg: cfg.Normalize()}
}

// Collect runs the bounded inspection. It never returns an error:
// every probe degrades to absence, and the parent context bounds the
// whole pass to cfg.Timeout.
func (c *Collector) Collect(ctx context.Context, workdir, instruction string) Brief {
	cfg := c.cfg
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return Brief{Empty: true, Lines: []string{
			"Repo brief (read-only explorer): no project directory configured.",
			"Work in the current directory and verify paths with tools before relying on them.",
		}}
	}
	fi, err := os.Stat(workdir)
	if err != nil || !fi.IsDir() {
		return Brief{Workdir: workdir, Empty: true, Lines: []string{
			"Repo brief (read-only explorer): workdir " + workdir + " is not a readable directory.",
			"Verify the working directory with tools before acting.",
		}}
	}
	s := &scanner{cfg: cfg, ctx: ctx, workdir: workdir, filesSeen: 0, bytesRead: 0}
	layout := s.topLayout()
	builds := s.buildFiles()
	entries := s.entryPoints()
	tests, testHow := s.testFiles()
	configs := s.configFiles()
	data := s.dataFiles()
	versions := s.versionPins()
	mentioned := s.mentionedPaths(instruction)
	pitfalls := s.pitfalls(layout, builds, entries, tests, data)

	var lines []string
	add := func(l string) {
		l = singleLine(l, 160)
		if l != "" {
			lines = append(lines, l)
		}
	}
	base := filepath.Base(workdir)
	if base == "" || base == "." || base == "/" {
		base = workdir
	}
	add("Repo brief (read-only explorer, " + workdir + "): verify with tools before relying on it.")
	if len(layout) > 0 {
		add("layout (" + base + "):")
		for _, l := range layout {
			add("  " + l)
		}
	} else {
		add("layout: empty or unreadable directory.")
	}
	if len(builds) > 0 {
		add("build:")
		for _, l := range builds {
			add("  " + l)
		}
	} else {
		add("build: no recognized build file at top level.")
	}
	if len(entries) > 0 {
		add("entry points:")
		for _, l := range entries {
			add("  " + l)
		}
	} else {
		add("entry points: none recognized; check layout above.")
	}
	if len(tests) > 0 {
		add("tests:")
		for _, l := range tests {
			add("  " + l)
		}
		if testHow != "" {
			add("  run: " + testHow)
		}
	} else {
		add("tests: no test files detected.")
	}
	if len(configs) > 0 {
		add("configs:")
		for _, l := range configs {
			add("  " + l)
		}
	}
	if len(data) > 0 {
		add("data:")
		for _, l := range data {
			add("  " + l)
		}
	}
	if len(versions) > 0 {
		add("versions (from files, not probes):")
		for _, l := range versions {
			add("  " + l)
		}
	}
	if len(mentioned) > 0 {
		add("instruction paths:")
		for _, l := range mentioned {
			add("  " + l)
		}
	}
	if len(pitfalls) > 0 {
		add("pitfalls:")
		for _, l := range pitfalls {
			add("  " + l)
		}
	}
	add("Verify paths and commands with tools; do not trust this summary blindly.")

	truncated := s.truncated
	if len(lines) > cfg.MaxBriefLines && cfg.MaxBriefLines > 0 {
		lines = lines[:cfg.MaxBriefLines]
		// Keep the verify footer as the last line so the brief
		// never ends mid-list after capping.
		lines[len(lines)-1] = "Verify paths and commands with tools; do not trust this summary blindly."
		truncated = true
	}
	return Brief{Workdir: workdir, Lines: lines, Truncated: truncated}
}

// scanner carries budgets across probes.
type scanner struct {
	cfg       Config
	ctx       context.Context
	workdir   string
	filesSeen int
	bytesRead int
	truncated bool
}

// heavyDirs collapse to entry counts instead of traversal.
var heavyDirs = map[string]bool{
	"node_modules": true, ".git": true, "target": true,
	"__pycache__": true, ".venv": true, "venv": true,
	"dist": true, "build": true, ".next": true,
}

// budget reports whether another file visit fits the budget.
func (s *scanner) budget(n int) bool {
	if s.ctx.Err() != nil {
		s.truncated = true
		return false
	}
	if s.filesSeen+n > s.cfg.MaxFiles {
		s.truncated = true
		return false
	}
	return true
}

// topLayout lists the workdir top level, sorted, capped at 24
// entries with one-line per entry. Heavy directories show their
// entry count instead of being traversed.
func (s *scanner) topLayout() []string {
	ents, err := os.ReadDir(s.workdir)
	if err != nil {
		return nil
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	var out []string
	shown := 0
	for _, e := range ents {
		if s.ctx.Err() != nil {
			s.truncated = true
			break
		}
		if shown >= 24 {
			out = append(out, "... ("+itoa(len(ents)-shown)+" more entries)")
			s.truncated = len(ents) > shown
			break
		}
		name := e.Name()
		if e.IsDir() {
			if heavyDirs[name] {
				n := countEntries(filepath.Join(s.workdir, name))
				out = append(out, name+"/ (dir, "+itoa(n)+" entries, collapsed)")
			} else {
				out = append(out, name+"/ (dir)")
			}
		} else {
			sz := fileSize(filepath.Join(s.workdir, name))
			out = append(out, name+" ("+humanBytes(sz)+")")
		}
		shown++
	}
	return out
}

func countEntries(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(ents)
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// buildFiles detects top-level build files and renders one how-to
// line each, in fixed order for stability.
func (s *scanner) buildFiles() []string {
	var out []string
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(s.workdir, name))
		return err == nil
	}
	if has("go.mod") {
		mod, ver := goModInfo(filepath.Join(s.workdir, "go.mod"), s.cfg.MaxFileReadBytes)
		line := "go.mod present"
		if mod != "" {
			line += " module " + mod
		}
		if ver != "" {
			line += " (go " + ver + ")"
		}
		line += "; build: go build ./...; test: go test ./..."
		out = append(out, line)
	}
	if has("Cargo.toml") {
		out = append(out, "Cargo.toml present; build: cargo build; test: cargo test")
	}
	if has("package.json") {
		scripts := packageScripts(filepath.Join(s.workdir, "package.json"), s.cfg.MaxFileReadBytes)
		line := "package.json present; install: npm install"
		if len(scripts) > 0 {
			line += "; scripts: " + strings.Join(scripts, ", ")
		}
		if containsStr(scripts, "test") {
			line += "; test: npm test"
		}
		out = append(out, line)
	}
	if has("pyproject.toml") || has("setup.py") || has("setup.cfg") {
		out = append(out, "python packaging present; install: pip install -e . (or requirements); test: pytest")
	} else if has("requirements.txt") || has("requirements-dev.txt") {
		out = append(out, "requirements.txt present; install: pip install -r requirements.txt; test: pytest")
	}
	if has("Makefile") {
		targets := makeTargets(filepath.Join(s.workdir, "Makefile"), s.cfg.MaxFileReadBytes)
		line := "Makefile present"
		if len(targets) > 0 {
			line += " targets: " + strings.Join(targets, ", ")
		}
		out = append(out, line)
	}
	if has("CMakeLists.txt") {
		out = append(out, "CMakeLists.txt present; configure: cmake -S . -B build; build: cmake --build build")
	}
	if has("build.gradle") || has("build.gradle.kts") {
		out = append(out, "gradle present; build: gradle build; test: gradle test")
	}
	if has("pom.xml") {
		out = append(out, "pom.xml present; build: mvn package; test: mvn test")
	}
	if has("go.work") {
		out = append(out, "go.work present (multi-module workspace)")
	}
	return out
}

// entryPoints lists likely program entry points, sorted, capped.
func (s *scanner) entryPoints() []string {
	patterns := []string{
		"main.go", "cmd/*/main.go", "main.py", "app.py", "wsgi.py",
		"src/main.py", "src/index.js", "src/index.ts", "src/main.js",
		"src/main.ts", "index.js", "index.ts", "app.js", "main.js",
		"src/Main.java", "src/main.rs", "src/lib.rs",
	}
	var out []string
	for _, pat := range patterns {
		if !s.budget(1) {
			break
		}
		matches, _ := filepath.Glob(filepath.Join(s.workdir, pat))
		s.filesSeen++
		for _, m := range matches {
			rel, _ := filepath.Rel(s.workdir, m)
			out = append(out, rel)
			if len(out) >= 8 {
				break
			}
		}
		if len(out) >= 8 {
			break
		}
	}
	// Bounded shallow walk for cmd/*/main.go already covered by
	// glob; add Dockerfile CMD hint only when no entry found.
	if len(out) == 0 {
		if h := dockerHint(filepath.Join(s.workdir, "Dockerfile"), s.cfg.MaxFileReadBytes); h != "" {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	if len(out) > 8 {
		out = out[:8]
		s.truncated = true
	}
	return out
}

// testFiles lists test files and one run command, capped.
func (s *scanner) testFiles() ([]string, string) {
	var out []string
	walkFiles(s, 2, func(rel string) bool {
		base := filepath.Base(rel)
		if strings.HasSuffix(base, "_test.go") ||
			strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") ||
			strings.HasSuffix(base, "_test.py") ||
			strings.HasSuffix(base, ".spec.js") || strings.HasSuffix(base, ".spec.ts") ||
			strings.HasSuffix(base, ".test.js") || strings.HasSuffix(base, ".test.ts") {
			return true
		}
		if strings.HasPrefix(rel, "tests/") || strings.HasPrefix(rel, "test/") || strings.HasPrefix(rel, "__tests__/") {
			return true
		}
		return false
	}, &out, 8)
	sort.Strings(out)
	how := ""
	hasGo, hasPy, hasJS, hasRust := false, false, false, false
	for _, f := range out {
		switch {
		case strings.HasSuffix(f, ".go"):
			hasGo = true
		case strings.HasSuffix(f, ".py"):
			hasPy = true
		case strings.HasSuffix(f, ".js") || strings.HasSuffix(f, ".ts"):
			hasJS = true
		case strings.HasSuffix(f, ".rs"):
			hasRust = true
		}
	}
	switch {
	case hasGo:
		how = "go test ./..."
	case hasRust:
		how = "cargo test"
	case hasPy:
		how = "pytest"
	case hasJS:
		how = "npm test"
	}
	// Top-level tests dir without recognized extensions still hints.
	if how == "" && len(out) > 0 {
		how = "run the project test command (see build above)"
	}
	return out, how
}

// configFiles lists deployment and config files, capped.
func (s *scanner) configFiles() []string {
	cands := []string{
		"Dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"compose.yml", ".env.example", ".nvmrc", ".python-version",
		"rust-toolchain.toml", "rust-toolchain", ".tool-versions",
	}
	var out []string
	for _, c := range cands {
		if _, err := os.Stat(filepath.Join(s.workdir, c)); err == nil {
			out = append(out, c)
		}
	}
	// Top-level yaml/toml/ini/json configs, sorted, capped at 4
	// beyond the curated list above.
	ents, err := os.ReadDir(s.workdir)
	if err == nil {
		var extra []string
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") ||
				strings.HasSuffix(n, ".toml") || strings.HasSuffix(n, ".ini") ||
				n == ".env" {
				already := false
				for _, o := range out {
					if o == n {
						already = true
						break
					}
				}
				if !already {
					extra = append(extra, n)
				}
			}
		}
		sort.Strings(extra)
		for _, n := range extra {
			if len(out) >= 10 {
				break
			}
			out = append(out, n)
		}
	}
	sort.Strings(out)
	if len(out) > 10 {
		out = out[:10]
		s.truncated = true
	}
	return out
}

// dataFiles lists data files with sizes and format hints, capped.
// Small text heads are read (bounded) for CSV headers only.
func (s *scanner) dataFiles() []string {
	var found []string
	walkFiles(s, 3, func(rel string) bool {
		lower := strings.ToLower(rel)
		for _, ext := range []string{".csv", ".json", ".jsonl", ".tsv", ".txt", ".md", ".sqlite", ".db", ".parquet", ".xml", ".yaml", ".yml"} {
			if strings.HasSuffix(lower, ext) {
				// Skip configs already covered when at top level
				// with small size? Keep data listing focused:
				// include everything; configs section overlaps
				// slightly but that is stable and explicit.
				return true
			}
		}
		return false
	}, &found, 24)
	sort.Strings(found)
	var out []string
	for _, rel := range found {
		if len(out) >= 8 {
			s.truncated = true
			break
		}
		full := filepath.Join(s.workdir, rel)
		sz := fileSize(full)
		hint := dataHint(s, full, rel, sz)
		out = append(out, rel+" ("+humanBytes(sz)+hint+")")
	}
	return out
}

func dataHint(s *scanner, full, rel string, sz int64) string {
	lower := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(lower, ".csv"):
		head := readHead(s, full, 512)
		if i := strings.Index(head, "\n"); i >= 0 {
			head = head[:i]
		}
		head = strings.TrimSpace(head)
		if head != "" {
			if len(head) > 80 {
				head = head[:79] + "…"
			}
			return "; header: " + head
		}
		return "; CSV"
	case strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".jsonl"):
		return "; JSON"
	case strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".db"):
		return "; SQLite; inspect with sqlite3 .tables"
	case strings.HasSuffix(lower, ".parquet"):
		return "; Parquet"
	case strings.HasSuffix(lower, ".md"):
		return "; Markdown"
	case strings.HasSuffix(lower, ".txt"):
		return "; text"
	default:
		return ""
	}
}

// versionPins reads version pins from files without executing
// anything (no --version probes; envsnap owns those).
func (s *scanner) versionPins() []string {
	var out []string
	if mod, ver := goModInfo(filepath.Join(s.workdir, "go.mod"), s.cfg.MaxFileReadBytes); ver != "" {
		_ = mod
		out = append(out, "go "+ver+" (go.mod)")
	}
	if v := readFirstLine(s, filepath.Join(s.workdir, ".nvmrc")); v != "" {
		out = append(out, "node "+v+" (.nvmrc)")
	}
	if v := readFirstLine(s, filepath.Join(s.workdir, ".python-version")); v != "" {
		out = append(out, "python "+v+" (.python-version)")
	}
	if v := readFirstLine(s, filepath.Join(s.workdir, "rust-toolchain.toml")); v != "" {
		out = append(out, "rust toolchain pinned (rust-toolchain.toml)")
	} else if v := readFirstLine(s, filepath.Join(s.workdir, "rust-toolchain")); v != "" {
		out = append(out, "rust "+v+" (rust-toolchain)")
	}
	if eng := packageEngines(filepath.Join(s.workdir, "package.json"), s.cfg.MaxFileReadBytes); eng != "" {
		out = append(out, "engines: "+eng+" (package.json)")
	}
	sort.Strings(out)
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

var pathToken = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_\-./]{1,80}\.[A-Za-z0-9]{1,8}|/[A-Za-z0-9_\-./]{2,80}`)

// mentionedPaths resolves path-like tokens from the instruction
// against the workdir: exists (with kind/size) or not found.
func (s *scanner) mentionedPaths(instruction string) []string {
	if strings.TrimSpace(instruction) == "" {
		return nil
	}
	seen := map[string]bool{}
	var toks []string
	for _, m := range pathToken.FindAllString(instruction, -1) {
		t := strings.Trim(m, ".,;:!?()[]{}\"'`")
		if t == "" || seen[t] {
			continue
		}
		// Skip bare version-like tokens and URLs.
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			continue
		}
		seen[t] = true
		toks = append(toks, t)
		if len(toks) >= 10 {
			break
		}
	}
	var out []string
	for _, t := range toks {
		clean := filepath.Clean(t)
		if filepath.IsAbs(clean) {
			// Only resolve absolute paths inside the workdir.
			rel, err := filepath.Rel(s.workdir, clean)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			clean = rel
		}
		// Drop tokens that are clearly not paths.
		if !strings.Contains(clean, "/") && !strings.Contains(clean, ".") {
			continue
		}
		full := filepath.Join(s.workdir, clean)
		fi, err := os.Stat(full)
		if err != nil {
			out = append(out, t+": not found")
		} else if fi.IsDir() {
			out = append(out, t+": exists (dir)")
		} else {
			out = append(out, t+": exists ("+humanBytes(fi.Size())+")")
		}
		if len(out) >= 5 {
			break
		}
	}
	return out
}

// pitfalls derives likely gotchas deterministically.
func (s *scanner) pitfalls(layout, builds, entries, tests, data []string) []string {
	var out []string
	if len(builds) == 0 {
		out = append(out, "no build file recognized; confirm build steps in README/docs before running anything.")
	}
	if len(tests) == 0 {
		out = append(out, "no tests detected; verify manually by executing the result.")
	}
	hasReadme := false
	for _, l := range layout {
		if strings.HasPrefix(l, "README") {
			hasReadme = true
			break
		}
	}
	if !hasReadme {
		out = append(out, "no README at top level; confirm the entry point with tools before acting.")
	}
	for _, l := range layout {
		if strings.HasSuffix(l, "(dir,") || strings.Contains(l, "collapsed)") {
			name := strings.SplitN(l, "/", 2)[0]
			out = append(out, name+"/ is large/collapsed; list it selectively instead of walking it.")
			if len(out) >= 5 {
				break
			}
		}
	}
	for _, d := range data {
		// Flag large data files by their rendered size suffix.
		if strings.Contains(d, "G)") || strings.Contains(d, "00M)") {
			name := strings.SplitN(d, " ", 2)[0]
			out = append(out, name+" is large; sample its head before a full read.")
			break
		}
	}
	if len(entries) == 0 && len(builds) > 0 {
		out = append(out, "entry point unclear; check the build file and README for the real command.")
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// walkFiles visits files below workdir up to depth, calling keep on
// each relative path and appending matches to out (capped at cap).
func walkFiles(s *scanner, depth int, keep func(rel string) bool, out *[]string, cap int) {
	if s.cfg.MaxDepth < depth {
		depth = s.cfg.MaxDepth
	}
	var rec func(dir, rel string, d int)
	rec = func(dir, rel string, d int) {
		if s.ctx.Err() != nil {
			s.truncated = true
			return
		}
		if len(*out) >= cap || !s.budget(1) {
			return
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		s.filesSeen++
		sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
		for _, e := range ents {
			if len(*out) >= cap || !s.budget(1) {
				return
			}
			name := e.Name()
			relPath := name
			if rel != "" {
				relPath = rel + "/" + name
			}
			// Never descend into heavy or hidden VCS dirs
			// except as collapsed counts in the layout.
			if e.IsDir() {
				if heavyDirs[name] || name == ".git" || strings.HasPrefix(name, ".") && d > 0 {
					continue
				}
				if d >= depth {
					continue
				}
				rec(filepath.Join(dir, name), relPath, d+1)
			} else {
				if keep(relPath) {
					*out = append(*out, relPath)
				}
			}
		}
	}
	rec(s.workdir, "", 0)
}

func goModInfo(path string, maxBytes int) (mod, ver string) {
	raw := readFileBounded(path, maxBytes)
	if raw == "" {
		return "", ""
	}
	for _, ln := range strings.Split(raw, "\n") {
		f := strings.TrimSpace(ln)
		if strings.HasPrefix(f, "module ") {
			mod = strings.TrimSpace(strings.TrimPrefix(f, "module "))
		}
		if strings.HasPrefix(f, "go ") {
			ver = strings.TrimSpace(strings.TrimPrefix(f, "go "))
		}
		if mod != "" && ver != "" {
			break
		}
	}
	return mod, ver
}

func packageScripts(path string, maxBytes int) []string {
	raw := readFileBounded(path, maxBytes)
	if raw == "" {
		return nil
	}
	var body struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return nil
	}
	var names []string
	for n := range body.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = names[:5]
	}
	return names
}

func packageEngines(path string, maxBytes int) string {
	raw := readFileBounded(path, maxBytes)
	if raw == "" {
		return ""
	}
	var body struct {
		Engines map[string]string `json:"engines"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return ""
	}
	var parts []string
	for k, v := range body.Engines {
		parts = append(parts, k+" "+v)
	}
	sort.Strings(parts)
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, ", ")
}

func makeTargets(path string, maxBytes int) []string {
	raw := readFileBounded(path, maxBytes)
	if raw == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimRight(ln, " \t\r")
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "\t") || strings.HasPrefix(ln, " ") {
			continue
		}
		if i := strings.Index(ln, ":"); i > 0 {
			name := strings.TrimSpace(ln[:i])
			if name == "" || strings.ContainsAny(name, "=%?$") || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
			if len(out) >= 5 {
				break
			}
		}
	}
	return out
}

func dockerHint(path string, maxBytes int) string {
	raw := readFileBounded(path, maxBytes)
	if raw == "" {
		return ""
	}
	for _, ln := range strings.Split(raw, "\n") {
		f := strings.TrimSpace(ln)
		if strings.HasPrefix(f, "CMD ") || strings.HasPrefix(f, "ENTRYPOINT ") {
			if len(f) > 100 {
				f = f[:99] + "…"
			}
			return "Dockerfile: " + f
		}
	}
	return ""
}

func readHead(s *scanner, path string, n int) string {
	if s.bytesRead+n > 200*1024 {
		s.truncated = true
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, n)
	r, err := f.Read(buf)
	if r > 0 {
		s.bytesRead += r
		return string(buf[:r])
	}
	_ = err
	return ""
}

func readFirstLine(s *scanner, path string) string {
	raw := readFileBounded(path, 1024)
	if raw == "" {
		return ""
	}
	if i := strings.Index(raw, "\n"); i >= 0 {
		raw = raw[:i]
	}
	v := strings.TrimSpace(raw)
	if s != nil {
		s.bytesRead += len(v)
	}
	return singleLine(v, 60)
}

func readFileBounded(path string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return ""
	}
	if fi.Size() > int64(maxBytes)*4 {
		// Still read only the head; huge files never load fully.
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, _ := f.Read(buf)
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

func singleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return itoa(int(n/(1<<30))) + "G"
	case n >= 1<<20:
		return itoa(int(n/(1<<20))) + "M"
	case n >= 1<<10:
		return itoa(int(n/(1<<10))) + "K"
	default:
		return itoa(int(n)) + "B"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
