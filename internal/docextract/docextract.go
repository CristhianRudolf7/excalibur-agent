// Package docextract defines the shared, format-neutral result of reading a
// document (PDF, OOXML, OpenDocument, EPUB, text, images) so the library,
// the document tools and the viewers agree on pages, blocks and positions.
//
// Conventions:
//   - Page numbers are 1-based physical page indexes. Conversion from any
//     0-based engine index happens once, inside the extractor.
//   - Label is the printed page label when the document defines one
//     ("xii", "45"); citations show Label when present and open Number.
//   - Boxes use PDF user-space points with the origin at the top-left of
//     the page (y grows downwards), so viewers can overlay them directly
//     after scaling by rendered width / Page.Width.
//   - Unpaginated formats (DOCX, XLSX, PPTX, ODF, EPUB, text) set
//     Document.Paginated=false and put everything on logical pages: one
//     page for text and DOCX/ODT, one page per sheet, one page per slide.
//     They never invent page numbers.
package docextract

import (
	"context"
	"io"
)

// Block kinds.
const (
	KindHeading   = "heading"
	KindParagraph = "paragraph"
	KindList      = "list_item"
	KindTable     = "table" // Text holds the table as GitHub Markdown.
	KindCaption   = "caption"
	KindCell      = "cell" // spreadsheet cell; Anchor is "Sheet!A1".
	KindSlideText = "slide_text"
	KindNote      = "note"  // speaker notes, footnotes, endnotes.
	KindImage     = "image" // Text is alt text or a vision description.
)

// Extraction methods for a page.
const (
	MethodNative = "native"
	MethodVision = "vision"
	MethodMixed  = "mixed"
	MethodNone   = "none" // nothing extractable and no OCR available.
)

// Box is one rectangle on one physical page, in points, top-left origin.
type Box struct {
	Page int     `json:"page"`
	X0   float64 `json:"x0"`
	Y0   float64 `json:"y0"`
	X1   float64 `json:"x1"`
	Y1   float64 `json:"y1"`
}

// Block is one structural unit in reading order.
type Block struct {
	Kind  string `json:"kind"`
	Level int    `json:"level,omitempty"` // heading level 1..6, list depth.
	Text  string `json:"text"`
	// Anchor is a stable in-document locator: DOCX w14:paraId, PPTX
	// "slide:3/shape:5", XLSX "Sheet1!B2", PDF "p12/b7". Empty when the
	// format has none.
	Anchor string `json:"anchor,omitempty"`
	Boxes  []Box  `json:"boxes,omitempty"`
	// Style carries simple inline formatting for previews: "bold",
	// "italic", "code"; and for tables "header" on the first row.
	Style []string `json:"style,omitempty"`
	// Image references an embedded image part (blob digest) for previews.
	Image string `json:"image,omitempty"`
}

// Page is one physical page (paginated formats) or one logical unit.
type Page struct {
	Number   int      `json:"number"`
	Label    string   `json:"label,omitempty"`
	Title    string   `json:"title,omitempty"` // sheet name or slide title.
	Width    float64  `json:"width,omitempty"`
	Height   float64  `json:"height,omitempty"`
	Method   string   `json:"method"`
	Quality  float64  `json:"quality"` // 0..1, 1 = clean native text.
	Blocks   []Block  `json:"blocks"`
	Warnings []string `json:"warnings,omitempty"`
}

// OutlineEntry is one bookmark or heading-derived table of contents entry.
type OutlineEntry struct {
	Title string `json:"title"`
	Level int    `json:"level"`
	Page  int    `json:"page"` // physical page, 0 when unpaginated.
}

// Document is the full extraction result.
type Document struct {
	Format    string         `json:"format"` // pdf, docx, xlsx, pptx, odt, ods, odp, epub, txt, md, html, csv, json, png, jpeg
	Title     string         `json:"title,omitempty"`
	Author    string         `json:"author,omitempty"`
	Paginated bool           `json:"paginated"`
	PageCount int            `json:"page_count"`
	Pages     []Page         `json:"pages"`
	Outline   []OutlineEntry `json:"outline,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
}

// Options bounds one extraction.
type Options struct {
	// Pages selects physical pages (1-based); nil means all.
	Pages []int
	// MaxPages caps the document (default 2000); larger inputs fail
	// with ErrTooLarge before any heavy work.
	MaxPages int
	// Progress, when set, is called after each page is finished.
	Progress func(done, total int)
}

// Extractor reads one format family.
type Extractor interface {
	// Formats lists the Document.Format values it produces.
	Formats() []string
	// Extract reads the whole input. It must honour ctx cancellation,
	// fail per page instead of per document whenever possible (recording
	// the failure in Page.Warnings) and never execute embedded content.
	Extract(ctx context.Context, r io.ReaderAt, size int64, opts Options) (Document, error)
}

// Renderer turns a physical page into a PNG for OCR and previews.
type Renderer interface {
	RenderPNG(ctx context.Context, r io.ReaderAt, size int64, page int, dpi int) ([]byte, error)
}

// OCR reads text from one rendered page image. Implementations call a
// vision model through the models gateway or a local engine.
type OCR interface {
	OCRPage(ctx context.Context, png []byte, page int) (Page, error)
}
