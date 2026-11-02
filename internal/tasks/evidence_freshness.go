package tasks

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// DependencyFreshnessEnabled replaces the global mutation frontier with
// per-check dependency versions. Recovered old histories use path overlap.
func DependencyFreshnessEnabled() bool { return envBool("EXCALIBUR_EVIDENCE_DEPENDENCIES", true) }

var dependencyPathRe = regexp.MustCompile(`[a-zA-Z0-9_./-]+\.[a-zA-Z0-9_.-]+|/[a-zA-Z0-9_./-]+`)

func argPath(h HistoryTool) string {
	var a map[string]string
	_ = json.Unmarshal([]byte(h.Args), &a)
	for _, k := range []string{"path", "file_path", "filename", "file"} {
		if a[k] != "" {
			return filepath.Clean(a[k])
		}
	}
	return ""
}

func overlapPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	// Old receipts may omit workdir. Avoid treating two absolute paths with
	// equal basenames as the same file.
	return (!filepath.IsAbs(a) || !filepath.IsAbs(b)) && filepath.Base(a) == filepath.Base(b)
}

func mutationPaths(h HistoryTool) (paths []string, unknown bool) {
	if writeTools[h.Name] {
		p := argPath(h)
		if p == "" {
			return nil, true
		}
		return []string{p}, false
	}
	command, ok := historyCommand(h.Args)
	if h.Name != "exec" || !ok || !commandMutatesArtifacts(command) {
		return nil, false
	}
	tree, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, true
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Redirect:
			if n.Word != nil && (n.Op == syntax.RdrOut || n.Op == syntax.AppOut || n.Op == syntax.RdrAll || n.Op == syntax.AppAll) {
				if p := n.Word.Lit(); p != "" && p != "/dev/null" {
					paths = append(paths, p)
				}
			}
		case *syntax.CallExpr:
			if len(n.Args) > 1 {
				switch filepath.Base(n.Args[0].Lit()) {
				case "touch", "rm", "mkdir", "tee":
					for _, w := range n.Args[1:] {
						if p := w.Lit(); p != "" && !strings.HasPrefix(p, "-") {
							paths = append(paths, p)
						}
					}
				case "cp", "mv", "install":
					if p := n.Args[len(n.Args)-1].Lit(); p != "" {
						paths = append(paths, p)
					}
				}
			}
		}
		return true
	})
	return paths, len(paths) == 0
}

// EvidenceFresh accepts a check until one of its read/executed files or
// deliverables changes. A structured receipt takes precedence over lexical
// inference, including mutations outside the recorded tool history.
func EvidenceFresh(history []HistoryTool, index int, deliverables []string) bool {
	if index < 0 || index >= len(history) {
		return false
	}
	h := history[index]
	if h.Dependencies != nil {
		return !h.DependenciesChanged
	}
	deps := append([]string(nil), deliverables...)
	deps = append(deps, dependencyPathRe.FindAllString(h.Args, -1)...)
	for _, before := range history[:index] {
		if writeTools[before.Name] {
			if p := argPath(before); p != "" {
				deps = append(deps, p)
			}
		}
	}
	for _, later := range history[index+1:] {
		paths, unknown := mutationPaths(later)
		if unknown {
			return false
		}
		for _, p := range paths {
			for _, dep := range deps {
				if overlapPath(p, dep) {
					return false
				}
			}
		}
	}
	return true
}

// MutationTargets exposes known shell/write destinations for evidence capture.
// Unknown side effects retain the conservative history fallback.
func MutationTargets(h HistoryTool) ([]string, bool) { return mutationPaths(h) }
