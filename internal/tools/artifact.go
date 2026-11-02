package tools

// ArtifactRef describes a visual file a tool produced for the user, such as a
// rendered diagram or chart. The daemon collects these references during a
// turn and announces them to clients when the turn ends, so an interface can
// open a side panel showing the result.
type ArtifactRef struct {
	// Kind is the artifact family: "diagram", "chart", "image" or "document".
	Kind string `json:"kind"`
	// Title is a short human-readable caption.
	Title string `json:"title,omitempty"`
	// Path is the absolute path of the rendered file.
	Path string `json:"path"`
	// Format is the rendered file format: "svg", "png", "pdf", ...
	Format string `json:"format"`
	// Dialect is the source notation of a diagram ("mermaid", "d2",
	// "graphviz", "plantuml", "svg", "drawio"); empty for other kinds.
	Dialect string `json:"dialect,omitempty"`
	// SourcePath is the absolute path of the source text, when one was kept.
	SourcePath string `json:"source_path,omitempty"`
	// Group links several artifacts that belong together, for example an
	// overview diagram and its per-module detail diagrams.
	Group string `json:"group,omitempty"`
}
