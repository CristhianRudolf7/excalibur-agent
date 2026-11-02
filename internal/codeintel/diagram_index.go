package codeintel

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type diagramIndexLimits struct {
	Files, Entries        int
	FileBytes, TotalBytes int64
	Timeout               time.Duration
}

var defaultDiagramIndexLimits = diagramIndexLimits{Files: 1000, Entries: 20000, FileBytes: 256 << 10, TotalBytes: 32 << 20, Timeout: 15 * time.Second}

type diagramIndexReport struct {
	Files, Skipped int
	Bytes          int64
	Partial        bool
	Prioritized    string
}

func diagramInside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// diagramPaths confines all reads to the canonical run workspace. WalkDir never
// follows descendant symlinks; explicit paths are resolved before confinement.
func (t *CodeDiagramTool) diagramPaths(requested string) (root, target string, err error) {
	if t.Media != nil {
		root = t.Media.Workdir
	}
	if root == "" {
		root, err = os.Getwd()
		if err != nil {
			return "", "", err
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	target = requested
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return "", "", err
	}
	if !diagramInside(root, target) {
		return "", "", fmt.Errorf("path must stay inside the run workspace")
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("path must be a repository or subdirectory")
	}
	return root, target, nil
}

// indexDiagram creates a fresh bounded snapshot on each request, so changed and
// deleted files cannot leave stale edges. It uses the same hash/parser/module
// rewrite path as the incremental project index, without needing an LSP server.
func indexDiagram(ctx context.Context, root, target, focus string, includeTests bool, limits diagramIndexLimits) (*Graph, diagramIndexReport, error) {
	rep := diagramIndexReport{}
	g, err := OpenGraph(":memory:")
	if err != nil {
		return nil, rep, err
	}
	fail := func(err error) (*Graph, diagramIndexReport, error) { _ = g.Close(); return nil, rep, err }
	x := NewIndexer(g, root)
	x.MaxBytes = limits.FileBytes
	priority := filepath.Join(root, filepath.FromSlash(diagramFocus(focus, x.goModulePrefix())))
	if diagramInside(target, priority) {
		if info, e := os.Stat(priority); e == nil && info.IsDir() {
			rep.Prioritized = priority
		}
	}
	if rep.Prioritized == "" {
		rep.Prioritized = target
	}
	indexCtx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	seen := map[string]bool{}
	entries := 0
	ign := loadIgnoreFile(root)
	budget := errors.New("diagram index budget reached")
	walk := func(start string) error {
		return filepath.WalkDir(start, func(p string, d fs.DirEntry, walkErr error) error {
			if err := indexCtx.Err(); err != nil {
				return err
			}
			entries++
			if entries > limits.Entries {
				return budget
			}
			if walkErr != nil {
				rep.Skipped++
				rep.Partial = true
				return nil
			}
			rel, e := filepath.Rel(root, p)
			if e != nil {
				return e
			}
			rel = filepath.ToSlash(rel)
			if d.Type()&os.ModeSymlink != 0 {
				rep.Skipped++
				return nil
			}
			if d.IsDir() {
				if heavyPath(rel) || (ign != nil && ign.ignored(rel, true)) {
					return filepath.SkipDir
				}
				return nil
			}
			if seen[rel] {
				return nil
			}
			seen[rel] = true
			lang, ok := DetectLanguage(rel)
			if !ok || IsDocLang(lang) || (!includeTests && IsTestPath(rel, lang)) || (ign != nil && ign.ignored(rel, false)) {
				return nil
			}
			if rep.Files >= limits.Files {
				return budget
			}
			info, e := d.Info()
			if e != nil {
				rep.Skipped++
				rep.Partial = true
				return nil
			}
			if info.Size() > limits.FileBytes {
				rep.Skipped++
				rep.Partial = true
				return nil
			}
			if rep.Bytes+info.Size() > limits.TotalBytes {
				return budget
			}
			rep.Bytes += info.Size()
			updated, e := x.indexOne(rel)
			if e != nil || !updated {
				rep.Skipped++
				rep.Partial = true
				return nil
			}
			rep.Files++
			return nil
		})
	}
	err = walk(rep.Prioritized)
	if err == nil && rep.Prioritized != target {
		err = walk(target)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if errors.Is(err, budget) || errors.Is(err, context.DeadlineExceeded) {
		rep.Partial = true
		err = nil
	}
	if err != nil {
		return fail(err)
	}
	return g, rep, nil
}
