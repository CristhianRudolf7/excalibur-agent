// Package edit applies guarded, token-span OOXML edits preserving unrelated XML.
package edit

import (
	"context"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"os"
	"path/filepath"
	"strings"
)

// Operation selects one format-specific action. Indexes (slide, to, start) are 1-based.
type Operation struct {
	Action   string        `json:"action"`
	Anchor   string        `json:"anchor,omitempty"`
	Match    string        `json:"match,omitempty"`
	Text     string        `json:"text,omitempty"`
	All      bool          `json:"all,omitempty"`
	Position string        `json:"position,omitempty"`
	Style    string        `json:"style,omitempty"`
	Rows     [][]string    `json:"rows,omitempty"`
	Header   bool          `json:"header,omitempty"`
	Sheet    string        `json:"sheet,omitempty"`
	Name     string        `json:"name,omitempty"`
	Cell     create.Cell   `json:"cell,omitempty"`
	NewSheet *create.Sheet `json:"new_sheet,omitempty"`
	Start    int           `json:"start,omitempty"`
	Count    int           `json:"count,omitempty"`
	Slide    int           `json:"slide,omitempty"`
	To       int           `json:"to,omitempty"`
}
type Result struct {
	SHA256  string   `json:"sha256"`
	Summary string   `json:"summary"`
	Anchors []string `json:"anchors"`
	Issues  []string `json:"issues"`
}

// File requires the hash of the bytes read before editing. Nothing is written on failure.
func File(ctx context.Context, filename, expected string, ops []Operation) (Result, error) {
	if expected == "" {
		return Result{}, fmt.Errorf("expected_sha256 is required")
	}
	if len(ops) == 0 || len(ops) > 100 {
		return Result{}, fmt.Errorf("operations must contain 1..100 entries")
	}
	if e := ctx.Err(); e != nil {
		return Result{}, e
	}
	st, e := os.Stat(filename)
	if e != nil {
		return Result{}, e
	}
	if st.Size() > opc.MaxBytes {
		return Result{}, fmt.Errorf("file too large")
	}
	b, e := os.ReadFile(filename)
	if e != nil {
		return Result{}, e
	}
	if h := opc.Hash(b); h != expected {
		return Result{}, &opc.HashMismatch{Current: h}
	}
	p, e := opc.Open(b)
	if e != nil {
		return Result{}, e
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	summaries := []string{}
	for _, op := range ops {
		if e = ctx.Err(); e != nil {
			return Result{}, e
		}
		switch format {
		case "docx":
			e = word(p, op)
		case "xlsx":
			e = excel(p, op)
		case "pptx":
			e = powerpoint(p, op)
		default:
			e = fmt.Errorf("unsupported format %q", format)
		}
		if e != nil {
			return Result{}, fmt.Errorf("%s: %w", op.Action, e)
		}
		if e = p.CheckLimits(); e != nil {
			return Result{}, e
		}
		summaries = append(summaries, op.Action)
	}
	if e = ctx.Err(); e != nil {
		return Result{}, e
	}
	h, issues, e := opc.WriteAtomic(filename, p, expected, true)
	return Result{SHA256: h, Summary: strings.Join(summaries, ", "), Anchors: Anchors(p, format), Issues: issues}, e
}

// Anchors returns current native paragraph IDs, sheet cell references or slide shape IDs.
func Anchors(p *opc.Package, format string) []string {
	out := []string{}
	switch format {
	case "docx":
		r, e := parse(string(p.Parts["word/document.xml"]))
		if e == nil {
			for i, n := range r.find("p") {
				a := n.attr("paraId")
				if a == "" {
					a = fmt.Sprintf("paragraph:%d", i+1)
				}
				out = append(out, a)
			}
		}
	case "xlsx":
		sheets, _ := sheetList(p)
		for _, sh := range sheets {
			r, e := parse(string(p.Parts[sh.part]))
			if e == nil {
				for _, c := range r.find("c") {
					out = append(out, sh.name+"!"+c.attr("r"))
				}
			}
		}
	case "pptx":
		slides, _ := slideList(p)
		for i, sl := range slides {
			r, e := parse(string(p.Parts[sl.part]))
			if e == nil {
				for _, n := range r.find("cNvPr") {
					if n.attr("id") != "1" {
						out = append(out, fmt.Sprintf("slide:%d/shape:%s", i+1, n.attr("id")))
					}
				}
			}
		}
	}
	return out
}
func word(p *opc.Package, o Operation) error {
	const part = "word/document.xml"
	s := string(p.Parts[part])
	r, e := parse(s)
	if e != nil {
		return e
	}
	paras := r.find("p")
	if o.Action == "append_table" {
		if len(o.Rows) == 0 || len(o.Rows[0]) == 0 {
			return fmt.Errorf("table needs rows and columns")
		}
		for _, row := range o.Rows {
			if len(row) != len(o.Rows[0]) {
				return fmt.Errorf("table rows must have equal widths")
			}
		}
		b := r.find("body")
		if len(b) == 0 {
			return fmt.Errorf("missing body")
		}
		table := create.TableXML(o.Rows, o.Header)
		table = strings.Replace(table, "<w:tbl>", `<w:tbl xmlns:w="`+create.W+`">`, 1)
		sects := b[0].find("sectPr")
		if len(sects) > 0 {
			s = s[:sects[len(sects)-1].start] + table + s[sects[len(sects)-1].start:]
		} else {
			s = insertEnd(s, b[0], table)
		}
		p.Parts[part] = []byte(s)
		return nil
	}
	matches := []*node{}
	for i, n := range paras {
		a := n.attr("paraId")
		if a == "" {
			a = fmt.Sprintf("paragraph:%d", i+1)
		}
		if o.Anchor != "" && a == o.Anchor || o.Anchor == "" && o.Match != "" && strings.Contains(texts(n), o.Match) {
			matches = append(matches, n)
		}
	}
	if len(matches) == 0 {
		return fmt.Errorf("paragraph not found")
	}
	if o.Action != "replace_text" && len(matches) > 1 {
		return fmt.Errorf("ambiguous paragraph; use an anchor")
	}
	switch o.Action {
	case "replace_text":
		if o.Match == "" {
			return fmt.Errorf("match required")
		}
		count := 0
		for i := len(matches) - 1; i >= 0; i-- {
			if !o.All && i != 0 {
				continue
			}
			var n int
			s, n = replaceIn(s, matches[i], o.Match, o.Text, o.All)
			if n < 0 {
				return fmt.Errorf("replacement exceeds 100000 matches in one paragraph")
			}
			count += n
		}
		if count == 0 {
			return fmt.Errorf("text not found")
		}
	case "insert_paragraph":
		n := matches[0]
		idx := n.end
		if o.Position == "before" {
			idx = n.start
		} else if o.Position != "after" && o.Position != "" {
			return fmt.Errorf("position must be before or after")
		}
		id := strings.ToUpper(opc.Hash([]byte(s + o.Text)))[:8]
		for strings.Contains(s, `paraId="`+id+`"`) {
			id = strings.ToUpper(opc.Hash([]byte(id)))[:8]
		}
		fragment := create.ParagraphXML(create.Block{Text: o.Text, Style: o.Style}, id)
		fragment = strings.Replace(fragment, "<w:p ", `<w:p xmlns:w="`+create.W+`" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" `, 1)
		s = s[:idx] + fragment + s[idx:]
	case "delete_paragraph":
		n := matches[0]
		s = s[:n.start] + s[n.end:]
	default:
		return fmt.Errorf("unknown DOCX action %q", o.Action)
	}
	p.Parts[part] = []byte(s)
	return nil
}
