// Package create generates basic editable Office documents from explicit JSON specs.
package create

import (
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"os"
)

// Spec is selected by the tool's format (docx, xlsx or pptx).
type Spec struct {
	Title  string    `json:"title,omitempty"`
	Blocks []Block   `json:"blocks,omitempty"`
	Page   PageSetup `json:"page,omitempty"`
	Sheets []Sheet   `json:"sheets,omitempty"`
	Slides []Slide   `json:"slides,omitempty"`
}
type PageSetup struct {
	Size        string `json:"size,omitempty"`
	MarginTwips int    `json:"margin_twips,omitempty"`
}
type Run struct {
	Text   string `json:"text"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
	Code   bool   `json:"code,omitempty"`
}

// Block.Kind is paragraph, heading, bullet, numbered, table, page_break or image.
// Text is shorthand for a single run; level is 1..3 for headings and 0..8 for lists.
type Block struct {
	Kind   string     `json:"kind"`
	Text   string     `json:"text,omitempty"`
	Runs   []Run      `json:"runs,omitempty"`
	Level  int        `json:"level,omitempty"`
	Style  string     `json:"style,omitempty"`
	Rows   [][]string `json:"rows,omitempty"`
	Header bool       `json:"header,omitempty"`
	Image  string     `json:"image,omitempty"`
}
type Sheet struct {
	Name           string    `json:"name"`
	Cells          []Cell    `json:"cells,omitempty"`
	ColumnWidths   []float64 `json:"column_widths,omitempty"`
	FreezeFirstRow bool      `json:"freeze_first_row,omitempty"`
}

// Cell.Type is string, number, bool or date (ISO 8601). Formula is stored, not evaluated.
type Cell struct {
	Ref     string `json:"ref"`
	Type    string `json:"type,omitempty"`
	Value   any    `json:"value,omitempty"`
	Formula string `json:"formula,omitempty"`
	Header  bool   `json:"header,omitempty"`
}
type Bullet struct {
	Text  string `json:"text"`
	Level int    `json:"level,omitempty"`
}

// Slide.Kind is title, bullets, two_column, image or table.
type Slide struct {
	Kind     string     `json:"kind"`
	Title    string     `json:"title,omitempty"`
	Subtitle string     `json:"subtitle,omitempty"`
	Bullets  []Bullet   `json:"bullets,omitempty"`
	Left     []Bullet   `json:"left,omitempty"`
	Right    []Bullet   `json:"right,omitempty"`
	Image    string     `json:"image,omitempty"`
	Rows     [][]string `json:"rows,omitempty"`
	Notes    string     `json:"notes,omitempty"`
}

// ImageLoader lets callers enforce workspace confinement before reading images.
type ImageLoader func(string) ([]byte, error)

func Build(format string, s Spec, load ImageLoader) (*opc.Package, error) {
	if err := validateBudget(s); err != nil {
		return nil, err
	}
	if load == nil {
		load = func(p string) ([]byte, error) {
			st, e := os.Stat(p)
			if e != nil {
				return nil, e
			}
			if st.Size() > 16<<20 {
				return nil, fmt.Errorf("image too large")
			}
			return os.ReadFile(p)
		}
	}
	source := load
	imageBytes := 0
	load = func(name string) ([]byte, error) {
		data, err := source(name)
		if err != nil {
			return nil, err
		}
		imageBytes += len(data)
		if imageBytes > 32<<20 {
			return nil, fmt.Errorf("combined images exceed 32 MiB")
		}
		return data, nil
	}
	switch format {
	case "docx":
		return docx(s, load)
	case "xlsx":
		return xlsx(s)
	case "pptx":
		return pptx(s, load)
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

const W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
const S = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
const P = "http://schemas.openxmlformats.org/presentationml/2006/main"
const A = "http://schemas.openxmlformats.org/drawingml/2006/main"
const R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

func esc(s string) string { return opc.Escape(s) }
func relation(id, kind, target string) opc.Relationship {
	return opc.Relationship{ID: id, Type: opc.OfficeRel + kind, Target: target}
}
func root(p *opc.Package, target string) {
	p.SetRelationships("_rels/.rels", []opc.Relationship{relation("rId1", "officeDocument", target)})
}

// validateBudget bounds markup expansion before allocating package parts.
func validateBudget(s Spec) error {
	if len(s.Blocks) > 2000 || len(s.Sheets) > 100 || len(s.Slides) > 500 {
		return fmt.Errorf("spec exceeds block/sheet/slide limits (2000/100/500)")
	}
	text, cells := len(s.Title), 0
	addRows := func(rows [][]string) {
		for _, row := range rows {
			cells += len(row)
			for _, v := range row {
				text += len(v)
			}
		}
	}
	for _, b := range s.Blocks {
		text += len(b.Text)
		cells += len(b.Runs)
		for _, r := range b.Runs {
			text += len(r.Text)
		}
		addRows(b.Rows)
	}
	for _, sh := range s.Sheets {
		cells += len(sh.Cells)
		for _, c := range sh.Cells {
			text += len(c.Formula) + len(fmt.Sprint(c.Value))
		}
	}
	for _, sl := range s.Slides {
		text += len(sl.Title) + len(sl.Subtitle) + len(sl.Notes)
		for _, list := range [][]Bullet{sl.Bullets, sl.Left, sl.Right} {
			cells += len(list)
			for _, b := range list {
				text += len(b.Text)
			}
		}
		addRows(sl.Rows)
	}
	if cells > 20000 || text > 4<<20 {
		return fmt.Errorf("spec exceeds 20000 cells/items or 4 MiB of text")
	}
	return nil
}
