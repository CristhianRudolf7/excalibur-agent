package codeintel

import (
	"path"
	"sort"
	"strings"
)

// ImpactHit is one selected test file with the reason it was chosen.
type ImpactHit struct {
	File string
	Why  string
}

// ImpactResult is the outcome of impacted-test selection.
type ImpactResult struct {
	// Hits lists selected test files, ordered by file.
	Hits []ImpactHit
	// Conservative is true when a changed path was unknown to the
	// graph and selection fell back to every test file.
	Conservative bool
}

// Files returns the selected test files, ordered.
func (r ImpactResult) Files() []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, h.File)
	}
	return out
}

// TestFiles returns every indexed test file, ordered: test files by
// path convention plus files holding test symbols. Section 18.2 uses
// this list before finalizing; ImpactedTests narrows it during work.
func (g *Graph) TestFiles() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	set := map[string]bool{}
	for rel, fr := range g.files {
		if IsTestPath(rel, fr.lang) {
			set[rel] = true
		}
	}
	for _, n := range g.nodes {
		if n.Kind == NodeSymbol && n.SubKind == SymbolTest && n.Path != "" {
			set[n.Path] = true
		}
	}
	out := make([]string, 0, len(set))
	for rel := range set {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// closureCap bounds reverse call-graph walks.
const closureCap = 5000

// ImpactedTests selects the tests affected by changing the given
// project-relative paths (Section 18.2, during-work selection):
//
//   - a changed test file selects itself;
//   - symbols defined in a changed file pull in their transitive
//     callers, and any test among them;
//   - tests edges (test-to-code) select their holders;
//   - test files importing a changed file's module are included;
//   - unknown or untracked paths select everything (conservative).
func (g *Graph) ImpactedTests(changed []string) ImpactResult {
	var res ImpactResult
	hit := map[string]string{}
	mark := func(file, why string) {
		if _, ok := hit[file]; !ok {
			hit[file] = why
		}
	}
	all := g.TestFiles()
	isTest := map[string]bool{}
	for _, t := range all {
		isTest[t] = true
	}
	for _, c := range changed {
		c = path.Clean(strings.TrimSpace(c))
		g.collectForChange(c, all, isTest, mark, &res)
	}
	for _, t := range all {
		if _, ok := hit[t]; ok {
			res.Hits = append(res.Hits, ImpactHit{File: t, Why: hit[t]})
		}
	}
	// Hits for files that are not in the test list cannot happen by
	// construction (mark only fires on test files), except the
	// conservative fallback which marks everything.
	sort.Slice(res.Hits, func(i, j int) bool { return res.Hits[i].File < res.Hits[j].File })
	return res
}

func (g *Graph) collectForChange(c string, all []string, isTest map[string]bool, mark func(string, string), res *ImpactResult) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	fr, known := g.files[c]
	if !known {
		for _, t := range all {
			mark(t, "unknown path "+c+": conservative")
		}
		res.Conservative = true
		return
	}
	_ = fr
	if isTest[c] {
		mark(c, "changed test")
	}
	// Symbols defined in the changed file.
	var syms []Node
	for _, e := range g.outEdgesLocked(FileID(c)) {
		if e.Kind != EdgeDefines {
			continue
		}
		if n, ok := g.nodes[e.Dst]; ok && n.Kind == NodeSymbol {
			syms = append(syms, n)
		}
	}
	// Transitive callers of those symbols. Inbound dynamic edges
	// (calls, references, tests) resolve live, so the walk always
	// sees the current project state.
	visited := map[string]bool{}
	queue := make([]string, 0, len(syms))
	for _, s := range syms {
		visited[s.ID] = true
		queue = append(queue, s.ID)
	}
	for len(queue) > 0 && len(visited) < closureCap {
		id := queue[0]
		queue = queue[1:]
		for _, e := range g.inEdgesLocked(id) {
			if e.Kind != EdgeCalls && e.Kind != EdgeReferences && e.Kind != EdgeTests {
				continue
			}
			src, ok := g.nodes[e.Src]
			if !ok || src.Kind != NodeSymbol {
				continue
			}
			if visited[src.ID] {
				continue
			}
			visited[src.ID] = true
			// The caller's own callers matter transitively.
			queue = append(queue, src.ID)
			if isTest[src.Path] && src.Path != "" {
				mark(src.Path, "calls "+nodeShort(id))
			}
		}
	}
	// Test files importing the changed file's module: the changed
	// file provides its directory module, so importers of it run
	// against the new code (covers table-driven and reflective
	// tests that never name a symbol directly).
	provided := ModuleID("dir:" + fr.lang + ":" + dirOf(c))
	for src, edges := range g.out {
		srcNode, ok := g.nodes[src]
		if !ok || srcNode.Kind != NodeFile {
			continue
		}
		if !isTest[srcNode.Path] || srcNode.Path == "" || srcNode.Path == c {
			continue
		}
		for _, e := range edges {
			if e.Kind == EdgeImports && e.Dst == provided {
				mark(srcNode.Path, "imports changed module")
				break
			}
		}
	}
}

func nodeShort(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func dirOf(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}

// isDirModule reports mod:dir:<lang>:<dir> module IDs.
func isDirModule(id string) bool {
	return strings.HasPrefix(id, "mod:dir:")
}

// moduleDir extracts the directory from a mod:dir:<lang>:<dir> ID.
func moduleDir(id string) string {
	rest := strings.TrimPrefix(id, "mod:dir:")
	if i := strings.Index(rest, ":"); i >= 0 {
		return rest[i+1:]
	}
	return rest
}
