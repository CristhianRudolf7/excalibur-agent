package codeintel

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func (t *CodeDiagramTool) render(ctx context.Context, a diagramArgs) (tools.Result, error) {
	result := tools.Result{Status: tools.StatusOK}
	fail := func(status tools.ResultStatus, err error) (tools.Result, error) {
		if ctx.Err() != nil {
			status = tools.StatusCanceled
			err = ctx.Err()
		}
		result.Status = status
		result.Output += "code_diagram: " + err.Error()
		return result, nil
	}
	root, target, err := t.diagramPaths(a.Path)
	if err != nil {
		return fail(tools.StatusInputError, err)
	}
	g, report, err := indexDiagram(ctx, root, target, a.Focus, a.IncludeTests, defaultDiagramIndexLimits)
	if err != nil {
		return fail(tools.StatusToolError, err)
	}
	defer g.Close()
	result.Output = fmt.Sprintf("Indexed %d code files (%d bytes); %d skipped. Static graph; calls are potential targets.\n", report.Files, report.Bytes, report.Skipped)
	if report.Partial {
		result.Output += fmt.Sprintf("Partial index: requested subtree %s indexed first (priority %s); file, byte, entry or time limits reached, or files skipped. Narrow path/focus for more detail.\n", target, report.Prioritized)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fail(tools.StatusInputError, err)
	}
	a.scope = filepath.ToSlash(rel)
	plans, err := planDiagrams(ctx, g, a, NewIndexer(g, root).goModulePrefix())
	if err != nil {
		return fail(tools.StatusInputError, err)
	}
	dir, err := t.diagramArtifactDir(root)
	if err != nil {
		return fail(tools.StatusToolError, err)
	}
	// A failure before the first rendered artifact should leave no orphan sources.
	defer func() {
		if len(result.Artifacts) == 0 {
			_ = os.RemoveAll(dir)
		}
	}()
	backend := tools.DiagramBackend(&tools.SVGDiagramBackend{})
	if t.Media != nil && t.Media.Diagram != nil {
		backend = t.Media.Diagram
	}
	sourceExt := "mmd"
	if a.Format == "graphviz" {
		sourceExt = "dot"
	}
	for i, plan := range plans {
		if err := ctx.Err(); err != nil {
			return fail(tools.StatusCanceled, err)
		}
		source := plan.Projection.source(a.Format, a.Direction)
		sourcePath := filepath.Join(dir, fmt.Sprintf("%02d.%s", i, sourceExt))
		renderedPath := filepath.Join(dir, fmt.Sprintf("%02d.%s", i, a.Output))
		if err := os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
			return fail(tools.StatusToolError, err)
		}
		rendered, err := backend.Render(ctx, source, a.Format, a.Output)
		if err != nil {
			return fail(tools.StatusToolError, err)
		}
		if err := ctx.Err(); err != nil {
			return fail(tools.StatusCanceled, err)
		}
		if err := os.WriteFile(renderedPath, []byte(rendered), 0600); err != nil {
			return fail(tools.StatusToolError, err)
		}
		result.Artifacts = append(result.Artifacts, tools.ArtifactRef{Kind: "diagram", Title: plan.Title, Path: renderedPath, Format: a.Output, Dialect: a.Format, SourcePath: sourcePath, Group: filepath.Base(dir)})
		result.Output += fmt.Sprintf("%s: %d nodes, %d edges, %d collapsed; source %s; rendered %s\n", plan.Title, len(plan.Projection.Nodes), len(plan.Projection.Edges), plan.Projection.Collapsed, sourcePath, renderedPath)
	}
	return result, nil
}

func (t *CodeDiagramTool) diagramArtifactDir(root string) (string, error) {
	run := t.RunID
	if run == "" {
		run = "standalone"
	}
	if strings.ContainsAny(run, "/\\") || run == "." || run == ".." || len(run) > 120 {
		run = fmt.Sprintf("run-%x", sha256.Sum256([]byte(run)))
	}
	// Resolve each existing ancestor before creating children so a workspace
	// artifact symlink cannot redirect writes outside the allowed tree.
	dir := root
	for _, part := range []string{".excalibur", "artifacts", run} {
		dir = filepath.Join(dir, part)
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return "", err
		}
		if !diagramInside(root, resolved) {
			return "", fmt.Errorf("artifact directory must stay inside the run workspace")
		}
		dir = resolved
	}
	return os.MkdirTemp(dir, "code-diagram-")
}
