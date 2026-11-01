package codeintel

import (
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	for _, rel := range []string{"main.go", "x.py", "a.js", "c.ts", "d.tsx",
		"e.java", "f.rs", "g.rb", "h.php", "i.c", "j.cpp", "k.hpp"} {
		if _, ok := DetectLanguage(rel); !ok {
			t.Errorf("no language for %s", rel)
		}
	}
	for _, rel := range []string{"a.go", "b.py"} {
		_ = rel
	}
	if _, ok := DetectLanguage("notes.txt"); !ok {
		// txt maps to doc.
		t.Log("txt is a doc language")
	}
	if _, ok := DetectLanguage("blob.bin"); ok {
		t.Error("binary extension should be out of scope")
	}
	if _, ok := DetectLanguage("Makefile"); ok {
		t.Error("extensionless file should be out of scope")
	}
	if !IsDocLang("doc") || IsDocLang("go") {
		t.Error("doc language classification wrong")
	}
}

func TestIsTestPath(t *testing.T) {
	tests := []struct{ rel, lang string }{
		{"pkg/a_test.go", "go"},
		{"tests/test_app.py", "python"},
		{"test_app.py", "python"},
		{"web/x.test.js", "javascript"},
		{"web/__tests__/x.js", "javascript"},
		{"web/x.spec.ts", "typescript"},
		{"TestFoo.java", "java"},
		{"foo/bar_test.rb", "ruby"},
	}
	for _, tc := range tests {
		if !IsTestPath(tc.rel, tc.lang) {
			t.Errorf("expected test path %s (%s)", tc.rel, tc.lang)
		}
	}
	nonTests := []struct{ rel, lang string }{
		{"pkg/a.go", "go"},
		{"app.py", "python"},
		{"web/x.js", "javascript"},
		{"Foo.java", "java"},
	}
	for _, tc := range nonTests {
		if IsTestPath(tc.rel, tc.lang) {
			t.Errorf("false positive test path %s (%s)", tc.rel, tc.lang)
		}
	}
}

func TestParseGoExact(t *testing.T) {
	src := `package calc

import "fmt"

type Acc struct{ total int }

func (a *Acc) Add(n int) int {
	a.total += n
	fmt.Println(a.total)
	return a.total
}

func Sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}
`
	upd, err := ParseFile("calc/calc.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if upd.Lang != "go" || upd.Hash == "" {
		t.Fatalf("update header: %+v", upd)
	}
	names := map[string]string{}
	for _, n := range upd.Nodes {
		if n.Kind == NodeSymbol {
			names[n.Name] = n.SubKind
		}
	}
	if names["Acc"] != SymbolType {
		t.Errorf("Acc: %+v", names)
	}
	if names["Add"] != SymbolMethod {
		t.Errorf("Add: %+v", names)
	}
	if names["Sum"] != SymbolFunc {
		t.Errorf("Sum: %+v", names)
	}
	// Method call on Println recorded via selector.
	var calls []string
	for _, u := range upd.Uses {
		if u.Kind == UseCalls {
			calls = append(calls, u.Name)
		}
	}
	found := false
	for _, c := range calls {
		if c == "Println" {
			found = true
		}
	}
	if !found {
		t.Errorf("Println call missing: %v", calls)
	}
}

func TestParseFallbackFamilies(t *testing.T) {
	cases := []struct {
		rel     string
		src     string
		symbols []string
		imports int
		calls   []string
	}{
		{"a.py", "import os\nfrom . import util\n\ndef foo():\n    bar(1)\n\nclass Baz:\n    pass\n",
			[]string{"foo", "Baz"}, 2, []string{"bar"}},
		{"a.js", "import {x} from './lib';\nconst cfg = require('./cfg');\nfunction render(el) {\n  draw(el);\n}\nclass View {}\n",
			[]string{"render", "View"}, 3, []string{"draw"}},
		{"a.ts", "import {x} from './lib';\nexport interface Shape {}\nexport function area(s) {\n  return calc(s);\n}\n",
			[]string{"Shape", "area"}, 2, []string{"calc"}},
		{"A.java", "import java.util.List;\npublic class A {\n  public int get() {\n    return size();\n  }\n}\n",
			[]string{"A", "get"}, 2, []string{"size"}},
		{"a.rs", "use std::io;\nfn main() {\n    run();\n}\nstruct Cfg {}\n",
			[]string{"main", "Cfg"}, 1, []string{"run"}},
		{"a.rb", "require 'json'\ndef greet(name)\n  puts(name)\nend\nclass Greeter\nend\n",
			[]string{"greet", "Greeter"}, 1, []string{"puts"}},
	}
	for _, tc := range cases {
		upd, err := ParseFile(tc.rel, []byte(tc.src))
		if err != nil {
			t.Fatalf("%s: %v", tc.rel, err)
		}
		got := map[string]bool{}
		for _, n := range upd.Nodes {
			if n.Kind == NodeSymbol {
				got[n.Name] = true
			}
		}
		for _, want := range tc.symbols {
			if !got[want] {
				t.Errorf("%s: missing symbol %s (got %v)", tc.rel, want, got)
			}
		}
		nImp := 0
		for _, e := range upd.Edges {
			if e.Kind == EdgeImports {
				nImp++
			}
		}
		if nImp != tc.imports {
			t.Errorf("%s: imports=%d want %d", tc.rel, nImp, tc.imports)
		}
		callSet := map[string]bool{}
		for _, u := range upd.Uses {
			if u.Kind == UseCalls {
				callSet[u.Name] = true
			}
		}
		for _, want := range tc.calls {
			if !callSet[want] {
				t.Errorf("%s: missing call %s", tc.rel, want)
			}
		}
	}
}

func TestParseAsyncPythonFunction(t *testing.T) {
	pf, err := fallbackParserFor("python").Parse("async_app.py", []byte("async def fetch():\n    await request()\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Symbols) != 1 || pf.Symbols[0].Name != "fetch" || pf.Symbols[0].SubKind != SymbolFunc {
		t.Fatalf("async function not parsed: %+v", pf.Symbols)
	}
	if len(pf.Symbols[0].Calls) != 1 || pf.Symbols[0].Calls[0] != "request" {
		t.Fatalf("async function calls not attributed: %+v", pf.Symbols[0].Calls)
	}
}

func TestParseDocSections(t *testing.T) {
	src := "# Guide\n\nUses `Handler` and `parseTree`.\n\n## Setup\n\nRun `install`.\n"
	upd, err := ParseFile("docs/guide.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if upd.Lang != "doc" {
		t.Fatalf("lang: %s", upd.Lang)
	}
	// file + module + 2 sections.
	if len(upd.Nodes) != 4 {
		t.Fatalf("nodes: %+v", upd.Nodes)
	}
	cites := 0
	for _, u := range upd.Uses {
		if u.Kind == UseCites {
			cites++
		}
	}
	// Guide mentions Handler, parseTree and install (Setup's body is
	// inside Guide's span); Setup mentions install.
	if cites != 4 {
		t.Errorf("cites=%d want 4", cites)
	}
}

func TestParseUnknownExtension(t *testing.T) {
	upd, err := ParseFile("data/blob.bin", []byte("\x00\x01\x02"))
	if err != nil {
		t.Fatal(err)
	}
	if len(upd.Nodes) != 1 || upd.Nodes[0].Kind != NodeFile {
		t.Fatalf("nodes: %+v", upd.Nodes)
	}
}

func TestPreferredParserAgreement(t *testing.T) {
	// Both backends must agree on the contract: same symbols and
	// same call names for a representative fixture. On !cgo builds
	// this compares the fallback against itself.
	src := `package p

func Alpha() int { return Beta() + 1 }

func Beta() int { return 2 }

type Store struct{}

func (s *Store) Save() { Alpha() }
`
	langs := []string{"go", "python"}
	_ = langs
	fb := fallbackParserFor("go")
	pfa, err := fb.Parse("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	pb, err := PreferredParser("go").Parse("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	names := func(pf *ParsedFile) map[string][]string {
		m := map[string][]string{}
		for _, s := range pf.Symbols {
			m[s.Name] = append([]string(nil), s.Calls...)
		}
		return m
	}
	a, b := names(pfa), names(pb)
	if len(a) != len(b) {
		t.Fatalf("symbol sets differ: %v vs %v", a, b)
	}
	for name, ac := range a {
		bc, ok := b[name]
		if !ok {
			t.Fatalf("missing symbol %s in preferred parser", name)
		}
		am, bm := map[string]bool{}, map[string]bool{}
		for _, c := range ac {
			am[c] = true
		}
		for _, c := range bc {
			bm[c] = true
		}
		for c := range am {
			if !bm[c] {
				t.Errorf("symbol %s: fallback call %s missing in preferred", name, c)
			}
		}
	}
	t.Logf("tree-sitter available: %v", TreeSitterAvailable())
}

func TestGoImportFallbackAliased(t *testing.T) {
	got := goImportFallback("package p\nimport alias \"example.com/alias\"\nimport . \"example.com/dot\"\nimport (\n blank \"example.com/blank\" // comment\n \"example.com/plain\"\n)\n")
	want := []string{"example.com/alias", "example.com/dot", "example.com/blank", "example.com/plain"}
	if len(got) != len(want) {
		t.Fatalf("imports = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("imports = %v, want %v", got, want)
		}
	}
}
func TestGoImportFallbackDoesNotTreatIdentifiersAsImports(t *testing.T) {
	got := goImportFallback("package p\nimported := true\nimportAlias := 1\nimport \"fmt\"\n")
	if len(got) != 1 || got[0] != "fmt" {
		t.Fatalf("imports = %v, want [fmt]", got)
	}
}

func TestGoMalformedFallbackRecognizesIndentedDeclarations(t *testing.T) {
	pf := &ParsedFile{Rel: "broken.go", Lang: "go"}
	src := []byte("package p\nfunc (r *Receiver) Save() {\n\tfunc Load() {\n\ttype Config struct{}\n")
	if err := parseGoInto(pf, src); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, sym := range pf.Symbols {
		got[sym.Name] = sym.SubKind
	}
	if got["Save"] != SymbolMethod || got["Load"] != SymbolFunc || got["Config"] != SymbolType {
		t.Fatalf("fallback symbols = %v, want method Save, function Load, type Config", got)
	}
}
