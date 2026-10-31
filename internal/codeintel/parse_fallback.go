package codeintel

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"regexp"
	"strings"
)

// fallbackParserFor returns the deterministic pure-Go parser for a
// language. It is the only parser on builds without cgo and the
// safety net on cgo builds for languages without a bundled grammar.
func fallbackParserFor(lang string) Parser {
	return &fallbackParser{lang: lang}
}

type fallbackParser struct{ lang string }

// Langs implements Parser.
func (p *fallbackParser) Langs() []string { return []string{p.lang} }

// Parse implements Parser.
func (p *fallbackParser) Parse(rel string, src []byte) (*ParsedFile, error) {
	pf := &ParsedFile{Rel: rel, Lang: p.lang, IsTest: IsTestPath(rel, p.lang)}
	if IsDocLang(p.lang) {
		pf.Sections = parseDocSections(string(src))
		return pf, nil
	}
	if p.lang == "go" {
		return pf, parseGoInto(pf, src)
	}
	parseGenericInto(pf, string(src))
	return pf, nil
}

// parseGoInto parses Go exactly with go/parser: functions, methods,
// types, imports and call expressions (identifiers and selectors).
func parseGoInto(pf *ParsedFile, src []byte) error {
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, pf.Rel, src, goparser.SkipObjectResolution)
	if err != nil {
		// Unparseable Go still yields its imports by regex so the
		// module graph degrades gracefully instead of going blind.
		pf.Imports = goImportFallback(string(src))
		pf.Symbols = goDefFallback(string(src))
		return nil
	}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		pf.Imports = append(pf.Imports, p)
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sub := SymbolFunc
			if d.Recv != nil {
				sub = SymbolMethod
			}
			sym := ParsedSymbol{
				Name:    d.Name.Name,
				SubKind: sub,
				Line:    fset.Position(d.Pos()).Line,
				EndLine: fset.Position(d.End()).Line,
			}
			if d.Body != nil {
				calls, refs := goCalls(d.Body)
				sym.Calls = calls
				sym.Refs = refs
			}
			pf.Symbols = append(pf.Symbols, sym)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				pf.Symbols = append(pf.Symbols, ParsedSymbol{
					Name:    ts.Name.Name,
					SubKind: SymbolType,
					Line:    fset.Position(ts.Pos()).Line,
					EndLine: fset.Position(ts.End()).Line,
				})
			}
		}
	}
	return nil
}

// goCalls collects callee names from a function body: plain calls
// (foo()) and selector calls (pkg.Foo(), r.Method()) by selector.
func goCalls(body *ast.BlockStmt) (calls, refs []string) {
	seen := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if !callKeywords[fn.Name] && !seen[fn.Name] {
				seen[fn.Name] = true
				calls = append(calls, fn.Name)
			}
		case *ast.SelectorExpr:
			if !callKeywords[fn.Sel.Name] && !seen[fn.Sel.Name] {
				seen[fn.Sel.Name] = true
				calls = append(calls, fn.Sel.Name)
			}
		}
		return true
	})
	return calls, nil
}

var goImportLine = regexp.MustCompile(`(?m)^\s*(?:(?:[A-Za-z_][A-Za-z0-9_]*|\.|_)\s+)?(?:"([^"]+)"|` + "`([^`]+)`" + `)\s*(?://.*)?$`)

// goImportFallback recovers import paths from broken Go files.
func goImportFallback(src string) []string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "import" || strings.HasPrefix(t, "import ") || strings.HasPrefix(t, "import\t") || strings.HasPrefix(t, "import(") {
			if strings.Contains(t, "(") {
				inBlock = true
			} else if m := goImportLine.FindStringSubmatch(t[strings.Index(t, "import")+6:]); m != nil {
				out = append(out, firstNonEmpty(m[1], m[2]))
			}
			continue
		}
		if inBlock {
			if strings.HasPrefix(t, ")") {
				inBlock = false
				continue
			}
			if m := goImportLine.FindStringSubmatch(t); m != nil {
				out = append(out, firstNonEmpty(m[1], m[2]))
			}
		}
	}
	return out
}

var goFuncFallback = regexp.MustCompile(`(?m)^func\s+(?:(\([^)]*\))\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
var goTypeFallback = regexp.MustCompile(`(?m)^type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

// goDefFallback recovers definitions from broken Go files.
func goDefFallback(src string) []ParsedSymbol {
	var out []ParsedSymbol
	for i, line := range strings.Split(src, "\n") {
		if m := goFuncFallback.FindStringSubmatch(line); m != nil {
			sub := SymbolFunc
			if strings.TrimSpace(m[1]) != "" {
				sub = SymbolMethod
			}
			out = append(out, ParsedSymbol{Name: m[2], SubKind: sub, Line: i + 1, EndLine: i + 1})
		} else if m := goTypeFallback.FindStringSubmatch(line); m != nil {
			out = append(out, ParsedSymbol{Name: m[1], SubKind: SymbolType, Line: i + 1, EndLine: i + 1})
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// defRule is one definition regex for a language family.
type defRule struct {
	re      *regexp.Regexp
	subKind string
}

// genericRules maps language keys to definition patterns (applied in
// order; first match per line wins).
var genericRules = map[string][]defRule{
	"python": {
		{regexp.MustCompile(`^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), SymbolFunc},
		{regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`), SymbolType},
	},
	"javascript": {
		{regexp.MustCompile(`^\s*(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_$]*)\s*\(`), SymbolFunc},
		{regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_$]*)\b`), SymbolType},
		{regexp.MustCompile(`^\s*(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?\(`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?\([^)]*\)\s*=>`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:async\s+)?([A-Za-z_][A-Za-z0-9_$]*)\s*\([^)]*\)\s*\{`), SymbolMethod},
	},
	"typescript": {
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_$]*)\s*[<\(]`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_][A-Za-z0-9_$]*)\b`), SymbolType},
		{regexp.MustCompile(`^\s*(?:export\s+)?interface\s+([A-Za-z_][A-Za-z0-9_$]*)\b`), SymbolType},
		{regexp.MustCompile(`^\s*(?:export\s+)?type\s+([A-Za-z_][A-Za-z0-9_$]*)\s*=`), SymbolType},
		{regexp.MustCompile(`^\s*(?:export\s+)?enum\s+([A-Za-z_][A-Za-z0-9_$]*)\b`), SymbolType},
		{regexp.MustCompile(`^\s*(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?\(`), SymbolFunc},
	},
	"java": {
		{regexp.MustCompile(`^\s*(?:public|private|protected)?\s*(?:static\s+|final\s+|abstract\s+)*(?:[\w<>\[\]]+\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), SymbolMethod},
		{regexp.MustCompile(`^\s*(?:public|private|protected)?\s*(?:static\s+|final\s+|abstract\s+)*(?:class|interface|enum|record)\s+([A-Za-z_][A-Za-z0-9_]*)\b`), SymbolType},
	},
	"c": {
		{regexp.MustCompile(`^\s*(?:static\s+|inline\s+|extern\s+)*[A-Za-z_][A-Za-z0-9_*\s]+\s+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*$`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:typedef\s+)?(?:struct|enum|union)\s+([A-Za-z_][A-Za-z0-9_]*)\b`), SymbolType},
	},
	"cpp": {
		{regexp.MustCompile(`^\s*(?:static\s+|inline\s+|virtual\s+|explicit\s+)*[A-Za-z_:][A-Za-z0-9_:<>,\s*&]+\s+([A-Za-z_][A-Za-z0-9_:]*)\s*\([^;]*$`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:class|struct|enum)\s+([A-Za-z_][A-Za-z0-9_]*)\b`), SymbolType},
	},
	"rust": {
		{regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*[<\(]`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait|union|type)\s+([A-Za-z_][A-Za-z0-9_]*)\b`), SymbolType},
	},
	"ruby": {
		{regexp.MustCompile(`^\s*def\s+(?:self\.)?([A-Za-z_][A-Za-z0-9_?!]*)\b`), SymbolFunc},
		{regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_:]*)\b`), SymbolType},
		{regexp.MustCompile(`^\s*module\s+([A-Za-z_][A-Za-z0-9_:]*)\b`), SymbolType},
	},
	"php": {
		{regexp.MustCompile(`^\s*(?:public|private|protected)?\s*(?:static\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), SymbolFunc},
		{regexp.MustCompile(`^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait|enum)\s+([A-Za-z_][A-Za-z0-9_]*)\b`), SymbolType},
	},
}

// importRules maps language keys to import patterns (first group).
var importRules = map[string][]*regexp.Regexp{
	"python": {
		regexp.MustCompile(`(?m)^\s*import\s+([a-zA-Z_][\w.]*)`),
		regexp.MustCompile(`(?m)^\s*from\s+(\.+[a-zA-Z_][\w.]*|\.+|[a-zA-Z_][\w.]*)\s+import\b`),
	},
	"javascript": {
		regexp.MustCompile(`(?m)^\s*import\s+(?:[^'"]*?\s+from\s+)?['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?m)(?:require|import)\s*\(\s*['"]([^'"]+)['"]\s*\)`),
	},
	"typescript": {
		regexp.MustCompile(`(?m)^\s*import\s+(?:[^'"]*?\s+from\s+)?['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?m)(?:require|import)\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`(?m)^\s*export\s+[^'"]*?\s+from\s+['"]([^'"]+)['"]`),
	},
	"java": {
		regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.]+)\s*;`),
	},
	"c": {
		regexp.MustCompile(`(?m)^\s*#\s*include\s+[<"]([^>"]+)[>"]`),
	},
	"cpp": {
		regexp.MustCompile(`(?m)^\s*#\s*include\s+[<"]([^>"]+)[>"]`),
	},
	"rust": {
		regexp.MustCompile(`(?m)^\s*use\s+([\w:{},*\s]+?)\s*;`),
	},
	"ruby": {
		regexp.MustCompile(`(?m)^\s*(?:require|require_relative|load)\s+['"]([^'"]+)['"]`),
	},
	"php": {
		regexp.MustCompile(`(?m)^\s*use\s+([\w\\]+)\s*;`),
	},
}

// parseGenericInto parses non-Go sources with the regex families.
func parseGenericInto(pf *ParsedFile, src string) {
	for _, re := range importRules[pf.Lang] {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			imp := strings.TrimSpace(m[1])
			imp = strings.Trim(imp, `'"`)
			if imp != "" {
				pf.Imports = append(pf.Imports, imp)
			}
		}
	}
	pf.Imports = dedupeNames(pf.Imports)
	rules := genericRules[pf.Lang]
	if len(rules) == 0 {
		return
	}
	lines := strings.Split(src, "\n")
	type openSym struct {
		idx    int
		indent int
	}
	var stack []openSym
	popTo := func(ln, indent int, strict bool) {
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if strict && top.indent < indent {
				break
			}
			if !strict && top.indent <= indent {
				break
			}
			stack = stack[:len(stack)-1]
			if pf.Symbols[top.idx].EndLine <= pf.Symbols[top.idx].Line {
				pf.Symbols[top.idx].EndLine = ln - 1
			}
		}
	}
	for i, line := range lines {
		ln := i + 1
		if strings.TrimSpace(line) == "" {
			continue // blank lines neither end scopes nor hold calls
		}
		indent := leadingSpaces(line)
		matched := false
		// Statement lines (return size();) never define: without
		// this guard the method pattern would promote the callee
		// to a symbol and swallow its own call as self-recursion.
		for _, r := range rules {
			if stmtGuard.MatchString(strings.TrimSpace(line)) {
				break
			}
			if m := r.re.FindStringSubmatch(line); m != nil {
				name := m[1]
				// C/C++ heuristic: skip control keywords leaking
				// through the function pattern.
				if callKeywords[name] {
					continue
				}
				// A new definition ends siblings and outer
				// scopes at its own indent or deeper.
				popTo(ln, indent, true)
				pf.Symbols = append(pf.Symbols, ParsedSymbol{
					Name: name, SubKind: r.subKind, Line: ln, EndLine: ln,
				})
				stack = append(stack, openSym{idx: len(pf.Symbols) - 1, indent: indent})
				matched = true
				break
			}
		}
		if !matched {
			// A dedented plain line (closing brace, next
			// statement) ends deeper scopes: brace-close and
			// dedent handling the fallback cannot see
			// structurally. Same-indent body lines keep the
			// scope open.
			popTo(ln, indent, false)
		}
	}
	total := len(lines)
	for _, top := range stack {
		if pf.Symbols[top.idx].EndLine <= pf.Symbols[top.idx].Line {
			pf.Symbols[top.idx].EndLine = total
		}
	}
	// Attribute each line's call names to the innermost enclosing
	// symbol; module-level calls attach to nothing (they still shape
	// imports, which carry cross-file relations).
	for i, line := range lines {
		ln := i + 1
		names := extractCallNames(line)
		if len(names) == 0 {
			continue
		}
		best := -1
		for j, s := range pf.Symbols {
			if s.Line <= ln && ln <= s.EndLine && (best < 0 || s.Line > pf.Symbols[best].Line) {
				best = j
			}
		}
		if best >= 0 {
			pf.Symbols[best].Calls = append(pf.Symbols[best].Calls, names...)
		}
	}
	for j := range pf.Symbols {
		pf.Symbols[j].Calls = dedupeNames(pf.Symbols[j].Calls)
		// Drop self-calls; the graph never stores reflexive edges.
		kept := pf.Symbols[j].Calls[:0]
		for _, c := range pf.Symbols[j].Calls {
			if c != pf.Symbols[j].Name {
				kept = append(kept, c)
			}
		}
		pf.Symbols[j].Calls = kept
	}
}

// stmtGuard marks statement lines that can never hold a definition
// (return/if/for/...): the generic definition patterns would
// otherwise promote callees to symbols.
var stmtGuard = regexp.MustCompile(`^(?:return|if|for|while|switch|catch|throw|new|else|do|assert|yield)\b`)

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
		} else if r == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

var docHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
var docMention = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_.]*)`")

// parseDocSections splits a document into headed sections and records
// the code-like names each section mentions.
func parseDocSections(src string) []ParsedSection {
	lines := strings.Split(src, "\n")
	type open struct {
		heading string
		slug    string
		line    int
	}
	var stack []open
	var out []ParsedSection
	flush := func(endLine int) {
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			out = append(out, ParsedSection{
				Heading: top.heading, Slug: top.slug,
				Line: top.line, EndLine: endLine,
			})
		}
	}
	for i, line := range lines {
		ln := i + 1
		if m := docHeading.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			for len(stack) >= level {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				out = append(out, ParsedSection{
					Heading: top.heading, Slug: top.slug,
					Line: top.line, EndLine: ln - 1,
				})
			}
			h := strings.TrimSpace(m[2])
			stack = append(stack, open{heading: h, slug: Slugify(h), line: ln})
		}
	}
	flush(len(lines))
	// Mentions per section body.
	for i := range out {
		var body strings.Builder
		for l := out[i].Line; l <= out[i].EndLine && l <= len(lines); l++ {
			body.WriteString(lines[l-1])
			body.WriteString("\n")
		}
		var mentions []string
		for _, m := range docMention.FindAllStringSubmatch(body.String(), -1) {
			mentions = append(mentions, m[1])
		}
		out[i].Mentions = dedupeNames(mentions)
	}
	return out
}
