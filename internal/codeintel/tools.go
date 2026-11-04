package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Extension identity of the code-intelligence tools (Section 14.12).
// They register through the same Registry as every other extension:
// there is no privileged path for built-ins.
const (
	// ExtensionID is the manifest id of this tool family.
	ExtensionID = "excalibur-codeintel"
	// ExtensionVersion tracks the manifest; bump on contract change.
	ExtensionVersion = "0.1.0"

	// CodeGraphToolName queries the code and document graph.
	CodeGraphToolName = "code_graph"
	// LSPToolName drives project language servers.
	LSPToolName = "lsp"
)

// Deps bundles what the deferred tools need. Open resolves the
// project graph for a root directory; DefaultProject fills in the
// project argument when callers omit it.
type Deps struct {
	Open           func(project string) (*Graph, error)
	LSP            *Manager
	DefaultProject string
}

func (d *Deps) graphFor(project string) (*Graph, error) {
	if project == "" {
		project = d.DefaultProject
	}
	if project == "" {
		return nil, fmt.Errorf("codeintel: no project given")
	}
	if d.Open == nil {
		return nil, fmt.Errorf("codeintel: no graph opener configured")
	}
	return d.Open(project)
}

func schemaFor(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

var codeGraphSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{
			"callers", "callees", "dependencies", "dependents",
			"symbols", "impacted_tests", "implementers", "stats",
		}},
		"project":     map[string]any{"type": "string", "minLength": 1},
		"symbol":      map[string]any{"type": "string"},
		"file":        map[string]any{"type": "string"},
		"module":      map[string]any{"type": "string"},
		"requirement": map[string]any{"type": "string"},
		"changed":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required":             []string{"action"},
	"additionalProperties": false,
})

var lspSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{
			"definition", "references", "symbols",
			"workspace_symbols", "diagnostics", "rename",
		}},
		"project":             map[string]any{"type": "string", "minLength": 1},
		"language":            map[string]any{"type": "string"},
		"file":                map[string]any{"type": "string"},
		"line":                map[string]any{"type": "integer", "minimum": 1},
		"character":           map[string]any{"type": "integer", "minimum": 0},
		"query":               map[string]any{"type": "string"},
		"new_name":            map[string]any{"type": "string"},
		"include_declaration": map[string]any{"type": "boolean"},
	},
	"required":             []string{"action"},
	"additionalProperties": false,
})

// CodeGraphTool queries the code and document graph: who calls a
// symbol, what a module depends on, which files implement a
// requirement, and which tests a change impacts (Sections 10.10,
// 18.2). Read-only and safe to retry.
type CodeGraphTool struct{ Deps *Deps }

// Definition implements tools.Tool.
func (t *CodeGraphTool) Definition() tools.Definition {
	return tools.Definition{
		Name: CodeGraphToolName,
		Description: "Query the code and document graph: find callers and callees of a symbol, " +
			"list module dependencies and dependents, list file symbols, select impacted tests, " +
			"find files implementing a requirement, report graph stats",
		InputSchema: codeGraphSchema,
		Effect:      types.EffectReadOnly, Idempotency: types.IdemIdempotent,
		Cancel:       types.CancelImmediate,
		Timeout:      60 * time.Second,
		OutputBudget: tools.DefaultOutputBudget,
		Platforms:    []string{"linux"},
		Profiles:     []string{"default", "delegate", "eval"},
		Group:        "code", Tags: []string{"code", "graph", "tests"},
		Keywords: []string{"callers", "callees", "dependencies", "dependents",
			"symbols", "impacted", "tests", "requirement", "graph"},
	}
}

// ResourceKeys implements tools.Tool.
func (t *CodeGraphTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Project string `json:"project"`
		File    string `json:"file"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var keys []string
	if a.Project != "" {
		keys = append(keys, "project:"+a.Project)
	}
	if a.File != "" {
		keys = append(keys, "file:"+a.File)
	}
	return keys, nil
}

// Invoke implements tools.Tool.
func (t *CodeGraphTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Action      string   `json:"action"`
		Project     string   `json:"project"`
		Symbol      string   `json:"symbol"`
		File        string   `json:"file"`
		Module      string   `json:"module"`
		Requirement string   `json:"requirement"`
		Changed     []string `json:"changed"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	g, err := t.Deps.graphFor(a.Project)
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if err := ctx.Err(); err != nil {
		return tools.Result{Status: tools.StatusCanceled, Output: err.Error()}, nil
	}
	var out string
	switch a.Action {
	case "callers":
		out, err = graphCallers(g, a.Symbol, a.File)
	case "callees":
		out, err = graphCallees(g, a.Symbol, a.File)
	case "dependencies":
		out, err = graphDependencies(g, a.File, a.Module)
	case "dependents":
		out, err = graphDependents(g, a.File, a.Module)
	case "symbols":
		out, err = graphSymbols(g, a.File)
	case "impacted_tests":
		out = graphImpacted(g, a.Changed)
	case "implementers":
		out = graphImplementers(g, a.Requirement)
	case "stats":
		out = graphStats(g)
	default:
		return tools.Result{Status: tools.StatusInputError,
			Output: fmt.Sprintf("unknown code_graph action %q", a.Action)}, nil
	}
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	return tools.Result{Status: tools.StatusOK, Output: out}, nil
}

// resolveSymbol maps a symbol argument (node ID or bare name, with an
// optional file disambiguator) to node IDs.
func resolveSymbol(g *Graph, symbol, file string) ([]Node, error) {
	if symbol == "" {
		return nil, fmt.Errorf("codeintel: action needs a symbol")
	}
	if n, ok := g.Node(symbol); ok && n.Kind == NodeSymbol {
		return []Node{n}, nil
	}
	ids := g.SymbolsNamed(symbol)
	var out []Node
	for _, id := range ids {
		n, ok := g.Node(id)
		if !ok {
			continue
		}
		if file != "" && n.Path != cleanToolPath(file) {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("codeintel: unknown symbol %q", symbol)
	}
	return out, nil
}

func formatLoc(n Node) string {
	if n.Path == "" {
		return n.ID
	}
	return fmt.Sprintf("%s:%d-%d %s", n.Path, n.Line, n.EndLine, n.Name)
}

func graphCallers(g *Graph, symbol, file string) (string, error) {
	syms, err := resolveSymbol(g, symbol, file)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, s := range syms {
		fmt.Fprintf(&b, "callers of %s:\n", formatLoc(s))
		edges := g.InEdges(s.ID)
		shown := 0
		for _, e := range edges {
			if e.Kind != EdgeCalls && e.Kind != EdgeReferences && e.Kind != EdgeTests {
				continue
			}
			src, ok := g.Node(e.Src)
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "  [%s] %s\n", e.Kind, formatLoc(src))
			shown++
			if shown >= 100 {
				b.WriteString("  (bounded to 100; refine the query)\n")
				break
			}
		}
		if shown == 0 {
			b.WriteString("  (none)\n")
		}
	}
	return b.String(), nil
}

func graphCallees(g *Graph, symbol, file string) (string, error) {
	syms, err := resolveSymbol(g, symbol, file)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, s := range syms {
		fmt.Fprintf(&b, "callees of %s:\n", formatLoc(s))
		shown := 0
		for _, e := range g.OutEdges(s.ID) {
			if e.Kind != EdgeCalls && e.Kind != EdgeReferences && e.Kind != EdgeTests {
				continue
			}
			dst, ok := g.Node(e.Dst)
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "  [%s] %s\n", e.Kind, formatLoc(dst))
			shown++
			if shown >= 100 {
				b.WriteString("  (bounded to 100; refine the query)\n")
				break
			}
		}
		if shown == 0 {
			b.WriteString("  (none)\n")
		}
	}
	return b.String(), nil
}

func graphDependencies(g *Graph, file, module string) (string, error) {
	var b strings.Builder
	if module != "" {
		return moduleDependencies(g, module), nil
	}
	if file == "" {
		return "", fmt.Errorf("codeintel: dependencies needs a file or module")
	}
	file = cleanToolPath(file)
	b.WriteString("dependencies of " + file + ":\n")
	mods := map[string]bool{}
	for _, e := range g.OutEdges(FileID(file)) {
		if e.Kind != EdgeImports {
			continue
		}
		mods[e.Dst] = true
	}
	if len(mods) == 0 {
		b.WriteString("  (none)\n")
		return b.String(), nil
	}
	names := make([]string, 0, len(mods))
	for m := range mods {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, m := range names {
		fmt.Fprintf(&b, "  %s\n", modDisplay(g, m))
	}
	return b.String(), nil
}

func modDisplay(g *Graph, modID string) string {
	if n, ok := g.Node(modID); ok && n.Name != "" {
		return fmt.Sprintf("%s (%s)", modID, n.Name)
	}
	return modID
}

func moduleDependencies(g *Graph, module string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dependencies of module %s:\n", module)
	// Files provided by the module: every file under a dir module.
	var provided []string
	if isDirModule(module) {
		dir := moduleDir(module)
		for _, rel := range g.IndexedFiles() {
			if dirOf(rel) == dir {
				provided = append(provided, rel)
			}
		}
	}
	mods := map[string]bool{}
	if len(provided) == 0 {
		// External or empty module: report importers instead of
		// pretending to list file dependencies.
		for _, rel := range g.IndexedFiles() {
			for _, e := range g.OutEdges(FileID(rel)) {
				if e.Kind == EdgeImports && e.Dst == module {
					mods[rel] = true
				}
			}
		}
		if len(mods) == 0 {
			b.WriteString("  (unknown or unused module)\n")
			return b.String()
		}
		b.WriteString("  imported by:\n")
		names := make([]string, 0, len(mods))
		for m := range mods {
			names = append(names, m)
		}
		sort.Strings(names)
		for _, m := range names {
			fmt.Fprintf(&b, "  %s\n", m)
		}
		return b.String()
	}
	for _, rel := range provided {
		for _, e := range g.OutEdges(FileID(rel)) {
			if e.Kind == EdgeImports {
				mods[e.Dst] = true
			}
		}
	}
	delete(mods, module)
	if len(mods) == 0 {
		b.WriteString("  (none)\n")
		return b.String()
	}
	names := make([]string, 0, len(mods))
	for m := range mods {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, m := range names {
		fmt.Fprintf(&b, "  %s\n", modDisplay(g, m))
	}
	return b.String()
}

func graphDependents(g *Graph, file, module string) (string, error) {
	mods := map[string]bool{}
	if module != "" {
		mods[module] = true
	} else {
		if file == "" {
			return "", fmt.Errorf("codeintel: dependents needs a file or module")
		}
		file = cleanToolPath(file)
		// The file provides its directory module; importers of it
		// depend on the file.
		mods[providedModuleFor(g, file)] = true
	}
	var b strings.Builder
	if module != "" {
		fmt.Fprintf(&b, "dependents of %s:\n", module)
	} else {
		fmt.Fprintf(&b, "dependents of %s:\n", file)
	}
	set := map[string]string{}
	for _, rel := range g.IndexedFiles() {
		for _, e := range g.OutEdges(FileID(rel)) {
			if e.Kind != EdgeImports || !mods[e.Dst] {
				continue
			}
			set[rel] = "imports module"
		}
	}
	if file != "" {
		// Files whose symbols call into the file's symbols, and
		// tests selecting it.
		res := g.ImpactedTests([]string{file})
		for _, h := range res.Hits {
			if _, ok := set[h.File]; !ok {
				set[h.File] = h.Why
			}
		}
	}
	if len(set) == 0 {
		b.WriteString("  (none)\n")
		return b.String(), nil
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	shown := 0
	for _, n := range names {
		fmt.Fprintf(&b, "  %s (%s)\n", n, set[n])
		shown++
		if shown >= 100 {
			b.WriteString("  (bounded to 100; refine the query)\n")
			break
		}
	}
	return b.String(), nil
}

// providedModuleFor returns the directory module ID of a file: the
// first known dir-module for its directory, else a dangling
// unknown-language module (queries over it return empty).
func providedModuleFor(g *Graph, file string) string {
	dir := dirOf(file)
	for _, id := range moduleIDs(g) {
		if isDirModule(id) && moduleDir(id) == dir {
			return id
		}
	}
	return ModuleID("dir:unknown:" + dir)
}

func moduleIDs(g *Graph) []string {
	var out []string
	seen := map[string]bool{}
	for _, rel := range g.IndexedFiles() {
		for _, e := range g.OutEdges(FileID(rel)) {
			if e.Kind != EdgeImports || seen[e.Dst] {
				continue
			}
			seen[e.Dst] = true
			out = append(out, e.Dst)
		}
	}
	return out
}

func graphSymbols(g *Graph, file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("codeintel: symbols needs a file")
	}
	file = cleanToolPath(file)
	syms := g.FileSymbols(file)
	var b strings.Builder
	fmt.Fprintf(&b, "symbols of %s (%d):\n", file, len(syms))
	for _, s := range syms {
		fmt.Fprintf(&b, "  %s:%d-%d [%s]\n", s.Name, s.Line, s.EndLine, s.SubKind)
	}
	return b.String(), nil
}

func graphImpacted(g *Graph, changed []string) string {
	if len(changed) == 0 {
		return "no changed files given\n"
	}
	res := g.ImpactedTests(changed)
	var b strings.Builder
	fmt.Fprintf(&b, "impacted tests (%d):\n", len(res.Hits))
	for _, h := range res.Hits {
		fmt.Fprintf(&b, "  %s (%s)\n", h.File, h.Why)
	}
	if res.Conservative {
		b.WriteString("  (conservative: an unknown path selected every test)\n")
	}
	return b.String()
}

func graphImplementers(g *Graph, requirement string) string {
	var b strings.Builder
	if requirement == "" {
		return "implementers needs a requirement id or keyword\n"
	}
	impls := g.Implementers(requirement)
	fmt.Fprintf(&b, "implementers of %q (%d linked):\n", requirement, len(impls))
	for _, n := range impls {
		fmt.Fprintf(&b, "  %s\n", formatLoc(n))
	}
	// Keyword fallback: unlinked candidates whose symbol or section
	// names mention the requirement text.
	kw := strings.ToLower(requirement)
	cands := map[string]Node{}
	g.mu.RLock()
	for _, n := range g.nodes {
		if n.Kind != NodeSymbol && n.Kind != NodeSection {
			continue
		}
		if strings.Contains(strings.ToLower(n.Name), kw) {
			cands[n.ID] = n
		}
	}
	g.mu.RUnlock()
	for _, e := range impls {
		delete(cands, e.ID)
	}
	if len(cands) > 0 {
		b.WriteString("candidates (name match, unlinked):\n")
		ids := make([]string, 0, len(cands))
		for id := range cands {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for i, id := range ids {
			if i >= 20 {
				b.WriteString("  (bounded to 20 candidates)\n")
				break
			}
			fmt.Fprintf(&b, "  %s\n", formatLoc(cands[id]))
		}
	}
	return b.String()
}

func graphStats(g *Graph) string {
	st := g.Stats()
	return fmt.Sprintf(
		"graph: %d files (%d file nodes), %d modules, %d symbols (%d tests), %d documents, %d sections, %d static edges\n",
		st.Files, st.FileNodes, st.Modules, st.Symbols, st.TestSymbols,
		st.Documents, st.Sections, st.Edges)
}

// LSPTool drives project language servers: definition, references,
// document and workspace symbols, diagnostics and rename (Section
// 13.8). Rename reports the workspace edit; the agent applies it with
// the file tools so history and leases stay consistent.
type LSPTool struct{ Deps *Deps }

// Definition implements tools.Tool.
func (t *LSPTool) Definition() tools.Definition {
	return tools.Definition{
		Name: LSPToolName,
		Description: "Query project language servers: find symbol definition, list references, " +
			"list document and workspace symbols, show published diagnostics, compute a rename edit",
		InputSchema: lspSchema,
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemKeyed,
		Cancel:       types.CancelCooperative,
		Timeout:      60 * time.Second,
		OutputBudget: tools.DefaultOutputBudget,
		Platforms:    []string{"linux"},
		Profiles:     []string{"default", "delegate", "eval"},
		Group:        "code", Tags: []string{"code", "lsp", "language"},
		Keywords: []string{"definition", "references", "symbols",
			"diagnostics", "rename", "language", "server"},
	}
}

// ResourceKeys implements tools.Tool.
func (t *LSPTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Project string `json:"project"`
		File    string `json:"file"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var keys []string
	if a.Project != "" {
		keys = append(keys, "project:"+a.Project)
	}
	if a.File != "" {
		keys = append(keys, "file:"+a.File)
	}
	return keys, nil
}

// lspArgs is the lsp tool input shape.
type lspArgs struct {
	Action             string `json:"action"`
	Project            string `json:"project"`
	Language           string `json:"language"`
	File               string `json:"file"`
	Line               int    `json:"line"`
	Character          int    `json:"character"`
	Query              string `json:"query"`
	NewName            string `json:"new_name"`
	IncludeDeclaration bool   `json:"include_declaration"`
}

// Invoke implements tools.Tool.
func (t *LSPTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a lspArgs
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if t.Deps.LSP == nil {
		return tools.Result{Status: tools.StatusToolError,
			Output: "codeintel: no lsp manager configured"}, nil
	}
	project := a.Project
	if project == "" {
		project = t.Deps.DefaultProject
	}
	if project == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "lsp: no project given"}, nil
	}
	if err := ctx.Err(); err != nil {
		return tools.Result{Status: tools.StatusCanceled, Output: err.Error()}, nil
	}
	var out string
	var err error
	switch a.Action {
	case "definition", "references", "symbols", "diagnostics", "rename":
		out, err = t.positionAction(ctx, a, project)
	case "workspace_symbols":
		out, err = t.workspaceSymbols(ctx, a, project)
	default:
		return tools.Result{Status: tools.StatusInputError,
			Output: fmt.Sprintf("unknown lsp action %q", a.Action)}, nil
	}
	if err != nil {
		if isInputError(err) {
			return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
		}
		return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
	}
	return tools.Result{Status: tools.StatusOK, Output: out}, nil
}

func isInputError(err error) bool {
	return strings.HasPrefix(err.Error(), "codeintel: no lsp server") ||
		strings.Contains(err.Error(), "needs a file") ||
		strings.Contains(err.Error(), "needs a language") ||
		strings.Contains(err.Error(), "needs a query") ||
		strings.Contains(err.Error(), "needs a new name") ||
		strings.Contains(err.Error(), "needs a 1-based line")
}

func (t *LSPTool) clientFor(ctx context.Context, project, lang, file string) (*Client, string, error) {
	if lang == "" && file != "" {
		if l, ok := DetectLanguage(cleanToolPath(file)); ok && !IsDocLang(l) {
			lang = l
		}
	}
	if lang == "" {
		return nil, "", fmt.Errorf("codeintel: lsp action needs a language or a code file")
	}
	cl, err := t.Deps.LSP.For(ctx, project, lang)
	if err != nil {
		return nil, "", err
	}
	return cl, lang, nil
}

func (t *LSPTool) positionAction(ctx context.Context, a lspArgs, project string) (string, error) {
	cl, lang, err := t.clientFor(ctx, project, a.Language, a.File)
	if err != nil {
		return "", err
	}
	_ = lang
	switch a.Action {
	case "diagnostics":
		if a.File == "" {
			// Whole-project cached diagnostics are not kept; the
			// caller names the files it touched.
			return "", fmt.Errorf("codeintel: diagnostics needs a file")
		}
		if _, err := cl.EnsureOpen(ctx, project, cleanToolPath(a.File)); err != nil {
			return "", err
		}
		out := cl.Diagnostics([]string{cleanToolPath(a.File)})
		if out == "" {
			return "(no diagnostics)\n", nil
		}
		return out, nil
	case "symbols":
		if a.File == "" {
			return "", fmt.Errorf("codeintel: symbols needs a file")
		}
		syms, err := cl.DocumentSymbols(ctx, project, cleanToolPath(a.File))
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "symbols of %s (%d):\n", a.File, len(syms))
		writeDocSyms(&b, syms, "  ")
		return b.String(), nil
	case "definition", "references", "rename":
		if a.File == "" {
			return "", fmt.Errorf("codeintel: %s needs a file", a.Action)
		}
		if a.Line < 1 {
			return "", fmt.Errorf("codeintel: %s needs a 1-based line", a.Action)
		}
		line, ch := a.Line-1, a.Character
		switch a.Action {
		case "definition":
			locs, err := cl.Definition(ctx, project, cleanToolPath(a.File), line, ch)
			if err != nil {
				return "", err
			}
			return formatLocations(project, locs), nil
		case "references":
			locs, err := cl.References(ctx, project, cleanToolPath(a.File), line, ch, a.IncludeDeclaration)
			if err != nil {
				return "", err
			}
			return formatLocations(project, locs), nil
		default:
			if a.NewName == "" {
				return "", fmt.Errorf("codeintel: rename needs a new name")
			}
			edit, err := cl.Rename(ctx, project, cleanToolPath(a.File), line, ch, a.NewName)
			if err != nil {
				return "", err
			}
			return formatEdit(edit), nil
		}
	}
	return "", fmt.Errorf("codeintel: unknown lsp action %q", a.Action)
}

func (t *LSPTool) workspaceSymbols(ctx context.Context, a lspArgs, project string) (string, error) {
	if a.Query == "" {
		return "", fmt.Errorf("codeintel: workspace_symbols needs a query")
	}
	// Without a language or file, search every configured server so
	// the caller need not know which language owns the symbol.
	var langs []string
	if a.Language != "" {
		langs = []string{a.Language}
	} else if a.File != "" {
		if l, ok := DetectLanguage(cleanToolPath(a.File)); ok && !IsDocLang(l) {
			langs = []string{l}
		} else {
			return "", fmt.Errorf("codeintel: workspace_symbols needs a language or a code file")
		}
	} else {
		langs = t.Deps.LSP.Configured()
		if len(langs) == 0 {
			return "", fmt.Errorf("codeintel: workspace_symbols needs a language or a code file")
		}
	}
	type hit struct {
		lang string
		sym  SymbolInformation
	}
	var all []hit
	for _, lang := range langs {
		cl, err := t.Deps.LSP.For(ctx, project, lang)
		if err != nil {
			return "", err
		}
		syms, err := cl.WorkspaceSymbols(ctx, a.Query)
		if err != nil {
			return "", err
		}
		for _, s := range syms {
			all = append(all, hit{lang: lang, sym: s})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].sym.Name != all[j].sym.Name {
			return all[i].sym.Name < all[j].sym.Name
		}
		return all[i].lang < all[j].lang
	})
	var b strings.Builder
	fmt.Fprintf(&b, "workspace symbols for %q (%d):\n", a.Query, len(all))
	for i, h := range all {
		if i >= 50 {
			b.WriteString("  (bounded to 50; refine the query)\n")
			break
		}
		fmt.Fprintf(&b, "  %s [%s] (%s)\n", h.sym.Name, uriToRel(project, h.sym.Location.URI), h.lang)
	}
	return b.String(), nil
}

func writeDocSyms(b *strings.Builder, syms []DocumentSymbol, indent string) {
	for _, s := range syms {
		fmt.Fprintf(b, "%s%s:%d-%d\n", indent, s.Name,
			s.Range.Start.Line+1, s.Range.End.Line+1)
		writeDocSyms(b, s.Children, indent+"  ")
	}
}

func formatLocations(project string, locs []Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "locations (%d):\n", len(locs))
	for i, l := range locs {
		if i >= 50 {
			b.WriteString("  (bounded to 50)\n")
			break
		}
		fmt.Fprintf(&b, "  %s:%d:%d\n", uriToRel(project, l.URI),
			l.Range.Start.Line+1, l.Range.Start.Character+1)
	}
	if len(locs) == 0 {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

func formatEdit(edit WorkspaceEdit) string {
	var b strings.Builder
	files := make([]string, 0, len(edit.Changes))
	total := 0
	for f, edits := range edit.Changes {
		files = append(files, f)
		total += len(edits)
	}
	sort.Strings(files)
	fmt.Fprintf(&b, "rename edit: %d edit(s) in %d file(s) (apply with the file tools):\n", total, len(files))
	for _, f := range files {
		for _, e := range edit.Changes[f] {
			fmt.Fprintf(&b, "  %s:%d:%d-%d:%d -> %q\n", f,
				e.Range.Start.Line+1, e.Range.Start.Character+1,
				e.Range.End.Line+1, e.Range.End.Character+1, e.NewText)
		}
	}
	return b.String()
}

// Register adds the lsp and code_graph deferred tools to the shared
// catalog (Section 14.12): the same Registry interface built-ins use,
// with the same validation. Names stay unique catalog-wide.
func Register(reg tools.Registry, deps *Deps) error {
	if deps == nil {
		deps = &Deps{}
	}
	if err := reg.Register(&CodeGraphTool{Deps: deps}); err != nil {
		return err
	}
	if err := reg.Register(&LSPTool{Deps: deps}); err != nil {
		return err
	}
	return nil
}

// Manifest returns the excalibur-extension.yaml content of this tool
// family (Section 14.12, kind builtin, lazy lifecycle): the same
// contract every extension path validates against.
func Manifest() tools.Manifest {
	return tools.Manifest{
		ID: ExtensionID, Version: ExtensionVersion,
		Contract: tools.ExtensionContract, Kind: tools.ExtensionBuiltin,
		Lifecycle: tools.LifecycleLazy,
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
		Tools: []tools.ExtensionTool{
			{Definition: (&CodeGraphTool{Deps: &Deps{}}).Definition()},
			{Definition: (&LSPTool{Deps: &Deps{}}).Definition()},
		},
	}
}
