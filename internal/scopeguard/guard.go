// Package scopeguard preserves run inputs and checks explicit filesystem scope.
package scopeguard

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var paths = regexp.MustCompile(`(?:/|\./)[A-Za-z0-9_@.*-]+(?:/[A-Za-z0-9_@.*-]+)*|[A-Za-z0-9_@*-]+(?:/[A-Za-z0-9_@.*-]+)+|[A-Za-z0-9_@*-]+\.[A-Za-z0-9_@.*-]+`)
var prohibition = regexp.MustCompile(`(?i)(?:do not|don't|must not|never)\s+(?:modify|delete|change|edit|touch)|leave\s+.*(?:alone|unchanged|untouched)`)
var expectation = regexp.MustCompile(`(?m)^.*(?:assert\b|expected\b|expect\(|want\s*[:=]).*$`)

// Guard is owned by the run loop; callers serialize observations.
type Guard struct {
	GitPaths          map[string]string
	Root              string
	StatePath         string
	instructionHash   string
	stateError        string
	GitDiffBaseline   string
	GitDiffCurrent    string
	Initial           map[string]string
	Protected         map[string]string
	Backups           map[string]string
	Constraints       []string
	failureChecks     map[string]string
	inlineChecks      map[string]string
	violations        []string
	only              []string
	required          []string
	counts            []countRule
	protectedDirs     []string
	preserveExistence []string
}

func digest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, e := os.Readlink(path)
		return fmt.Sprintf("link:%s", target), e
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%o", h.Sum(nil), info.Mode().Perm()), nil
}

func (g *Guard) resolve(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(g.Root, p)
}

// Start snapshots regular files without following directory symlinks. Input
// copies are eager because even a database inspection can remove its WAL.
func Start(root, instruction string) (*Guard, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	g := &Guard{Root: root, Initial: map[string]string{}, Protected: map[string]string{}, Backups: map[string]string{}, failureChecks: map[string]string{}, inlineChecks: map[string]string{}}
	snapshotPaths := []string{root}
	if filepath.Dir(root) == root {
		if onlyEdits.MatchString(instruction) || (prohibition.MatchString(instruction) && strings.Contains(strings.ToLower(instruction), "all files")) {
			return nil, fmt.Errorf("scope guard requires a project directory for tree-wide constraints at the filesystem root")
		}
		// The filesystem root is an environment, not a working tree. Named
		// files are fingerprinted below; explicitly scoped directories still
		// get a complete snapshot without walking virtual filesystems.
		snapshotPaths = nil
		for _, sentence := range clauses.Split(instruction, -1) {
			for _, p := range constraintPaths(sentence) {
				p = g.resolve(p)
				if p != root {
					if info, e := os.Stat(p); e == nil && info.IsDir() {
						snapshotPaths = append(snapshotPaths, p)
					}
				}
			}
		}
	}
	for _, snapshotPath := range snapshotPaths {
		err = filepath.WalkDir(snapshotPath, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() && d.Type()&os.ModeSymlink == 0 {
				return nil
			}
			h, e := digest(p)
			if e == nil {
				g.Initial[p] = h
			}
			return e
		})
		if err != nil {
			return nil, err
		}
	}
	for _, sentence := range clauses.Split(instruction, -1) {
		lower := strings.ToLower(sentence)
		if prohibition.MatchString(sentence) || regexp.MustCompile(`\b(only|all|exactly|at least|at most)\b|\d+`).MatchString(lower) {
			g.Constraints = append(g.Constraints, strings.TrimSpace(sentence))
		}
		if prohibition.MatchString(sentence) && regexp.MustCompile(`(?i)\ball\s+(?:existing\s+)?files\b`).MatchString(sentence) {
			for p, h := range g.Initial {
				g.Protected[p] = h
			}
		}
		for _, p := range constraintPaths(sentence) {
			p = g.resolve(strings.TrimRight(p, ".,"))
			if prohibition.MatchString(sentence) {
				if deleteOnly.MatchString(sentence) {
					if _, err := os.Lstat(p); err == nil {
						g.preserveExistence = append(g.preserveExistence, p)
					}
					continue
				}
				if info, e := os.Stat(p); e == nil && info.IsDir() {
					// Explicit protection includes metadata directories even
					// when the general working-tree snapshot excludes them.
					if e = filepath.WalkDir(p, func(path string, entry fs.DirEntry, walkErr error) error {
						if walkErr != nil {
							return walkErr
						}
						if entry.Type().IsRegular() || entry.Type()&os.ModeSymlink != 0 {
							h, hashErr := digest(path)
							if hashErr != nil {
								return hashErr
							}
							g.Initial[path] = h
						}
						return nil
					}); e != nil {
						return nil, e
					}
					for candidate, h := range g.Initial {
						if strings.HasPrefix(candidate, p+string(filepath.Separator)) {
							g.Protected[candidate] = h
						}
					}
					g.protectedDirs = append(g.protectedDirs, p)
					continue
				}
				if matches, _ := filepath.Glob(p); len(matches) > 0 {
					for _, m := range matches {
						if h, e := digest(m); e == nil {
							g.Protected[m] = h
						}
					}
					continue
				}
				g.Protected[p] = g.Initial[p]
				if h, e := digest(p); e == nil {
					g.Protected[p] = h
				}
			}
		}
	}
	g.GitDiffBaseline = gitDiffHash(root)
	g.GitPaths = gitChanges(root)
	g.extract(instruction)
	for _, p := range g.namedInputs(instruction) {
		p = g.resolve(p)
		if info, e := os.Lstat(p); e == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			if h, e := digest(p); e == nil {
				g.Initial[p] = h
			}
		}
	}
	inputs := map[string]bool{}
	for _, p := range g.namedInputs(instruction) {
		p = g.resolve(p)
		if dataInput(p) {
			inputs[p] = true
			for _, suffix := range []string{"-wal", "-shm", "-journal"} {
				inputs[p+suffix] = true
			}
		}
	}

	var dir string
	for p := range inputs {
		info, e := os.Stat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if dir == "" {
			if e = os.MkdirAll("/tmp/excalibur-originals", 0700); e != nil {
				return nil, e
			}
			dir, e = os.MkdirTemp("/tmp/excalibur-originals", "run-")
			if e != nil {
				return nil, e
			}
		}
		// Preserve relative layout, including database companion filenames.
		dst := filepath.Join(dir, strings.TrimPrefix(filepath.Clean(p), string(filepath.Separator)))
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return nil, e
		}
		src, e := os.Open(p)
		if e != nil {
			return nil, e
		}
		out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			src.Close()
			return nil, e
		}
		_, e = io.Copy(out, src)
		ce := out.Close()
		src.Close()
		if e != nil {
			return nil, e
		}
		if ce != nil {
			return nil, ce
		}
		g.Backups[p] = dst
	}
	return g, nil
}

// Observe records expectation lines after a failed check. Later changes to
// these lines require restoring the check; changing implementation is allowed.
func (g *Guard) Observe(failed bool) {
	g.observe(failed, nil)
}

func (g *Guard) observe(failed bool, include func(string) bool) {
	for p, old := range g.failureChecks {
		b, e := os.ReadFile(p)
		if e != nil || strings.Join(expectation.FindAllString(string(b), -1), "\n") != old {
			g.violations = append(g.violations, "test-tampering: expected value changed after failed check in "+p)
		}
	}
	if !failed {
		return
	}
	_ = filepath.WalkDir(g.Root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if include != nil && !include(p) {
			return nil
		}
		info, e := d.Info()
		if e != nil || info.Size() > 1<<20 {
			return nil
		}
		b, e := os.ReadFile(p)
		if e == nil {
			lines := expectation.FindAllString(string(b), -1)
			if len(lines) > 0 {
				g.failureChecks[p] = strings.Join(lines, "\n")
			}
		}
		return nil
	})
}

// Check returns exact filesystem violations; constraints without a deterministic
// filesystem interpretation remain visible in the run's verification context.
func (g *Guard) Check() string {
	g.GitDiffCurrent = gitDiffHash(g.Root)
	bad := g.checkConstraints()
	bad = append(bad, g.checkGit()...)
	if g.stateError != "" {
		bad = append(bad, g.stateError)
	}
	for p, want := range g.Protected {
		got, e := digest(p)
		if (want != "" && e != nil) || got != want {
			bad = append(bad, "protected path changed or deleted: "+p)
		}
	}
	for p, old := range g.failureChecks {
		b, e := os.ReadFile(p)
		if e != nil || strings.Join(expectation.FindAllString(string(b), -1), "\n") != old {
			bad = append(bad, "test-tampering: expected value changed after failed check in "+p)
		}
	}
	bad = append(bad, g.violations...)
	sort.Strings(bad)
	return strings.Join(bad, "; ")
}

// Note reports preservation paths and literal constraints to the agent.
func (g *Guard) Note() string {
	lines := []string{"Scope guard active. Probe copies of input data first; preserve originals. Never change an expected value to make a failed check pass."}
	var keys []string
	for p := range g.Backups {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		lines = append(lines, "Input preserved: "+p+" -> "+g.Backups[p])
	}
	if g.GitDiffBaseline != "" {
		lines = append(lines, "Git diff baseline SHA256: "+g.GitDiffBaseline)
	}
	lines = append(lines, g.Constraints...)
	return strings.Join(lines, "\n")
}
