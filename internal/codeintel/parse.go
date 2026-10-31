package codeintel

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"strings"
)

// ParsedSymbol is one definition found in a source file. Calls holds
// bare callee names used in the symbol body (over-approximated by the
// fallback parsers, precise call sites by tree-sitter); Refs holds
// bare names used outside call position, such as type names.
type ParsedSymbol struct {
	Name    string
	SubKind string
	Line    int
	EndLine int
	Calls   []string
	Refs    []string
}

// ParsedSection is one headed section of a document with the code
// names it mentions (backtick spans shaped like identifiers).
type ParsedSection struct {
	Heading  string
	Slug     string
	Line     int
	EndLine  int
	Mentions []string
}

// ParsedFile is the parser output for one file.
type ParsedFile struct {
	Rel      string
	Lang     string
	Imports  []string
	Symbols  []ParsedSymbol
	Sections []ParsedSection
	IsTest   bool
}

// Parser extracts the graph contribution of one file.
type Parser interface {
	// Parse reads src (never touches disk) and describes rel.
	Parse(rel string, src []byte) (*ParsedFile, error)
	// Langs lists the language keys this parser handles.
	Langs() []string
}

// extensionLangs maps file extensions to language keys. The set
// matches the tree-sitter grammars bundled on cgo builds; the
// fallback parser covers the same keys with deterministic heuristics.
var extensionLangs = map[string]string{
	".go":       "go",
	".py":       "python",
	".js":       "javascript",
	".jsx":      "javascript",
	".mjs":      "javascript",
	".cjs":      "javascript",
	".ts":       "typescript",
	".tsx":      "typescript",
	".mts":      "typescript",
	".cts":      "typescript",
	".java":     "java",
	".c":        "c",
	".h":        "c",
	".cpp":      "cpp",
	".hpp":      "cpp",
	".cc":       "cpp",
	".hh":       "cpp",
	".rs":       "rust",
	".rb":       "ruby",
	".php":      "php",
	".md":       "doc",
	".markdown": "doc",
	".txt":      "doc",
	".rst":      "doc",
}

// docExtensions marks document languages parsed for sections.
var docExtensions = map[string]bool{
	"doc": true,
}

// DetectLanguage returns the language key for a project-relative path,
// or "" when the file is out of scope for the graph.
func DetectLanguage(rel string) (string, bool) {
	lang, ok := extensionLangs[strings.ToLower(path.Ext(rel))]
	return lang, ok
}

// IsDocLang reports document languages (sections, not symbols).
func IsDocLang(lang string) bool { return docExtensions[lang] }

// testPathRules mark test files by path convention, per language.
var testPathRules = []struct {
	lang  string
	match func(rel, base string) bool
}{
	{"go", func(rel, base string) bool { return strings.HasSuffix(base, "_test.go") }},
	{"python", func(rel, base string) bool {
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") ||
			strings.Contains("/"+rel, "/tests/") || strings.Contains("/"+rel, "/test/")
	}},
	{"javascript", func(rel, base string) bool {
		return strings.HasSuffix(base, ".test.js") || strings.HasSuffix(base, ".spec.js") ||
			strings.HasSuffix(base, ".test.jsx") || strings.HasSuffix(base, ".spec.jsx") ||
			strings.Contains("/"+rel, "/__tests__/")
	}},
	{"typescript", func(rel, base string) bool {
		return strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".spec.ts") ||
			strings.HasSuffix(base, ".test.tsx") || strings.HasSuffix(base, ".spec.tsx") ||
			strings.Contains("/"+rel, "/__tests__/")
	}},
	{"java", func(rel, base string) bool {
		return strings.HasPrefix(base, "Test") || strings.HasSuffix(base, "Test.java") ||
			strings.HasSuffix(base, "Tests.java")
	}},
	{"rust", func(rel, base string) bool { return strings.Contains("/"+rel, "/tests/") }},
	{"ruby", func(rel, base string) bool {
		return strings.HasSuffix(base, "_test.rb") || strings.HasSuffix(base, "_spec.rb")
	}},
	{"c", func(rel, base string) bool { return strings.Contains("/"+rel, "/test") }},
	{"cpp", func(rel, base string) bool { return strings.Contains("/"+rel, "/test") }},
	{"php", func(rel, base string) bool { return strings.HasSuffix(base, "Test.php") }},
}

// IsTestPath reports whether rel is a test file by path convention.
func IsTestPath(rel, lang string) bool {
	base := path.Base(rel)
	for _, r := range testPathRules {
		if r.lang == lang && r.match(rel, base) {
			return true
		}
	}
	return false
}

// testNameRules mark test symbols by name convention.
func isTestSymbol(name, lang string) bool {
	switch lang {
	case "go":
		return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Example") ||
			strings.HasPrefix(name, "Fuzz")
	case "python", "ruby":
		return strings.HasPrefix(name, "test_")
	case "javascript", "typescript":
		l := strings.ToLower(name)
		return strings.HasPrefix(l, "test") || strings.HasSuffix(l, "test")
	case "java", "rust", "c", "cpp", "php":
		l := strings.ToLower(name)
		return strings.HasPrefix(l, "test")
	default:
		return false
	}
}

// PreferredParser returns the tree-sitter parser when the build has
// cgo and a grammar for lang, else the deterministic fallback.
func PreferredParser(lang string) Parser {
	if p := treeSitterParserFor(lang); p != nil {
		return p
	}
	return fallbackParserFor(lang)
}

// ParseFile parses one file with the preferred parser for its
// language and returns the graph-ready update. Unknown extensions
// yield a file node only (still tracked for incremental hashing).
func ParseFile(rel string, src []byte) (FileUpdate, error) {
	sum := sha256.Sum256(src)
	hash := hex.EncodeToString(sum[:])
	upd := FileUpdate{Rel: rel, Hash: hash}
	lang, ok := DetectLanguage(rel)
	if !ok {
		upd.Nodes = []Node{{ID: FileID(rel), Kind: NodeFile, Path: rel, Hash: hash}}
		return upd, nil
	}
	upd.Lang = lang
	pf, err := PreferredParser(lang).Parse(rel, src)
	if err != nil {
		return upd, err
	}
	return BuildUpdate(pf, hash), nil
}

// BuildUpdate converts parser output into graph nodes, static edges
// and name uses. Cross-file call targets are never pinned here:
// the graph resolves dynamic edges (calls, references, tests, cites)
// live from Uses against the whole project name index, so indexing
// order never matters and incremental updates cannot leave stale
// cross-file links.
func BuildUpdate(pf *ParsedFile, hash string) FileUpdate {
	upd := FileUpdate{Rel: pf.Rel, Lang: pf.Lang, Hash: hash}
	fid := FileID(pf.Rel)
	upd.Nodes = append(upd.Nodes, Node{ID: fid, Kind: NodeFile, Path: pf.Rel, Hash: hash})
	dir := path.Dir(pf.Rel)
	if dir == "." {
		dir = ""
	}
	mod := ModuleID("dir:" + pf.Lang + ":" + dir)
	upd.Nodes = append(upd.Nodes, Node{ID: mod, Kind: NodeModule, Path: dir, Name: dir})

	for _, s := range pf.Symbols {
		sub := s.SubKind
		if sub == "" {
			sub = SymbolOther
		}
		if pf.IsTest || isTestSymbol(s.Name, pf.Lang) {
			sub = SymbolTest
		}
		end := s.EndLine
		if end < s.Line {
			end = s.Line
		}
		id := SymbolID(pf.Rel, s.Name, s.Line)
		upd.Nodes = append(upd.Nodes, Node{
			ID: id, Kind: NodeSymbol, SubKind: sub,
			Path: pf.Rel, Name: s.Name, Line: s.Line, EndLine: end,
		})
		upd.Edges = append(upd.Edges, Edge{Src: fid, Kind: EdgeDefines, Dst: id})
		for _, c := range dedupeNames(s.Calls) {
			if c != s.Name {
				upd.Uses = append(upd.Uses, NameUse{Holder: id, Kind: UseCalls, Name: c})
			}
		}
		for _, r := range dedupeNames(s.Refs) {
			if r != s.Name {
				upd.Uses = append(upd.Uses, NameUse{Holder: id, Kind: UseReferences, Name: r})
			}
		}
	}
	seenMod := map[string]bool{}
	for _, imp := range pf.Imports {
		imp = strings.TrimSpace(imp)
		if imp == "" {
			continue
		}
		for _, dst := range ResolveImports(pf.Rel, pf.Lang, imp) {
			if seenMod[dst] {
				continue
			}
			seenMod[dst] = true
			upd.Nodes = append(upd.Nodes, Node{ID: dst, Kind: NodeModule, Name: imp})
			upd.Edges = append(upd.Edges, Edge{Src: fid, Kind: EdgeImports, Dst: dst})
		}
	}

	for _, sec := range pf.Sections {
		sid := SectionID(pf.Rel, sec.Slug, sec.Line)
		end := sec.EndLine
		if end < sec.Line {
			end = sec.Line
		}
		upd.Nodes = append(upd.Nodes, Node{
			ID: sid, Kind: NodeSection, Path: pf.Rel,
			Name: sec.Heading, Line: sec.Line, EndLine: end,
		})
		upd.Edges = append(upd.Edges, Edge{Src: FileID(pf.Rel), Kind: EdgeDefines, Dst: sid})
		for _, m := range dedupeNames(sec.Mentions) {
			upd.Uses = append(upd.Uses, NameUse{Holder: sid, Kind: UseCites, Name: m})
		}
	}
	return upd
}

func dedupeNames(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// callKeywords are never recorded as call names.
var callKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "return": true,
	"func": true, "fn": true, "def": true, "class": true, "new": true,
	"sizeof": true, "typeof": true, "import": true, "package": true,
	"len": true, "cap": true, "make": true, "append": true, "panic": true,
	"println": true, "print": true, "delete": true, "copy": true,
}

var callNamePattern = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// extractCallNames returns bare callee names from call-like text,
// minus language keywords. Deterministic and conservative.
func extractCallNames(body string) []string {
	var out []string
	for _, m := range callNamePattern.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if !callKeywords[name] {
			out = append(out, name)
		}
	}
	return dedupeNames(out)
}

// ResolveImport maps a raw import string to a module node ID. It is
// purely syntactic (no filesystem access) so BuildUpdate stays a pure
// function of parser output:
//
//   - relative imports (./x, ../x, pkg.mod) resolve to the directory
//     module of the syntactic target: mod:dir:<lang>:<dir>;
//   - Go import paths keep their raw form: mod:go:<path>;
//   - anything else is external: mod:ext:<raw>.
//
// Dangling modules (no indexed file under their directory) are
// harmless: dependency queries over them return empty.
func ResolveImport(rel, lang, imp string) string {
	all := ResolveImports(rel, lang, imp)
	if len(all) == 0 {
		return ModuleID("ext:" + strings.TrimSpace(imp))
	}
	return all[0]
}

// ResolveImports maps a raw import string to module node IDs (see
// ResolveImport). Extensionless targets (./lib, pkg.core) are
// ambiguous between a sibling file and a package directory, so both
// the target directory module and its parent directory module are
// returned: dependents match on either, dependencies list both.
func ResolveImports(rel, lang, imp string) []string {
	imp = strings.TrimSpace(imp)
	if imp == "" {
		return nil
	}
	if lang == "go" {
		// Go imports are module paths; without go.mod parsing the
		// target module node keeps the raw path (the dependencies
		// action still lists them; the indexer rewrites first-party
		// paths to directory modules; call-graph closure covers
		// test selection).
		return []string{ModuleID("go:" + imp)}
	}
	if dir, ok := resolveTargetDir(rel, imp); ok {
		if dir == "." {
			dir = ""
		}
		// Same-language by construction (relative and dotted
		// first-party imports); only a real extension on the import
		// itself overrides the language.
		targetLang := lang
		if ext := path.Ext(imp); ext != "" {
			if l, ok := extensionLangs[strings.ToLower(ext)]; ok && !IsDocLang(l) {
				targetLang = l
			}
		}
		main := ModuleID("dir:" + targetLang + ":" + dir)
		if hasKnownExt(resolveTargetFile(rel, imp)) {
			return []string{main}
		}
		parent := ModuleID("dir:" + targetLang + ":" + dirOf(dir))
		if parent == main {
			return []string{main}
		}
		return []string{main, parent}
	}
	return []string{ModuleID("ext:" + imp)}
}

// resolveTargetFile recovers the syntactic target file of an import
// for extension probing ("" when the import names a directory or a
// bare package).
func resolveTargetFile(rel, imp string) string {
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return path.Clean(path.Join(path.Dir(rel), imp))
	}
	return ""
}

func hasKnownExt(target string) bool {
	if target == "" {
		return false
	}
	_, ok := extensionLangs[strings.ToLower(path.Ext(target))]
	return ok
}

// resolveTargetDir computes the syntactic target directory of an
// import; ok is false for external (non-relative, non-dotted) imports.
func resolveTargetDir(rel, imp string) (dir string, ok bool) {
	relImport := strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../")
	dotted := isBareImport(imp) && strings.Contains(imp, ".")
	if !relImport && !dotted {
		return "", false
	}
	var target string
	if relImport {
		target = path.Clean(path.Join(path.Dir(rel), imp))
	} else {
		target = path.Clean(strings.ReplaceAll(imp, ".", "/"))
	}
	base := path.Base(target)
	if base == "index" || base == "__init__" {
		return path.Dir(target), true
	}
	if _, known := extensionLangs[strings.ToLower(path.Ext(target))]; known {
		return path.Dir(target), true
	}
	// Extensionless target: a directory import (index file) or a
	// same-named file; the directory covers both for dependents.
	return target, true
}

func isBareImport(imp string) bool {
	if imp == "" {
		return false
	}
	for _, r := range imp {
		if !(r == '_' || r == '.' || r == '/' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return !strings.Contains(imp, ":") && !strings.Contains(imp, " ")
}
