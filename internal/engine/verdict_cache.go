package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

type reviewVerdict struct{ key, reply string }

// Hash complete regular files, never FIFOs, devices or socket endpoints.
func fileVersion(path string) string {
	if strings.HasPrefix(path, sourceScopePrefix) {
		return sourceScopeVersion(path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "missing"
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "broken-link"
		}
		return "link:" + target + ":" + fileVersion(target)
	}
	if !info.Mode().IsRegular() {
		return info.Mode().String()
	}
	f, err := os.Open(path)
	if err != nil {
		return "unreadable"
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "unreadable"
	}
	return fmt.Sprintf("%o:%x", info.Mode().Perm(), h.Sum(nil))
}

// Literal file operands include inline script references. Dynamic filesystem
// accesses additionally use the dependency receipts captured on tool results.
var fileOperandRe = regexp.MustCompile(`[a-zA-Z0-9_./-]+\.[a-zA-Z0-9_.-]+|/[a-zA-Z0-9_./-]+`)

func historyFileOperands(h tasks.HistoryTool, workdir string) []string {
	var a map[string]any
	_ = json.Unmarshal([]byte(h.Args), &a)
	if dir, ok := a["workdir"].(string); ok && dir != "" {
		if filepath.IsAbs(dir) {
			workdir = dir
		} else {
			workdir = filepath.Join(workdir, dir)
		}
	}
	seen := map[string]bool{}
	targets, _ := tasks.MutationTargets(h)
	written := map[string]bool{}
	for _, target := range targets {
		if !filepath.IsAbs(target) {
			target = filepath.Join(workdir, target)
		}
		written[filepath.Clean(target)] = true
	}
	for _, key := range []string{"path", "file_path", "file", "filename", "command"} {
		value, _ := a[key].(string)
		operands := []string{value}
		if key == "command" {
			operands = fileOperandRe.FindAllString(value, -1)
		}
		for _, p := range operands {
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(workdir, p)
			}
			p = filepath.Clean(p)
			if written[p] {
				continue
			}
			if info, err := os.Lstat(p); err == nil && !info.IsDir() {
				seen[p] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (e *Engine) reviewVersion(run *Run, cfg tasks.SelfVerifyConfig, drafts []tasks.DraftCriterion, history []tasks.HistoryTool) string {
	run.mu.Lock()
	request, workdir := run.Request, run.Workdir
	run.mu.Unlock()
	files := map[string]string{}
	for _, d := range e.deliverables(run, request, history) {
		files[d.Path] = fileVersion(d.Path)
	}
	var evidence []tasks.HistoryTool
	seen := map[string]bool{}
	for _, h := range history {
		if h.Name != "exec" && h.Name != "read_file" && h.Name != "call_tool" && h.Name != "exec_io" {
			continue
		}
		// Repeated unchanged checks represent the same evidence version.
		raw, _ := json.Marshal(h)
		if !seen[string(raw)] {
			evidence = append(evidence, h)
			seen[string(raw)] = true
		}
		if scope := projectSourceScope(h, workdir); scope != "" {
			files[scope] = fileVersion(scope)
		}
		for p := range h.Dependencies {
			files[p] = fileVersion(p)
		}
		for _, p := range historyFileOperands(h, workdir) {
			files[p] = fileVersion(p)
		}
	}
	if len(files) > 0 && !validDependencyVersions(files) {
		return ""
	}
	raw, _ := json.Marshal(struct {
		Request  string
		Config   tasks.SelfVerifyConfig
		Drafts   []tasks.DraftCriterion
		Evidence []tasks.HistoryTool
		Files    map[string]string
	}{request, cfg, drafts, evidence, files})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (r *Run) cachedReview(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.review.reply, key != "" && r.review.key == key && r.review.reply != ""
}
func (r *Run) cacheReview(key, reply string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.review = reviewVerdict{key: key, reply: strings.TrimSpace(reply)}
}
