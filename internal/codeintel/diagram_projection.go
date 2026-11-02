package codeintel

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Diagram projections retain graph evidence only. Name-resolved call edges have
// the same conservative (potential target) meaning as code_graph, not runtime traces.
type diagramNode struct{ ID, Label, Group string }
type diagramEdge struct {
	Src, Dst, Kind string
}
type diagramProjection struct {
	Nodes     map[string]diagramNode
	Edges     map[diagramEdge]int
	Collapsed int
	Entry     string
}

func newDiagramProjection() diagramProjection {
	return diagramProjection{Nodes: map[string]diagramNode{}, Edges: map[diagramEdge]int{}}
}
func (p *diagramProjection) node(id, label, group string) string {
	p.Nodes[id] = diagramNode{id, label, group}
	return id
}
func (p *diagramProjection) edge(src, dst, kind string, weight int) {
	if src != dst && src != "" && dst != "" {
		p.Edges[diagramEdge{Src: src, Dst: dst, Kind: kind}] += weight
	}
}
func diagramContains(dir, file string) bool {
	return dir == "" || dir == "." || file == dir || strings.HasPrefix(file, dir+"/")
}
func diagramTop(file string) string {
	if i := strings.IndexByte(file, '/'); i >= 0 {
		return file[:i]
	}
	return "."
}

// diagramFocus accepts a graph ID, a Go import path, or a relative directory.
func diagramFocus(focus, modulePrefix string) string {
	if strings.HasPrefix(focus, "mod:dir:") {
		return moduleDir(focus)
	}
	if modulePrefix != "" {
		if focus == modulePrefix {
			return ""
		}
		focus = strings.TrimPrefix(focus, modulePrefix+"/")
	}
	return strings.TrimPrefix(path.Clean(focus), "./")
}

func projectDiagram(ctx context.Context, g *Graph, a diagramArgs, modulePrefix string) (diagramProjection, error) {
	p := newDiagramProjection()
	files := map[string]bool{}
	for _, f := range g.IndexedFiles() {
		lang, ok := DetectLanguage(f)
		if ok && !IsDocLang(lang) && (a.IncludeTests || !IsTestPath(f, lang)) {
			files[f] = true
		}
	}
	focus := diagramFocus(a.Focus, modulePrefix)
	if a.Level == "overview" {
		focus = ""
	}
	if a.Level == "overview" || a.Level == "module" {
		for _, f := range sortedDiagramFiles(files) {
			if err := ctx.Err(); err != nil {
				return p, err
			}
			if !diagramContains(focus, f) {
				continue
			}
			src := f
			if a.Level == "overview" {
				src = diagramOverviewModule(f, a.scope)
				p.node(src, src, "repository")
			} else {
				p.node(src, f, path.Dir(f))
			}
			seen := map[string]bool{}
			for _, e := range g.OutEdges(FileID(f)) {
				if e.Kind != EdgeImports {
					continue
				}
				n, ok := g.Node(e.Dst)
				if !ok {
					continue
				}
				target, internal := diagramImportTarget(f, n, files)
				dst := ""
				if internal {
					if a.Level == "overview" {
						dst = diagramOverviewModule(path.Join(target, "_"), a.scope)
						p.node(dst, dst, "repository")
					} else {
						dst = "pkg:" + target
						label := target
						if label == "" {
							label = "."
						}
						p.node(dst, label, "packages")
					}
				} else {
					lib := diagramLibrary(n)
					dst = "ext:" + lib
					p.node(dst, lib, "external dependencies")
				}
				// Multiple syntactic candidates for one import represent one relation.
				if !seen[dst] {
					p.edge(src, dst, EdgeImports, 1)
					seen[dst] = true
				}
			}
		}
	} else {
		symbols := map[string]Node{}
		for _, f := range sortedDiagramFiles(files) {
			for _, n := range g.FileSymbols(f) {
				if a.IncludeTests || n.SubKind != SymbolTest {
					symbols[n.ID] = n
				}
			}
		}
		selected := map[string]bool{}
		if a.Level == "flow" {
			var entries []string
			for id, n := range symbols {
				if id == a.Focus || n.Name == a.Focus {
					entries = append(entries, id)
				}
			}
			sort.Strings(entries)
			if len(entries) != 1 {
				return p, fmt.Errorf("focus %q resolved to %d entry functions; use an exact symbol ID", a.Focus, len(entries))
			}
			p.Entry = entries[0]
			if n := symbols[p.Entry]; n.SubKind != SymbolFunc && n.SubKind != SymbolMethod && n.SubKind != SymbolTest {
				return p, fmt.Errorf("focus must be a function or method")
			}
			selected[p.Entry] = true
			frontier := []string{p.Entry}
			for depth := 0; depth < a.Depth && len(frontier) > 0; depth++ {
				var next []string
				for _, id := range frontier {
					if err := ctx.Err(); err != nil {
						return p, err
					}
					for _, e := range g.OutEdges(id) {
						if _, ok := symbols[e.Dst]; e.Kind != EdgeCalls || !ok {
							continue
						}
						p.edge(id, e.Dst, e.Kind, 1)
						if !selected[e.Dst] {
							selected[e.Dst] = true
							next = append(next, e.Dst)
						}
					}
				}
				sort.Strings(next)
				frontier = next
			}
		} else {
			focusedSymbols := []string{}
			for id, n := range symbols {
				if diagramContains(focus, n.Path) || n.Name == a.Focus || id == a.Focus {
					selected[id] = true
					if n.Name == a.Focus || id == a.Focus {
						focusedSymbols = append(focusedSymbols, id)
					}
				}
			}
			// A symbol focus includes its immediate graph neighborhood, so a
			// function or type does not become an isolated definition box.
			for _, id := range focusedSymbols {
				if err := ctx.Err(); err != nil {
					return p, err
				}
				for _, e := range append(g.OutEdges(id), g.InEdges(id)...) {
					if e.Kind != EdgeCalls && e.Kind != EdgeImplements {
						continue
					}
					for _, endpoint := range []string{e.Src, e.Dst} {
						if _, ok := symbols[endpoint]; ok {
							selected[endpoint] = true
						}
					}
				}
			}
			for id := range selected {
				if err := ctx.Err(); err != nil {
					return p, err
				}
				n := symbols[id]
				fid := FileID(n.Path)
				p.node(fid, n.Path, path.Dir(n.Path))
				p.edge(fid, id, EdgeDefines, 1)
				for _, e := range g.OutEdges(id) {
					if selected[e.Dst] && (e.Kind == EdgeCalls || e.Kind == EdgeImplements || (a.IncludeTests && e.Kind == EdgeTests)) {
						p.edge(id, e.Dst, e.Kind, 1)
					}
				}
			}
		}
		for id := range selected {
			n := symbols[id]
			p.node(id, n.Name+" ("+n.Path+":"+strconv.Itoa(n.Line)+")", path.Dir(n.Path))
		}
	}
	if len(p.Nodes) == 0 {
		return p, fmt.Errorf("no code found for %s focus %q", a.Level, a.Focus)
	}
	return p, nil
}

func sortedDiagramFiles(files map[string]bool) []string {
	out := make([]string, 0, len(files))
	for f := range files {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func diagramImportTarget(file string, n Node, files map[string]bool) (string, bool) {
	// Resolve only against indexed paths. Unresolved/dangling graph modules stay
	// external; no filesystem guesses or model-generated dependency edges.
	if strings.HasPrefix(n.ID, "mod:dir:") {
		dir := moduleDir(n.ID)
		for f := range files {
			if path.Dir(f) == dir || (dir == "" && path.Dir(f) == ".") {
				return dir, true
			}
		}
	}
	// Python's bare local imports and JS extensionless imports may denote files.
	raw := n.Name
	var target string
	if strings.HasPrefix(raw, ".") {
		target = path.Clean(path.Join(path.Dir(file), raw))
	} else {
		target = strings.ReplaceAll(raw, ".", "/")
	}
	for _, f := range sortedDiagramFiles(files) {
		stem := strings.TrimSuffix(f, path.Ext(f))
		if f == target || stem == target || path.Dir(f) == target {
			dir := path.Dir(f)
			if dir == "." {
				dir = ""
			}
			return dir, true
		}
	}
	return "", false
}
func diagramLibrary(n Node) string {
	raw := n.Name
	if raw == "" {
		raw = strings.TrimPrefix(strings.TrimPrefix(n.ID, "mod:go:"), "mod:ext:")
	}
	parts := strings.Split(raw, "/")
	if strings.HasPrefix(n.ID, "mod:go:") && len(parts) > 2 && strings.Contains(parts[0], ".") {
		return strings.Join(parts[:3], "/")
	}
	if strings.HasPrefix(raw, "@") && len(parts) > 1 {
		return strings.Join(parts[:2], "/")
	}
	if strings.HasPrefix(n.ID, "mod:go:") && len(parts) > 0 {
		return parts[0]
	}
	return strings.Split(parts[0], ".")[0]
}

func (p diagramProjection) ranked() []string {
	degree := map[string]int{}
	for e, w := range p.Edges {
		degree[e.Src] += w
		degree[e.Dst] += w
	}
	ids := make([]string, 0, len(p.Nodes))
	for id := range p.Nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i] == p.Entry {
			return true
		}
		if ids[j] == p.Entry {
			return false
		}
		if degree[ids[i]] != degree[ids[j]] {
			return degree[ids[i]] > degree[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (p diagramProjection) bounded(max int) diagramProjection {
	if len(p.Nodes) <= max {
		return p
	}
	q := newDiagramProjection()
	q.Entry = p.Entry
	ids := p.ranked()
	for _, id := range ids[:max-1] {
		q.Nodes[id] = p.Nodes[id]
	}
	q.Collapsed = len(p.Nodes) - len(q.Nodes)
	const rest = "collapsed:remainder"
	q.node(rest, fmt.Sprintf("+%d more", q.Collapsed), "collapsed")
	for e, w := range p.Edges {
		src, dst := e.Src, e.Dst
		if _, ok := q.Nodes[src]; !ok {
			src = rest
		}
		if _, ok := q.Nodes[dst]; !ok {
			dst = rest
		}
		q.edge(src, dst, e.Kind, w)
	}
	return q
}

func (p diagramProjection) source(format, direction string) string {
	ids := make([]string, 0, len(p.Nodes))
	groups := map[string][]string{}
	for id, n := range p.Nodes {
		ids = append(ids, id)
		groups[n.Group] = append(groups[n.Group], id)
	}
	sort.Strings(ids)
	aliases := map[string]string{}
	for i, id := range ids {
		aliases[id] = fmt.Sprintf("n%d", i)
	}
	groupNames := make([]string, 0, len(groups))
	for group := range groups {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)
	var b strings.Builder
	if format == "graphviz" {
		fmt.Fprintf(&b, "digraph code {\n  rankdir=%s;\n", direction)
	} else {
		fmt.Fprintf(&b, "flowchart %s\n", direction)
	}
	quote := func(s string) string {
		if format == "mermaid" {
			s = strings.ReplaceAll(s, "&", "&amp;")
			s = strings.ReplaceAll(s, "\"", "&quot;")
			s = strings.ReplaceAll(s, "<", "&lt;")
			s = strings.ReplaceAll(s, ">", "&gt;")
			s = strings.ReplaceAll(s, "\n", " ")
			return "\"" + s + "\""
		}
		return strconv.Quote(s)
	}
	for i, group := range groupNames {
		if format == "graphviz" {
			fmt.Fprintf(&b, "  subgraph cluster_%d {\n    label=%s;\n", i, quote(group))
		} else {
			fmt.Fprintf(&b, "  subgraph g%d[%s]\n", i, quote(group))
		}
		sort.Strings(groups[group])
		for _, id := range groups[group] {
			if format == "graphviz" {
				fmt.Fprintf(&b, "    %s [label=%s];\n", aliases[id], quote(p.Nodes[id].Label))
			} else {
				fmt.Fprintf(&b, "    %s[%s]\n", aliases[id], quote(p.Nodes[id].Label))
			}
		}
		if format == "graphviz" {
			b.WriteString("  }\n")
		} else {
			b.WriteString("  end\n")
		}
	}
	edges := make([]diagramEdge, 0, len(p.Edges))
	for e := range p.Edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Src != b.Src {
			return a.Src < b.Src
		}
		if a.Dst != b.Dst {
			return a.Dst < b.Dst
		}
		return a.Kind < b.Kind
	})
	for _, e := range edges {
		label := e.Kind
		if w := p.Edges[e]; w > 1 {
			label += fmt.Sprintf(" (%d)", w)
		}
		if format == "graphviz" {
			fmt.Fprintf(&b, "  %s -> %s [label=%s];\n", aliases[e.Src], aliases[e.Dst], quote(label))
		} else {
			fmt.Fprintf(&b, "  %s -->|%s| %s\n", aliases[e.Src], label, aliases[e.Dst])
		}
	}
	if format == "graphviz" {
		b.WriteString("}\n")
	}
	return b.String()
}

func diagramOverviewModule(file, scope string) string {
	if scope == "" || scope == "." {
		return diagramTop(file)
	}
	rel := strings.TrimPrefix(file, scope+"/")
	return path.Join(scope, diagramTop(rel))
}
