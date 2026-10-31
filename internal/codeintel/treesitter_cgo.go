//go:build cgo

package codeintel

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// TreeSitterAvailable reports whether the tree-sitter backend is
// compiled in. It is true on cgo builds (Section 10.10: the graph is
// built with tree-sitter, deterministic and multilingual) and false
// behind the !cgo stub, where the pure-Go fallback parses instead.
func TreeSitterAvailable() bool { return true }

// tsLang binds a language key to its grammar.
type tsLang struct {
	lang     string
	grammar  *sitter.Language
	defKinds map[string]string
	callKind string
}

func tsLanguages() []*tsLang {
	return []*tsLang{
		{lang: "go", grammar: golang.GetLanguage(), defKinds: map[string]string{
			"function_declaration": SymbolFunc,
			"method_declaration":   SymbolMethod,
			"type_spec":            SymbolType,
		}, callKind: "call_expression"},
		{lang: "python", grammar: python.GetLanguage(), defKinds: map[string]string{
			"function_definition": SymbolFunc,
			"class_definition":    SymbolType,
		}, callKind: "call"},
		{lang: "javascript", grammar: javascript.GetLanguage(), defKinds: map[string]string{
			"function_declaration": SymbolFunc,
			"method_definition":    SymbolMethod,
			"class_declaration":    SymbolType,
		}, callKind: "call_expression"},
		{lang: "typescript", grammar: typescript.GetLanguage(), defKinds: map[string]string{
			"function_declaration":   SymbolFunc,
			"method_definition":      SymbolMethod,
			"class_declaration":      SymbolType,
			"interface_declaration":  SymbolType,
			"type_alias_declaration": SymbolType,
			"enum_declaration":       SymbolType,
		}, callKind: "call_expression"},
		{lang: "rust", grammar: rust.GetLanguage(), defKinds: map[string]string{
			"function_item": SymbolFunc,
			"struct_item":   SymbolType,
			"enum_item":     SymbolType,
			"trait_item":    SymbolType,
			"type_item":     SymbolType,
		}, callKind: "call_expression"},
		{lang: "java", grammar: java.GetLanguage(), defKinds: map[string]string{
			"method_declaration":      SymbolMethod,
			"constructor_declaration": SymbolMethod,
			"class_declaration":       SymbolType,
			"interface_declaration":   SymbolType,
			"enum_declaration":        SymbolType,
		}, callKind: "method_invocation"},
		{lang: "c", grammar: c.GetLanguage(), defKinds: map[string]string{
			"function_definition": SymbolFunc,
			"struct_specifier":    SymbolType,
			"enum_specifier":      SymbolType,
		}, callKind: "call_expression"},
		{lang: "cpp", grammar: cpp.GetLanguage(), defKinds: map[string]string{
			"function_definition": SymbolFunc,
			"declaration":         SymbolFunc, // function prototypes share this node
			"class_specifier":     SymbolType,
			"struct_specifier":    SymbolType,
			"enum_specifier":      SymbolType,
		}, callKind: "call_expression"},
		{lang: "ruby", grammar: ruby.GetLanguage(), defKinds: map[string]string{
			"method": SymbolFunc,
			"class":  SymbolType,
			"module": SymbolType,
		}, callKind: "call"},
	}
}

// tsx shares the TypeScript definition shapes.
func tsxLang() *tsLang {
	return &tsLang{lang: "tsx", grammar: tsx.GetLanguage(), defKinds: map[string]string{
		"function_declaration":   SymbolFunc,
		"method_definition":      SymbolMethod,
		"class_declaration":      SymbolType,
		"interface_declaration":  SymbolType,
		"type_alias_declaration": SymbolType,
		"enum_declaration":       SymbolType,
	}, callKind: "call_expression"}
}

// treeSitterParserFor returns the tree-sitter parser for lang, or nil
// when the language has no bundled grammar (the caller falls back to
// the pure-Go parser).
func treeSitterParserFor(lang string) Parser {
	for _, l := range tsLanguages() {
		if l.lang == lang {
			return &tsParser{def: l}
		}
	}
	if lang == "tsx" {
		return &tsParser{def: tsxLang()}
	}
	return nil
}

// TreeSitterLanguages lists the languages with a bundled grammar.
func TreeSitterLanguages() []string {
	out := []string{}
	for _, l := range tsLanguages() {
		out = append(out, l.lang)
	}
	return append(out, "tsx")
}

type tsParser struct {
	def *tsLang
}

// Langs implements Parser.
func (p *tsParser) Langs() []string { return []string{p.def.lang} }

// Parse implements Parser.
func (p *tsParser) Parse(rel string, src []byte) (*ParsedFile, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(p.def.grammar)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, fmt.Errorf("codeintel: tree-sitter parse %s: %w", rel, err)
	}
	defer tree.Close()
	pf := &ParsedFile{Rel: rel, Lang: p.def.lang, IsTest: IsTestPath(rel, p.def.lang)}
	if IsDocLang(p.def.lang) {
		pf.Sections = parseDocSections(string(src))
		return pf, nil
	}
	root := tree.RootNode()
	walkTS(pf, p.def, root, src)
	pf.Imports = dedupeNames(pf.Imports)
	if p.def.lang == "go" {
		// Tree-sitter-go import shape varies by grammar version;
		// reuse the exact go/ast import extraction as the source of
		// truth for imports while symbols come from the tree.
		var gpf ParsedFile
		gpf.Rel = rel
		if err := parseGoInto(&gpf, src); err == nil {
			pf.Imports = gpf.Imports
		}
	} else {
		for _, re := range importRules[p.def.lang] {
			for _, m := range re.FindAllStringSubmatch(string(src), -1) {
				imp := strings.Trim(strings.TrimSpace(m[1]), `'"`)
				if imp != "" {
					pf.Imports = append(pf.Imports, imp)
				}
			}
		}
		pf.Imports = dedupeNames(pf.Imports)
	}
	for i := range pf.Symbols {
		pf.Symbols[i].Calls = dedupeNames(pf.Symbols[i].Calls)
		kept := pf.Symbols[i].Calls[:0]
		for _, c := range pf.Symbols[i].Calls {
			if c != pf.Symbols[i].Name {
				kept = append(kept, c)
			}
		}
		pf.Symbols[i].Calls = kept
		pf.Symbols[i].Refs = dedupeNames(pf.Symbols[i].Refs)
	}
	return pf, nil
}

// walkTS collects definitions and call names in source order.
func walkTS(pf *ParsedFile, def *tsLang, n *sitter.Node, src []byte) {
	if n == nil || n.IsNull() {
		return
	}
	if sub, ok := def.defKinds[n.Type()]; ok && n.IsNamed() {
		name := tsDefName(n, src)
		if name != "" && !callKeywords[name] {
			sym := ParsedSymbol{
				Name:    name,
				SubKind: sub,
				Line:    int(n.StartPoint().Row) + 1,
				EndLine: int(n.EndPoint().Row) + 1,
			}
			collectTSCalls(pf, def, n, src, &sym)
			pf.Symbols = append(pf.Symbols, sym)
		}
	}
	// Recurse into children (nested definitions included).
	count := n.ChildCount()
	for i := uint32(0); i < count; i++ {
		walkTS(pf, def, n.Child(int(i)), src)
	}
}

// tsDefName reads a definition name: the `name` field first, then the
// first identifier-like named child.
func tsDefName(n *sitter.Node, src []byte) string {
	if f := n.ChildByFieldName("name"); f != nil && !f.IsNull() {
		if t := f.Type(); t == "identifier" || t == "type_identifier" ||
			t == "property_identifier" || t == "field_identifier" || t == "name" {
			return f.Content(src)
		}
	}
	count := n.NamedChildCount()
	for i := uint32(0); i < count; i++ {
		c := n.NamedChild(int(i))
		if c == nil || c.IsNull() {
			continue
		}
		switch c.Type() {
		case "identifier", "type_identifier", "property_identifier",
			"field_identifier", "name", "namespace_name", "constant":
			return c.Content(src)
		}
	}
	return ""
}

// collectTSCalls gathers call and reference names inside a definition
// body, skipping nested definitions (they contribute their own).
func collectTSCalls(pf *ParsedFile, def *tsLang, n *sitter.Node, src []byte, sym *ParsedSymbol) {
	count := n.ChildCount()
	for i := uint32(0); i < count; i++ {
		c := n.Child(int(i))
		if c == nil || c.IsNull() {
			continue
		}
		if _, isDef := def.defKinds[c.Type()]; isDef {
			continue
		}
		tsCallsIn(pf, def, c, src, sym)
	}
}

func tsCallsIn(pf *ParsedFile, def *tsLang, n *sitter.Node, src []byte, sym *ParsedSymbol) {
	if n == nil || n.IsNull() {
		return
	}
	if n.Type() == def.callKind {
		if name := tsCalleeName(n, src); name != "" && !callKeywords[name] {
			sym.Calls = append(sym.Calls, name)
		}
		return
	}
	count := n.ChildCount()
	for i := uint32(0); i < count; i++ {
		tsCallsIn(pf, def, n.Child(int(i)), src, sym)
	}
}

// tsCalleeName resolves the callee of a call node: a bare identifier,
// or the selected member of a selector/member/attribute expression.
func tsCalleeName(n *sitter.Node, src []byte) string {
	fn := n.ChildByFieldName("function")
	if fn == nil || fn.IsNull() {
		fn = n.ChildByFieldName("name")
	}
	if fn == nil || fn.IsNull() {
		if c := n.Child(0); c != nil && !c.IsNull() {
			fn = c
		} else {
			return ""
		}
	}
	switch fn.Type() {
	case "identifier", "name":
		return fn.Content(src)
	case "selector_expression", "member_expression", "attribute",
		"scoped_identifier", "field_expression":
		if f := fn.ChildByFieldName("field"); f != nil && !f.IsNull() {
			return f.Content(src)
		}
		if f := fn.ChildByFieldName("property"); f != nil && !f.IsNull() {
			return f.Content(src)
		}
		if f := fn.ChildByFieldName("name"); f != nil && !f.IsNull() {
			return f.Content(src)
		}
		// Last identifier-like child (the member, not the receiver).
		var last string
		count := fn.ChildCount()
		for i := uint32(0); i < count; i++ {
			c := fn.Child(int(i))
			if c == nil || c.IsNull() {
				continue
			}
			switch c.Type() {
			case "identifier", "property_identifier", "field_identifier":
				last = c.Content(src)
			}
		}
		return last
	default:
		// Decorators, macros and callables used as values: the
		// identifier when the function node is a plain name.
		if fn.IsNamed() && strings.Count(fn.Content(src), "\n") == 0 &&
			len(fn.Content(src)) < 64 {
			text := fn.Content(src)
			if isIdentLike(text) {
				return text
			}
		}
		return ""
	}
}

func isIdentLike(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}
