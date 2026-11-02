package api

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"time"
)

// DocumentInfo is the library document metadata on the client wire.
type DocumentInfo struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	SourceName    string    `json:"source_name"`
	MIME          string    `json:"mime"`
	Format        string    `json:"format"`
	BlobDigest    string    `json:"blob_digest"`
	ScopeID       string    `json:"scope_id"`
	Size          int64     `json:"size"`
	Version       int       `json:"version"`
	Status        string    `json:"status"`
	ProgressPages int       `json:"progress_pages"`
	TotalPages    int       `json:"total_pages"`
	PageCount     int       `json:"page_count"`
	Paginated     bool      `json:"paginated"`
	UploadedAt    time.Time `json:"uploaded_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Error         string    `json:"error"`
	FailureStage  string    `json:"failure_stage"`
	Warnings      []string  `json:"warnings"`
	Summary       string    `json:"summary"`
}

type ChunkDetail struct {
	ChunkID    string           `json:"chunk_id"`
	DocumentID string           `json:"document_id"`
	Version    int              `json:"version"`
	Page       int              `json:"page"`
	PageLabel  string           `json:"page_label"`
	Paragraph  int              `json:"paragraph"`
	Section    string           `json:"section_path"`
	Anchor     string           `json:"anchor"`
	Text       string           `json:"text"`
	Boxes      []docextract.Box `json:"boxes"`
}

type LibraryIngestParams struct {
	BlobDigest string `json:"blob_digest"`
	Name       string `json:"name"`
	MIME       string `json:"mime"`
	Title      string `json:"title,omitempty"`
	ScopeID    string `json:"scope_id,omitempty"`
}
type LibraryListParams struct {
	ScopeID string `json:"scope_id,omitempty"`
	Query   string `json:"query,omitempty"`
	Status  string `json:"status,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}
type LibraryIDParams struct {
	ID string `json:"id"`
}
type LibraryPagesParams struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
	From    int    `json:"from"`
	To      int    `json:"to"`
}
type LibraryChunkParams struct {
	DocumentID string `json:"document_id"`
	ChunkID    string `json:"chunk_id"`
}
type LibraryDocumentResult struct {
	Document DocumentInfo `json:"document"`
}
type LibraryIngestResult struct {
	Document DocumentInfo `json:"document"`
	Existing bool         `json:"existing"`
}
type LibraryListResult struct {
	Documents  []DocumentInfo `json:"documents"`
	NextCursor string         `json:"next_cursor"`
}
type LibraryPagesResult struct {
	Document DocumentInfo      `json:"document"`
	Pages    []docextract.Page `json:"pages"`
}
type LibraryChunkResult struct {
	Chunk ChunkDetail `json:"chunk"`
}
type LibraryDeleteResult struct {
	Deleted bool   `json:"deleted"`
	Notice  string `json:"notice"`
}
