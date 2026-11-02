package tools

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

func visualOutputFormat(format, output string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(output), "."))
	knownExt := ext == "svg" || ext == "png" || ext == "pdf"
	if format == "" {
		format = "svg"
		if knownExt {
			format = ext
		}
	}
	if format != "svg" && format != "png" && format != "pdf" {
		return "", fmt.Errorf("unsupported output format %q (want svg, png or pdf)", format)
	}
	if knownExt && ext != format {
		return "", fmt.Errorf("output extension .%s does not match output_format %s", ext, format)
	}
	return format, nil
}

func visualArtifactPaths(deps *MediaDeps, output, kind, title, dialect, format, source string) (ArtifactRef, error) {
	if output == "" {
		hash := sha256.Sum256([]byte(dialect + "\x00" + format + "\x00" + source))
		output = filepath.Join("artifacts", fmt.Sprintf("%s-%x.%s", kind, hash[:8], format))
	}
	path, err := mediaOutputPath(deps, output)
	if err != nil {
		return ArtifactRef{}, err
	}
	ext := "json"
	if kind == "diagram" {
		ext = diagramSourceExtension(dialect)
	}
	sourcePath := strings.TrimSuffix(path, filepath.Ext(path)) + "." + ext
	// SVG source and its sanitized SVG rendering must remain separate files.
	if sourcePath == path {
		sourcePath = strings.TrimSuffix(path, filepath.Ext(path)) + ".source." + ext
	}
	sourcePath, err = mediaOutputPath(deps, sourcePath)
	if err != nil {
		return ArtifactRef{}, err
	}
	return ArtifactRef{Kind: kind, Title: title, Path: path, Format: format, Dialect: dialect, SourcePath: sourcePath}, nil
}
func saveVisualArtifact(artifact ArtifactRef, data, source string) error {
	if err := writeAtomic(artifact.SourcePath, []byte(source)); err != nil {
		return fmt.Errorf("save visual source: %w", err)
	}
	if err := writeAtomic(artifact.Path, []byte(data)); err != nil {
		return fmt.Errorf("save rendered visual: %w", err)
	}
	return nil
}
func visualArtifactKeys(artifact ArtifactRef) []string {
	return []string{"file:" + artifact.Path, "file:" + artifact.SourcePath}
}

type diagramRenderArguments struct {
	Source       string `json:"source"`
	Dialect      string `json:"dialect"`
	Title        string `json:"title"`
	OutputFormat string `json:"output_format"`
	Output       string `json:"output"`
}

func (a diagramRenderArguments) artifact(deps *MediaDeps) (ArtifactRef, error) {
	format, err := visualOutputFormat(a.OutputFormat, a.Output)
	if err != nil {
		return ArtifactRef{}, err
	}
	dialect := canonicalDiagramDialect(a.Dialect)
	if err := checkDiagramSyntax(a.Source, dialect); err != nil {
		return ArtifactRef{}, err
	}
	title := a.Title
	if title == "" {
		title = dialect + " diagram"
	}
	return visualArtifactPaths(deps, a.Output, "diagram", title, dialect, format, a.Source)
}
