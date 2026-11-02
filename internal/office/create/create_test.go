package create

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
)

func TestCreateRoundTrip(t *testing.T) {
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	load := func(string) ([]byte, error) { return img.Bytes(), nil }
	cases := []struct {
		format     string
		s          Spec
		part, text string
	}{
		{"docx", Spec{Title: "Report", Blocks: []Block{{Kind: "heading", Level: 1, Text: "Heading"}, {Kind: "paragraph", Runs: []Run{{Text: "Bold", Bold: true}, {Text: " Italic", Italic: true}, {Text: "code", Code: true}}}, {Kind: "bullet", Text: "bullet"}, {Kind: "numbered", Text: "number"}, {Kind: "table", Rows: [][]string{{"Header"}, {"cell"}}, Header: true}, {Kind: "page_break"}, {Kind: "image", Image: "img.png"}}}, "word/document.xml", "Heading"},
		{"xlsx", Spec{Sheets: []Sheet{{Name: "Data", FreezeFirstRow: true, ColumnWidths: []float64{20}, Cells: []Cell{{Ref: "A1", Value: "Header", Header: true}, {Ref: "A2", Type: "number", Value: 42}, {Ref: "B2", Type: "bool", Value: true}, {Ref: "C2", Type: "date", Value: "2026-11-02"}, {Ref: "D2", Formula: "SUM(A2:A3)"}}}}}, "xl/workbook.xml", "fullCalcOnLoad"},
		{"pptx", Spec{Slides: []Slide{{Kind: "title", Title: "Title"}, {Kind: "bullets", Title: "Bullet title", Bullets: []Bullet{{Text: "Level one", Level: 1}}, Notes: "Speaker notes"}, {Kind: "two_column", Left: []Bullet{{Text: "left"}}, Right: []Bullet{{Text: "right"}}}, {Kind: "image", Image: "img.png"}, {Kind: "table", Rows: [][]string{{"Header"}, {"Cell"}}}}}, "ppt/slides/slide2.xml", "Level one"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			p, e := Build(tc.format, tc.s, load)
			if e != nil {
				t.Fatal(e)
			}
			if issues := p.Validate(); len(issues) > 0 {
				t.Fatal(issues)
			}
			b, e := p.Bytes()
			if e != nil {
				t.Fatal(e)
			}
			z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, f := range z.File {
				r, _ := f.Open()
				data, _ := io.ReadAll(r)
				_ = r.Close()
				if f.Name == tc.part {
					found = strings.Contains(string(data), tc.text)
				}
				if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
					d := xml.NewDecoder(bytes.NewReader(data))
					for {
						_, e := d.Token()
						if e == io.EOF {
							break
						}
						if e != nil {
							t.Fatal(f.Name, e)
						}
					}
				}
			}
			if !found {
				t.Fatal("missing expected content")
			}
		})
	}
}
func TestInvalidSpecs(t *testing.T) {
	for _, tc := range []struct {
		f string
		s Spec
	}{{"docx", Spec{Blocks: []Block{{Kind: "heading", Level: 4}}}}, {"xlsx", Spec{Sheets: []Sheet{{Name: "bad/name"}}}}, {"xlsx", Spec{Sheets: []Sheet{{Name: "ok", Cells: []Cell{{Ref: "XFE1", Value: "bad"}}}}}}, {"pptx", Spec{Slides: []Slide{{Kind: "unknown"}}}}} {
		if _, e := Build(tc.f, tc.s, nil); e == nil {
			t.Fatal(tc.f, "invalid accepted")
		}
	}
}

func TestCombinedImageBudget(t *testing.T) {
	var small bytes.Buffer
	_ = png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	large := make([]byte, 16<<20)
	copy(large, small.Bytes())
	calls := 0
	_, e := Build("docx", Spec{Blocks: []Block{{Kind: "image", Image: "a"}, {Kind: "image", Image: "b"}, {Kind: "image", Image: "c"}, {Kind: "image", Image: "d"}}}, func(string) ([]byte, error) { calls++; return large, nil })
	if e == nil || !strings.Contains(e.Error(), "combined images") || calls != 3 {
		t.Fatal(e, calls)
	}
}

func TestGeneratedOfficeFeatures(t *testing.T) {
	doc, e := Build("docx", Spec{Page: PageSetup{Size: "Letter", MarginTwips: 720}, Blocks: []Block{{Kind: "heading", Level: 2, Text: "Subheading"}, {Kind: "heading", Level: 3, Text: "Detail"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	text := string(doc.Parts["word/document.xml"])
	for _, want := range []string{`w:val="Heading2"`, `w:val="Heading3"`, `w:w="12240" w:h="15840"`, `w:top="720"`} {
		if !strings.Contains(text, want) {
			t.Fatal("Word feature missing", want)
		}
	}
	numbering := string(doc.Parts["word/numbering.xml"])
	if strings.LastIndex(numbering, "</w:abstractNum>") > strings.Index(numbering, "<w:num w:numId=") {
		t.Fatal("numbering schema order is invalid")
	}
	book, e := Build("xlsx", Spec{Sheets: []Sheet{{Name: "Data", ColumnWidths: []float64{24}, FreezeFirstRow: true, Cells: []Cell{{Ref: "A1", Value: "Header", Header: true}, {Ref: "A2", Type: "number", Value: 12.5}, {Ref: "B2", Type: "bool", Value: false}, {Ref: "C2", Type: "date", Value: "2026-11-02"}, {Ref: "D2", Formula: "A2*2"}}}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	text = string(book.Parts["xl/worksheets/sheet1.xml"])
	for _, want := range []string{`state="frozen"`, `width="24"`, `s="1" t="inlineStr"`, `<v>12.5</v>`, `t="b"><v>0</v>`, `t="d"><v>2026-11-02</v>`, `<f>A2*2</f></c>`} {
		if !strings.Contains(text, want) {
			t.Fatal("spreadsheet feature missing", want, text)
		}
	}
	deck, e := Build("pptx", Spec{Slides: []Slide{{Kind: "two_column", Title: "Compare", Left: []Bullet{{Text: "Before"}}, Right: []Bullet{{Text: "After"}}, Notes: "Speaker"}, {Kind: "table", Rows: [][]string{{"Column"}, {"Value"}}}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	for part, wants := range map[string][]string{"ppt/presentation.xml": {`cx="12192000" cy="6858000"`}, "ppt/slides/slide1.xml": {"Before", "After"}, "ppt/slides/slide2.xml": {"<a:tbl>", "Column", "Value"}, "ppt/notesSlides/notesSlide1.xml": {"Speaker"}, "ppt/theme/theme1.xml": {"<a:theme"}, "ppt/slideMasters/slideMaster1.xml": {"<p:sldMaster"}} {
		for _, want := range wants {
			if !strings.Contains(string(deck.Parts[part]), want) {
				t.Fatal("presentation feature missing", part, want)
			}
		}
	}
}
