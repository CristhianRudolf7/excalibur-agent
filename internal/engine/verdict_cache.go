package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
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

// reviewReuseAudit contains identities and versions only, never request, tool
// output or file contents. RequestRevision is the literal instruction digest.
type reviewReuseAudit struct {
	Key                     string            `json:"key"`
	Hit                     bool              `json:"hit"`
	RequestRevision         string            `json:"request_revision"`
	ContextRevision         int               `json:"context_revision"`
	Dependencies            map[string]string `json:"dependencies"`
	SessionAuthorityVersion string            `json:"session_authority_version"`
	DependencyReceipts      int               `json:"dependency_receipts"`
	LiteralFallback         bool              `json:"literal_operand_fallback"`
	VersionsValid           bool              `json:"versions_valid"`
}

func (e *Engine) recordReviewReuse(ctx context.Context, run *Run, audit reviewReuseAudit, hit bool) {
	if !switchOn("EXCALIBUR_REVIEW_REUSE_AUDIT") {
		return
	}
	audit.Hit = hit
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.Record(ctx, journal.EntryType("review_reuse"), journal.MarshalPayload(audit), "", "", "")
	}
}

func (e *Engine) reviewIdentity(run *Run, cfg tasks.SelfVerifyConfig, drafts []tasks.DraftCriterion, history []tasks.HistoryTool) (string, reviewReuseAudit) {
	run.mu.Lock()
	request, workdir := run.verificationRequestLocked(), run.Workdir
	revision := run.contextRevision
	run.mu.Unlock()
	files := map[string]string{}
	audit := reviewReuseAudit{RequestRevision: fmt.Sprintf("%x", sha256.Sum256([]byte(request))), ContextRevision: revision, Dependencies: files}
	sessions := map[string]tasks.SessionState{}
	for _, h := range history {
		for _, receipt := range h.Sessions {
			sessions[receipt.ID] = tasks.SessionState{Alive: receipt.Alive, ExitCode: receipt.ExitCode}
		}
		for id, state := range h.SessionStates {
			sessions[id] = state
		}
	}
	sessionRaw, _ := json.Marshal(sessions)
	audit.SessionAuthorityVersion = fmt.Sprintf("%x", sha256.Sum256(sessionRaw))
	for _, d := range e.deliverables(run, request, history) {
		files[d.Path] = fileVersion(d.Path)
	}
	var evidence []tasks.HistoryTool
	seen := map[string]bool{}
	for _, h := range history {
		if h.Name != "exec" && h.Name != "read_file" && h.Name != "call_tool" && h.Name != "exec_io" && h.Name != "inspect_image" {
			continue
		}
		if len(h.Dependencies) > 0 {
			audit.DependencyReceipts++
		} else {
			audit.LiteralFallback = true
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
		return "", audit
	}
	audit.VersionsValid = true
	raw, _ := json.Marshal(struct {
		Request  string
		Config   tasks.SelfVerifyConfig
		Drafts   []tasks.DraftCriterion
		Evidence []tasks.HistoryTool
		Files    map[string]string
	}{request, cfg, drafts, evidence, files})
	audit.Key = fmt.Sprintf("%x", sha256.Sum256(raw))
	return audit.Key, audit
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
