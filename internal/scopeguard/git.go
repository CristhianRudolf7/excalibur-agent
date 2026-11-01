package scopeguard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitDiffHash includes index and working-tree differences from HEAD, including
// mode changes. Existing operator changes are recorded as the baseline.
func gitDiffHash(root string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--relative", "--no-ext-diff", "--no-textconv", "--binary", "HEAD", "--")
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// gitChanges records path-level diff metadata so index-only edits cannot hide
// behind an unchanged working-tree fingerprint. Rename detection is disabled
// to give every changed path an independent entry.
func gitChanges(root string) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--relative", "--no-ext-diff", "--no-textconv", "--no-renames", "--raw", "-z", "HEAD", "--")
	b, err := cmd.Output()
	if err != nil {
		return nil
	}
	fields := strings.Split(string(b), "\x00")
	out := map[string]string{}
	for i := 0; i+1 < len(fields); i += 2 {
		out[filepath.Join(root, fields[i+1])] = fields[i]
	}
	return out
}

func (g *Guard) checkGit() []string {
	current := gitChanges(g.Root)
	var bad []string
	union := map[string]bool{}
	for p := range current {
		union[p] = true
	}
	for p := range g.GitPaths {
		union[p] = true
	}
	for p := range union {
		if current[p] == g.GitPaths[p] {
			continue
		}
		if _, protected := g.Protected[p]; protected {
			bad = append(bad, "protected path git diff changed: "+p)
		}
		if len(g.only) > 0 {
			allowed := false
			for _, a := range g.only {
				if p == a || strings.HasPrefix(p, a+string(filepath.Separator)) {
					allowed = true
				}
			}
			if !allowed {
				bad = append(bad, "only constraint: git diff changed outside allowed locations: "+p)
			}
		}
	}
	return bad
}
