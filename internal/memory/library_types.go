package memory

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"time"
)

type DocMeta struct {
	ID         string
	Title      string
	SourceName string
	MIME       string
	Format     string
	Size       int64
	BlobDigest string
	Origin     string
}

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

type ListQuery struct {
	Query, Status string
	Limit         int
	Cursor        string
}

type ChunkDetail struct {
	ChunkID    string           `json:"chunk_id"`
	DocumentID string           `json:"document_id"`
	Section    string           `json:"section_path"`
	Anchor     string           `json:"anchor"`
	Text       string           `json:"text"`
	Version    int              `json:"version"`
	Page       int              `json:"page"`
	Paragraph  int              `json:"paragraph"`
	PageLabel  string           `json:"page_label"`
	Boxes      []docextract.Box `json:"boxes"`
}

// ExpandedHit carries context without changing the citable hit.
type ExpandedHit struct {
	Hit      LibraryHit `json:"hit"`
	Previous string     `json:"previous,omitempty"`
	Next     string     `json:"next,omitempty"`
	Tables   []string   `json:"tables,omitempty"`
}
