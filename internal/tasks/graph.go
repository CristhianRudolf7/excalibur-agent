// Dependency graph algorithms (Sections 8.2, 8.5).
//
// The task graph stays acyclic: every dependency insertion runs a
// depth-first search from the destination and is rejected when it
// would close a cycle. Replanning reuses the same traversal to
// compute the affected subgraph: the descendants of the changed
// nodes plus the tasks linked to changed requirements.
package tasks

import "sort"

// FindCycle reports one dependency cycle, or nil when the graph is
// acyclic. Nodes are item ids; edges run from Dependency.From to
// Dependency.To. The search is iterative to stay safe on deep plans,
// and deterministic: neighbours visit in sorted order so the reported
// cycle is stable.
func FindCycle(nodes []string, deps []Dependency) []string {
	adj := adjacency(deps)
	sorted := append([]string(nil), nodes...)
	sort.Strings(sorted)
	const (
		white = 0 // unvisited
		gray  = 1 // on the current path
		black = 2 // done
	)
	color := make(map[string]int, len(nodes))
	var path []string
	var cycle []string
	var visit func(n string) bool
	visit = func(n string) bool {
		color[n] = gray
		path = append(path, n)
		for _, m := range adj[n] {
			switch color[m] {
			case gray:
				start := 0
				for path[start] != m {
					start++
				}
				cycle = append([]string(nil), path[start:]...)
				cycle = append(cycle, m)
				return true
			case white:
				if visit(m) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		color[n] = black
		return false
	}
	for _, n := range sorted {
		if color[n] == white {
			if visit(n) {
				return cycle
			}
		}
	}
	return nil
}

// WouldCycle reports whether adding from -> to would close a cycle:
// it checks for a path from to back to from in the current graph.
func WouldCycle(nodes []string, deps []Dependency, from, to string) bool {
	adj := adjacency(deps)
	// A self-loop is always a cycle.
	if from == to {
		return true
	}
	// Depth-first search from the destination back to the source.
	visited := map[string]bool{}
	stack := []string{to}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == from {
			return true
		}
		if visited[n] {
			continue
		}
		visited[n] = true
		stack = append(stack, adj[n]...)
	}
	_ = nodes
	return false
}

// Descendants returns every node reachable from any of roots
// following dependency edges, roots excluded, in sorted order.
func Descendants(deps []Dependency, roots []string) []string {
	adj := adjacency(deps)
	seen := map[string]bool{}
	for _, r := range roots {
		seen[r] = true
	}
	var out []string
	stack := append([]string(nil), roots...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, m := range adj[n] {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
				stack = append(stack, m)
			}
		}
	}
	sort.Strings(out)
	return out
}

// AffectedSubgraph computes the deterministic replan scope of Section
// 8.5: the changed nodes, their descendants in the dependency graph,
// plus the tasks linked to changed requirements.
func AffectedSubgraph(deps []Dependency, changed []string, taskReqs map[string][]string, changedReqs []string) []string {
	changedSet := map[string]bool{}
	for _, c := range changed {
		changedSet[c] = true
	}
	for _, r := range changedReqs {
		for task, reqs := range taskReqs {
			for _, q := range reqs {
				if q == r {
					changedSet[task] = true
				}
			}
		}
	}
	var roots []string
	for n := range changedSet {
		roots = append(roots, n)
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range append(append([]string(nil), roots...), Descendants(deps, roots)...) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func adjacency(deps []Dependency) map[string][]string {
	adj := map[string][]string{}
	for _, d := range deps {
		adj[d.From] = append(adj[d.From], d.To)
	}
	for n := range adj {
		sort.Strings(adj[n])
	}
	return adj
}
