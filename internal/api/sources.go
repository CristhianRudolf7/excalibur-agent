package api

import "github.com/CristhianRudolf7/excalibur-agent/internal/docextract"

// SourceRef is a validated source attached to a persisted answer (Section 10.9).
// Document locations come from stored metadata, never model-generated labels.
type SourceRef struct {
	N           int              `json:"n"`
	Kind        string           `json:"kind"`
	DocumentID  string           `json:"document_id,omitempty"`
	ChunkID     string           `json:"chunk_id,omitempty"`
	Title       string           `json:"title"`
	Page        int              `json:"page,omitempty"`
	PageLabel   string           `json:"page_label,omitempty"`
	Paragraph   int              `json:"paragraph,omitempty"`
	SectionPath string           `json:"section_path,omitempty"`
	Excerpt     string           `json:"excerpt,omitempty"`
	Boxes       []docextract.Box `json:"boxes,omitempty"`
	MIME        string           `json:"mime,omitempty"`
	BlobDigest  string           `json:"blob_digest,omitempty"`
	Verbatim    string           `json:"verbatim,omitempty"`
	URL         string           `json:"url,omitempty"`
	Domain      string           `json:"domain,omitempty"`
	ReadAt      string           `json:"read_at,omitempty"`
	Tool        string           `json:"tool,omitempty"`
}
