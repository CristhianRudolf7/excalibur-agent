package tools

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"
)

func liveJobProgressEnabled() bool  { return switchDefaultOn("EXCALIBUR_LIVE_JOB_PROGRESS") }
func liveJobDuplicateEnabled() bool { return switchDefaultOn("EXCALIBUR_LIVE_JOB_DUPLICATES") }

func (s *Session) runningProgress() string {
	if !liveJobProgressEnabled() {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited {
		return ""
	}
	now := time.Now()
	last := s.lastOutput
	note := ""
	if last.IsZero() {
		last = s.Started
		note = " (no output yet)"
	}
	return fmt.Sprintf("STILL RUNNING session: %s  elapsed: %.1fs  last_output_age: %.1fs%s  output_bytes: %d\n", s.ID, max(0, now.Sub(s.Started).Seconds()), max(0, now.Sub(last).Seconds()), note, s.full.Len())
}

func sessionProgress(m *SessionManager, id string) string {
	if s, ok := m.Get(SessionID(id)); ok {
		return s.runningProgress()
	}
	return ""
}

// Literal output destinations only: dynamic shell expansions remain unknown.
func jobOutputPaths(command, dir string) map[string]bool {
	paths := map[string]bool{}
	add := func(p string) {
		if p == "" || p == "-" || strings.HasPrefix(p, "/dev/") {
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		paths[filepath.Clean(p)] = true
	}
	tree, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return paths
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Redirect:
			if n.Op == syntax.RdrOut || n.Op == syntax.AppOut || n.Op == syntax.RdrAll || n.Op == syntax.AppAll {
				if p, ok := literalShellWord(n.Word); ok {
					add(p)
				}
			}
		case *syntax.CallExpr:
			for i, w := range n.Args {
				p, ok := literalShellWord(w)
				if !ok {
					continue
				}
				if (p == "-o" || p == "--output" || p == "--output-file" || p == "--out") && i+1 < len(n.Args) {
					if dest, ok := literalShellWord(n.Args[i+1]); ok {
						add(dest)
					}
				}
				for _, flag := range []string{"--output=", "--output-file=", "--out="} {
					if strings.HasPrefix(p, flag) {
						add(strings.TrimPrefix(p, flag))
					}
				}
			}
		}
		return true
	})
	return paths
}

func (m *SessionManager) duplicateJobWarning(command, dir string) string {
	if !liveJobDuplicateEnabled() {
		return ""
	}
	outputs := jobOutputPaths(command, dir)
	var ids []string
	for _, s := range m.List("") {
		s.mu.Lock()
		alive, old, oldDir := !s.exited, s.submittedCmd, s.Dir
		if old == "" {
			old = s.Cmd
		}
		id := s.ID
		s.mu.Unlock()
		if !alive {
			continue
		}
		same := strings.TrimSpace(command) == strings.TrimSpace(old) && filepath.Clean(dir) == filepath.Clean(oldDir)
		if !same {
			for p := range jobOutputPaths(old, oldDir) {
				if outputs[p] {
					same = true
					break
				}
			}
		}
		if same {
			ids = append(ids, string(id))
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return "warning: command duplicates a live job or output destination in session(s) " + strings.Join(ids, ", ") + "; launch allowed; check whether parallel work is intentional.\n"
}
