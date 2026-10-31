package codeintel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Compile-time seams with the shared contracts (Sections 13.1, 13.6,
// 14.12): both tools implement tools.Tool, and the LSP client feeds
// the tools.DiagnosticProvider hook the edit tools consume.
var (
	_ tools.Tool               = (*CodeGraphTool)(nil)
	_ tools.Tool               = (*LSPTool)(nil)
	_ tools.DiagnosticProvider = (*Client)(nil)
)

func testDeps(t *testing.T) (*Deps, *Graph, string) {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":        "module example.com/proj\n\ngo 1.27\n",
		"calc/calc.go":  "package calc\n\nfunc Add(a, b int) int { return a + b }\n",
		"calc/usage.go": "package calc\n\nfunc Double(n int) int { return Add(n, n) }\n",
		"calc/calc_test.go": "package calc\n\nfunc TestAdd() { if Add(1, 2) != 3 { panic(\"x\") } }\n" +
			"func TestDouble() { if Double(2) != 4 { panic(\"x\") } }\n",
		"docs/spec.md": "# Spec\n\nImplements `Add` for R7.\n",
	})
	g, err := OpenGraph(root + ".db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	x := NewIndexer(g, root)
	if _, err := x.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &Deps{
		Open:           func(project string) (*Graph, error) { return g, nil },
		DefaultProject: root,
	}, g, root
}

func callTool(t *testing.T, tool tools.Tool, args map[string]any) tools.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Invoke(context.Background(), tools.Invocation{
		Tool:      tool.Definition().Name,
		Arguments: raw,
	})
	if err != nil {
		t.Fatalf("invoke error: %v", err)
	}
	return res
}

func TestDeferredRegistration(t *testing.T) {
	deps, _, _ := testDeps(t)
	reg := tools.NewMapRegistry()
	if err := Register(reg, deps); err != nil {
		t.Fatal(err)
	}
	names := reg.Names()
	if len(names) != 2 || names[0] != CodeGraphToolName || names[1] != LSPToolName {
		t.Fatalf("names: %v", names)
	}
	for _, want := range []string{CodeGraphToolName, LSPToolName} {
		tool, err := reg.Get(want)
		if err != nil {
			t.Fatal(err)
		}
		if err := tool.Definition().Validate(); err != nil {
			t.Fatalf("%s invalid: %v", want, err)
		}
		if err := tools.ValidateName(want); err != nil {
			t.Fatalf("bad name %s: %v", want, err)
		}
	}
	// Duplicate registration is rejected catalog-wide (14.12).
	if err := Register(reg, deps); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestExtensionManifest(t *testing.T) {
	m := Manifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	if m.Contract != tools.ExtensionContract || m.Kind != tools.ExtensionBuiltin {
		t.Fatalf("manifest: %+v", m)
	}
	if len(m.Tools) != 2 {
		t.Fatalf("manifest tools: %+v", m.Tools)
	}
	// Every deferred tool carries discovery hints for search_tools
	// (verb, object, domain) and a usable JSON schema.
	for _, et := range m.Tools {
		if et.Description == "" || et.Group == "" || len(et.Tags) == 0 || len(et.Keywords) == 0 {
			t.Errorf("%s lacks discovery hints", et.Name)
		}
		if !json.Valid([]byte(et.InputSchema)) {
			t.Errorf("%s has invalid schema", et.Name)
		}
		if et.Fast {
			t.Errorf("%s must not be master-fast", et.Name)
		}
	}
	// Cancel and effect classes per Section 13.1.
	lsp := m.Tools[1]
	if lsp.Cancel != "cooperative" || lsp.Effect != "write_local" || lsp.Idempotency != "keyed" {
		t.Errorf("lsp classes: %+v", lsp.Definition)
	}
	cg := m.Tools[0]
	if cg.Cancel != "immediate" || cg.Effect != "read_only" || cg.Idempotency != "idempotent" {
		t.Errorf("code_graph classes: %+v", cg.Definition)
	}
}

func TestCodeGraphActions(t *testing.T) {
	deps, g, _ := testDeps(t)
	tool := &CodeGraphTool{Deps: deps}
	mustOK := func(args map[string]any) string {
		t.Helper()
		res := callTool(t, tool, args)
		if res.Status != tools.StatusOK {
			t.Fatalf("%v -> %s: %s", args, res.Status, res.Output)
		}
		return res.Output
	}
	out := mustOK(map[string]any{"action": "callers", "symbol": "Add"})
	if !strings.Contains(out, "Double") || !strings.Contains(out, "calls") {
		t.Errorf("callers: %s", out)
	}
	out = mustOK(map[string]any{"action": "callees", "symbol": "Double"})
	if !strings.Contains(out, "Add") {
		t.Errorf("callees: %s", out)
	}
	out = mustOK(map[string]any{"action": "symbols", "file": "calc/calc.go"})
	if !strings.Contains(out, "Add") {
		t.Errorf("symbols: %s", out)
	}
	// Same-package Go files import nothing: no dependencies.
	out = mustOK(map[string]any{"action": "dependencies", "file": "calc/usage.go"})
	if !strings.Contains(out, "(none)") {
		t.Errorf("dependencies: %s", out)
	}
	// Unknown modules report no importers instead of failing.
	out = mustOK(map[string]any{"action": "dependencies", "module": "mod:ext:nope"})
	if !strings.Contains(out, "unknown or unused") {
		t.Errorf("module dependencies: %s", out)
	}
	out = mustOK(map[string]any{"action": "dependents", "file": "calc/calc.go"})
	if !strings.Contains(out, "calc_test.go") {
		t.Errorf("dependents: %s", out)
	}
	out = mustOK(map[string]any{"action": "impacted_tests", "changed": []string{"calc/calc.go"}})
	if !strings.Contains(out, "calc_test.go") {
		t.Errorf("impacted: %s", out)
	}
	out = mustOK(map[string]any{"action": "stats"})
	if !strings.Contains(out, "graph:") {
		t.Errorf("stats: %s", out)
	}
	// Requirements: linked implementers plus keyword candidates.
	if err := g.AttachRequirement("R7", SymbolID("calc/calc.go", "Add", 3)); err != nil {
		t.Fatal(err)
	}
	out = mustOK(map[string]any{"action": "implementers", "requirement": "R7"})
	if !strings.Contains(out, "calc/calc.go") {
		t.Errorf("implementers: %s", out)
	}
	// Errors are input errors, not tool failures.
	res := callTool(t, tool, map[string]any{"action": "callers", "symbol": "Nope"})
	if res.Status != tools.StatusInputError {
		t.Errorf("unknown symbol: %s %s", res.Status, res.Output)
	}
	res = callTool(t, tool, map[string]any{"action": "frobnicate"})
	if res.Status != tools.StatusInputError {
		t.Errorf("unknown action: %s %s", res.Status, res.Output)
	}
	res = callTool(t, tool, map[string]any{"action": "symbols"})
	if res.Status != tools.StatusInputError {
		t.Errorf("missing file: %s %s", res.Status, res.Output)
	}
	// Resource keys name project and file for leases (16.2).
	keys, err := tool.ResourceKeys(json.RawMessage(`{"project":"p","file":"f"}`))
	if err != nil || len(keys) != 2 {
		t.Errorf("keys: %v %v", keys, err)
	}
}

func TestLSPToolActions(t *testing.T) {
	root := lspTestProject(t)
	m := NewManager()
	defer m.Close()
	m.Start = func(cfg ServerConfig) (*Client, func(), error) {
		_, cc := startFake(t, cfg.RootDir)
		cl := NewPipeClient(cc, cfg.RootDir)
		return cl, func() { _ = cl.Close() }, nil
	}
	m.SetConfig(ServerConfig{Language: "go", Command: "fake-ls"})
	deps := &Deps{LSP: m, DefaultProject: root}
	tool := &LSPTool{Deps: deps}
	mustOK := func(args map[string]any) string {
		t.Helper()
		args["project"] = root
		res := callTool(t, tool, args)
		if res.Status != tools.StatusOK {
			t.Fatalf("%v -> %s: %s", args, res.Status, res.Output)
		}
		return res.Output
	}
	out := mustOK(map[string]any{"action": "definition", "file": "main.go", "line": 4, "character": 1})
	if !strings.Contains(out, "target.go:4:6") {
		t.Errorf("definition: %s", out)
	}
	out = mustOK(map[string]any{"action": "references", "file": "main.go", "line": 4, "character": 1})
	if !strings.Contains(out, "target.go") || !strings.Contains(out, "other.go") {
		t.Errorf("references: %s", out)
	}
	out = mustOK(map[string]any{"action": "symbols", "file": "target.go"})
	if !strings.Contains(out, "Target") || !strings.Contains(out, "helper") {
		t.Errorf("symbols: %s", out)
	}
	out = mustOK(map[string]any{"action": "workspace_symbols", "query": "Tar"})
	if !strings.Contains(out, "Target") {
		t.Errorf("workspace_symbols: %s", out)
	}
	// Diagnostics arrive as a push after didOpen; poll briefly.
	var diag tools.Result
	for i := 0; i < 50; i++ {
		diag = callTool(t, tool, map[string]any{"action": "diagnostics", "file": "main.go"})
		if diag.Status == tools.StatusOK && strings.Contains(diag.Output, "undefined") {
			break
		}
	}
	if diag.Status != tools.StatusOK || !strings.Contains(diag.Output, "undefined") {
		t.Fatalf("diagnostics: %+v", diag)
	}
	out = mustOK(map[string]any{"action": "rename", "file": "main.go", "line": 4, "character": 1, "new_name": "Renamed"})
	if !strings.Contains(out, "Renamed") || !strings.Contains(out, "apply with the file tools") {
		t.Errorf("rename: %s", out)
	}
	// Missing language with an unscorable file is an input error.
	res := callTool(t, tool, map[string]any{"action": "definition", "file": "x", "line": 1})
	if res.Status != tools.StatusInputError {
		t.Errorf("scoreless: %s %s", res.Status, res.Output)
	}
	res = callTool(t, tool, map[string]any{"action": "bogus"})
	if res.Status != tools.StatusInputError {
		t.Errorf("unknown action: %s %s", res.Status, res.Output)
	}
	res = callTool(t, tool, map[string]any{"action": "workspace_symbols"})
	if res.Status != tools.StatusInputError {
		t.Errorf("missing query: %s %s", res.Status, res.Output)
	}
	// No manager configured is a tool error, not an input error.
	bare := &LSPTool{Deps: &Deps{DefaultProject: root}}
	res = callTool(t, bare, map[string]any{"action": "symbols", "file": "main.go", "project": root})
	if res.Status != tools.StatusToolError {
		t.Errorf("no manager: %s %s", res.Status, res.Output)
	}
	// Manager diagnostics aggregate across live servers.
	found := ""
	for i := 0; i < 50 && found == ""; i++ {
		found = m.Diagnostics([]string{"main.go"})
	}
	if !strings.Contains(found, "undefined") {
		t.Errorf("manager diagnostics: %q", found)
	}
}
