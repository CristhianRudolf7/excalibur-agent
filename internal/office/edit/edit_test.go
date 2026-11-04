package edit

import (
	"bytes"
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepare(t *testing.T, format string, s create.Spec) (string, string) {
	t.Helper()
	p, e := create.Build(format, s, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.Parts["custom/unknown.xml"] = []byte(`<custom a='exact'> untouched </custom>`) // content type defaults cover XML
	f := filepath.Join(t.TempDir(), "test."+format)
	h, _, e := opc.WriteAtomic(f, p, "", false)
	if e != nil {
		t.Fatal(e)
	}
	return f, h
}
func read(t *testing.T, f string) *opc.Package {
	t.Helper()
	b, e := os.ReadFile(f)
	if e != nil {
		t.Fatal(e)
	}
	p, e := opc.Open(b)
	if e != nil {
		t.Fatal(e)
	}
	if issues := p.Validate(); len(issues) > 0 {
		t.Fatal(issues)
	}
	if string(p.Parts["custom/unknown.xml"]) != `<custom a='exact'> untouched </custom>` {
		t.Fatal("unknown changed")
	}
	return p
}
func apply(t *testing.T, f, h string, ops ...Operation) string {
	t.Helper()
	res, e := File(context.Background(), f, h, ops)
	if e != nil {
		t.Fatal(e)
	}
	if len(res.Issues) > 0 || res.Summary == "" || res.SHA256 == h {
		t.Fatal(res)
	}
	return res.SHA256
}
func TestWordEditsPreserveFormatting(t *testing.T) {
	f, h := prepare(t, "docx", create.Spec{Blocks: []create.Block{{Kind: "paragraph", Runs: []create.Run{{Text: "Hello ", Bold: true}, {Text: "world", Italic: true}, {Text: " untouched", Code: true}}}, {Kind: "paragraph", Text: "Second"}}})
	p := read(t, f)
	original := string(p.Parts["word/document.xml"])
	styles := append([]byte(nil), p.Parts["word/styles.xml"]...)
	h = apply(t, f, h, Operation{Action: "replace_text", Match: "lo wor", Text: "p!"})
	p = read(t, f)
	s := string(p.Parts["word/document.xml"])
	if !strings.Contains(s, `<w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Help!</w:t>`) || !strings.Contains(s, `<w:i/></w:rPr><w:t xml:space="preserve">ld</w:t>`) {
		t.Fatal(s)
	}
	if !strings.Contains(s, `<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/>`) || !bytes.Equal(styles, p.Parts["word/styles.xml"]) {
		t.Fatal("unrelated formatting changed")
	}
	if strings.Count(original, "Second") != strings.Count(s, "Second") {
		t.Fatal("unrelated text changed")
	}
	h = apply(t, f, h, Operation{Action: "insert_paragraph", Anchor: "00000002", Position: "before", Text: "Inserted", Style: "Heading2"})
	h = apply(t, f, h, Operation{Action: "delete_paragraph", Match: "Second"}, Operation{Action: "append_table", Rows: [][]string{{"Header"}, {"data"}}, Header: true})
	p = read(t, f)
	s = string(p.Parts["word/document.xml"])
	if strings.Contains(s, "Second") || !strings.Contains(s, "Inserted") || !strings.Contains(s, "tblHeader") {
		t.Fatal(s)
	}
	before, _ := os.ReadFile(f)
	if _, e := File(context.Background(), f, "stale", []Operation{{Action: "delete_paragraph", Match: "Inserted"}}); e == nil || !strings.Contains(e.Error(), h) {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(f)
	if !bytes.Equal(before, after) {
		t.Fatal("stale hash changed file")
	}
}
func TestReplaceAllAcrossRuns(t *testing.T) {
	s := `<w:p xmlns:w="` + create.W + `"><w:r><w:rPr><w:b/></w:rPr><w:t>one one</w:t></w:r><w:r><w:t> one</w:t></w:r></w:p>`
	r, e := parse(s)
	if e != nil {
		t.Fatal(e)
	}
	s, n := replaceIn(s, r.children[0], "one", "two", true)
	if n != 3 || strings.Contains(s, "one") || strings.Count(s, "two") != 3 {
		t.Fatal(s, n)
	}
}
func TestExcelEdits(t *testing.T) {
	f, h := prepare(t, "xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Data", Cells: []create.Cell{{Ref: "A1", Value: "Header", Header: true}, {Ref: "A2", Type: "number", Value: 2}, {Ref: "A3", Type: "number", Value: 3}, {Ref: "B4", Formula: "SUM(A2:A3)"}}}}})
	h = apply(t, f, h, Operation{Action: "set_cell", Sheet: "Data", Cell: create.Cell{Ref: "A1", Formula: "A2+1"}}, Operation{Action: "set_cell", Sheet: "Data", Cell: create.Cell{Ref: "C2", Value: "inserted"}}, Operation{Action: "set_cell", Sheet: "Data", Cell: create.Cell{Ref: "A8", Value: "new row"}})
	p := read(t, f)
	s := string(p.Parts["xl/worksheets/sheet1.xml"])
	if !strings.Contains(s, `r="A1" s="1" xmlns="`+create.S+`"><f>A2+1</f>`) || strings.Contains(s, "<v>3</v></c><c r=\"B4\"") {
		t.Fatal(s)
	}
	h = apply(t, f, h, Operation{Action: "add_sheet", NewSheet: &create.Sheet{Name: "Other", Cells: []create.Cell{{Ref: "A1", Formula: "Data!A8"}}}}, Operation{Action: "rename_sheet", Sheet: "Data", Name: "New Data"})
	p = read(t, f)
	if !strings.Contains(string(p.Parts["xl/worksheets/sheet2.xml"]), "&#39;New Data&#39;!A8") {
		t.Fatal(string(p.Parts["xl/worksheets/sheet2.xml"]))
	}
	apply(t, f, h, Operation{Action: "delete_rows", Sheet: "New Data", Start: 2, Count: 1})
	p = read(t, f)
	s = string(p.Parts["xl/worksheets/sheet1.xml"])
	if !strings.Contains(s, `r="A7"`) || strings.Contains(s, `r="A8"`) || !strings.Contains(s, "#REF!") {
		t.Fatal(s)
	}
	if !strings.Contains(string(p.Parts["xl/worksheets/sheet2.xml"]), "!A7") {
		t.Fatal("cross-sheet formula not shifted")
	}
}
func TestRejectComplexRowsAndAtomicBatch(t *testing.T) {
	f, h := prepare(t, "xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Data", Cells: []create.Cell{{Ref: "A1", Formula: "A2"}}}}})
	p := read(t, f)
	p.Parts["xl/worksheets/sheet1.xml"] = bytes.Replace(p.Parts["xl/worksheets/sheet1.xml"], []byte("<f>"), []byte(`<f t="shared">`), 1)
	h, _, _ = opc.WriteAtomic(f, p, h, true)
	before, _ := os.ReadFile(f)
	_, e := File(context.Background(), f, h, []Operation{{Action: "set_cell", Sheet: "Data", Cell: create.Cell{Ref: "A3", Value: "no partial write"}}, {Action: "delete_rows", Sheet: "Data", Start: 1, Count: 1}})
	if e == nil {
		t.Fatal("shared formula accepted")
	}
	after, _ := os.ReadFile(f)
	if !bytes.Equal(before, after) {
		t.Fatal("partial batch written")
	}
}
func TestPresentationEdits(t *testing.T) {
	f, h := prepare(t, "pptx", create.Spec{Slides: []create.Slide{{Kind: "title", Title: "First", Notes: "Old"}, {Kind: "bullets", Title: "Second", Bullets: []create.Bullet{{Text: "Hello"}}}, {Kind: "title", Title: "Third"}}})
	h = apply(t, f, h, Operation{Action: "replace_text", Anchor: "slide:2/shape:3", Match: "Hello", Text: "World"}, Operation{Action: "set_notes", Slide: 1, Text: "Updated notes"}, Operation{Action: "set_notes", Slide: 2, Text: "New notes"}, Operation{Action: "duplicate_slide", Slide: 1})
	p := read(t, f)
	ss, e := slideList(p)
	if e != nil || len(ss) != 4 {
		t.Fatal(ss, e)
	}
	if !strings.Contains(string(p.Parts[ss[2].part]), "World") {
		t.Fatal("wrong slide edited")
	}
	apply(t, f, h, Operation{Action: "set_notes", Slide: 2, Text: "Duplicate notes"}, Operation{Action: "move_slide", Slide: 4, To: 1}, Operation{Action: "delete_slide", Slide: 3})
	p = read(t, f)
	ss, e = slideList(p)
	if e != nil || len(ss) != 3 || !strings.Contains(string(p.Parts[ss[0].part]), "Third") {
		t.Fatal(ss, e)
	}
	if !strings.Contains(string(p.Parts["ppt/notesSlides/notesSlide1.xml"]), "Updated notes") {
		t.Fatal("duplicated notes were shared")
	}
}
func TestCanceledEdit(t *testing.T) {
	f, h := prepare(t, "docx", create.Spec{Blocks: []create.Block{{Kind: "paragraph", Text: "test"}}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := File(ctx, f, h, []Operation{{Action: "replace_text", Match: "test", Text: "changed"}}); e == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestFormulaRowShiftsRespectFunctionsStringsAndRanges(t *testing.T) {
	cases := map[string]string{
		`sum(a2:a8)`:             `sum(a2:a7)`,
		`LOG10(A8)+A8_Name+"A8"`: `LOG10(A7)+A8_Name+"A8"`,
		`SUM(A2:A8)`:             `SUM(A2:A7)`,
		`SUM(A1:A2)`:             `SUM(A1:A1)`,
		`SUM(A2:A2)`:             `SUM(#REF!)`,
		`'Data'!A2:B8`:           `'Data'!A2:B7`,
		`'Other'!A8+Data!A8`:     `'Other'!A8+Data!A7`,
	}
	for input, want := range cases {
		got := outsideStrings(input, func(s string) string { return shiftReferences(s, "Data", true, 2, 1) })
		if got != want {
			t.Errorf("%s: got %s want %s", input, got, want)
		}
	}
}

func TestSetCellOnPrefixedSpreadsheetKeepsValueNamespace(t *testing.T) {
	p, e := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Data"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.Parts["xl/worksheets/sheet1.xml"] = []byte(`<x:worksheet xmlns:x="` + create.S + `"><x:sheetData><x:row r="1"><x:c r="A1" s="1"><x:v>1</x:v></x:c></x:row></x:sheetData></x:worksheet>`)
	if e = excel(p, Operation{Action: "set_cell", Sheet: "Data", Cell: create.Cell{Ref: "A1", Formula: "1+2"}}); e != nil {
		t.Fatal(e)
	}
	r, e := parse(string(p.Parts["xl/worksheets/sheet1.xml"]))
	if e != nil {
		t.Fatal(e)
	}
	fs := r.find("f")
	if len(fs) != 1 || fs[0].name.Space != create.S {
		t.Fatal("formula lost namespace", fs)
	}
	cs := r.find("c")
	if len(cs) != 1 || cs[0].attr("s") != "1" {
		t.Fatal("cell style lost")
	}
}

func TestSetCellRejectsArrayMembers(t *testing.T) {
	p, e := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Data"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.Parts["xl/worksheets/sheet1.xml"] = []byte(`<worksheet xmlns="` + create.S + `"><sheetData><row r="1"><c r="A1"><f t="array" ref="A1:A3">A4:A6*2</f><v>2</v></c></row></sheetData></worksheet>`)
	if e = excel(p, Operation{Action: "set_cell", Sheet: "Data", Cell: create.Cell{Ref: "A2", Type: "number", Value: 3}}); e == nil {
		t.Fatal("array member overwritten")
	}
}

func TestPresentationReplaceAcrossSplitRuns(t *testing.T) {
	p, e := create.Build("pptx", create.Spec{Slides: []create.Slide{{Kind: "bullets", Title: "Title", Bullets: []create.Bullet{{Text: "Hello"}}}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.Parts["ppt/slides/slide1.xml"] = bytes.Replace(p.Parts["ppt/slides/slide1.xml"], []byte(`<a:t>Hello</a:t>`), []byte(`<a:t>Hel</a:t></a:r><a:r><a:rPr b="1"/><a:t>lo</a:t>`), 1)
	if e = powerpoint(p, Operation{Action: "replace_text", Anchor: "slide:1/shape:3", Match: "Hello", Text: "World"}); e != nil {
		t.Fatal(e)
	}
	s := string(p.Parts["ppt/slides/slide1.xml"])
	if !strings.Contains(s, `<a:t>World</a:t>`) || !strings.Contains(s, `<a:rPr b="1"/><a:t></a:t>`) {
		t.Fatal(s)
	}
}

func TestRowDeletionRejectsTables(t *testing.T) {
	p, e := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Data"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.Parts["xl/worksheets/sheet1.xml"] = []byte(`<worksheet xmlns="` + create.S + `"><sheetData/><tableParts count="1"><tablePart xmlns:r="` + create.R + `" r:id="table"/></tableParts></worksheet>`)
	if e = excel(p, Operation{Action: "delete_rows", Sheet: "Data", Start: 2, Count: 1}); e == nil {
		t.Fatal("table-bearing sheet accepted")
	}
}

func TestNotesCanBeClearedThenReplaced(t *testing.T) {
	p, e := create.Build("pptx", create.Spec{Slides: []create.Slide{{Kind: "title", Title: "Title", Notes: "Original"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{"", "Replacement"} {
		if e = powerpoint(p, Operation{Action: "set_notes", Slide: 1, Text: text}); e != nil {
			t.Fatal(e)
		}
	}
	r, e := parse(string(p.Parts["ppt/notesSlides/notesSlide1.xml"]))
	if e != nil {
		t.Fatal(e)
	}
	ts := r.find("t")
	if len(ts) != 1 || ts[0].content() != "Replacement" {
		t.Fatal(string(p.Parts["ppt/notesSlides/notesSlide1.xml"]))
	}
}

func TestSetNotesAddsMasterToPreviouslyNoteFreeDeck(t *testing.T) {
	p := opc.New()
	p.Parts["ppt/presentation.xml"] = []byte(`<p:presentation xmlns:p="` + create.P + `" xmlns:r="` + create.R + `"><p:sldIdLst><p:sldId id="256" r:id="slide1"/></p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`)
	p.Parts["ppt/slides/slide1.xml"] = []byte(`<p:sld xmlns:p="` + create.P + `"><p:cSld><p:spTree/></p:cSld></p:sld>`)
	p.Types(map[string]string{"ppt/presentation.xml": "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", "ppt/slides/slide1.xml": "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"})
	p.SetRelationships("_rels/.rels", []opc.Relationship{{ID: "root", Type: opc.OfficeRel + "officeDocument", Target: "ppt/presentation.xml"}})
	p.SetRelationships("ppt/_rels/presentation.xml.rels", []opc.Relationship{{ID: "slide1", Type: opc.OfficeRel + "slide", Target: "slides/slide1.xml"}})
	if e := powerpoint(p, Operation{Action: "set_notes", Slide: 1, Text: "New notes"}); e != nil {
		t.Fatal(e)
	}
	if issues := p.Validate(); len(issues) > 0 {
		t.Fatal(issues)
	}
	if !strings.Contains(string(p.Parts["ppt/presentation.xml"]), "notesMasterIdLst") {
		t.Fatal("missing notes master metadata")
	}
	rels, e := p.Relationships("ppt/notesSlides/_rels/notesSlide1.xml.rels")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, r := range rels {
		if strings.HasSuffix(r.Type, "/notesMaster") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing notes master relationship")
	}
}

func TestReplaceAllUsesOriginalOffsetsAcrossMultipleRuns(t *testing.T) {
	s := `<p><r><t>ab a</t></r><r><t>b ab</t></r></p>`
	r, e := parse(s)
	if e != nil {
		t.Fatal(e)
	}
	got, n := replaceIn(s, r.children[0], "ab", "LONG", true)
	if n != 3 {
		t.Fatal(n)
	}
	root, e := parse(got)
	if e != nil || texts(root) != "LONG LONG LONG" {
		t.Fatal(got, e)
	}
	s = `<p><t>` + strings.Repeat("a", 100001) + `</t></p>`
	r, e = parse(s)
	if e != nil {
		t.Fatal(e)
	}
	if _, n = replaceIn(s, r.children[0], "a", "b", true); n != -1 {
		t.Fatal("replacement budget not enforced", n)
	}
}

func TestSheetEditsPreserveOpaqueFormulaLikeXML(t *testing.T) {
	p, e := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Data", Cells: []create.Cell{{Ref: "A8", Value: "value"}}}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	opaque := []byte(`<custom xmlns="` + create.S + `"><f>Data!A8</f><definedName>Data!A8</definedName></custom>`)
	p.Parts["custom/opaque.xml"] = append([]byte(nil), opaque...)
	if e = excel(p, Operation{Action: "rename_sheet", Sheet: "Data", Name: "Renamed"}); e != nil {
		t.Fatal(e)
	}
	if e = excel(p, Operation{Action: "delete_rows", Sheet: "Renamed", Start: 2, Count: 1}); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(p.Parts["custom/opaque.xml"], opaque) {
		t.Fatal("opaque part changed")
	}
}

func TestThemedPresentationEditsPreserveLayout(t *testing.T) {
	f, h := prepare(t, "pptx", create.Spec{Theme: create.Theme{Style: "dark", Accent: "#14B8A6"}, Slides: []create.Slide{
		{Kind: "section", Title: "Chapter", Subtitle: "Before"},
		{Kind: "cards", Title: "Results", Cards: []create.Card{{Title: "Tests", Text: "All passing", Value: "38"}, {Title: "Platforms", Text: "One experience", Value: "3"}}, Notes: "Original notes"},
		{Kind: "quote", Title: "Feedback", Quote: "A clearer workflow", Attribution: "Customer"},
	}})
	before := read(t, f)
	h = apply(t, f, h,
		Operation{Action: "replace_text", Anchor: "slide:1/shape:2", Match: "Chapter", Text: "Next chapter"},
		Operation{Action: "replace_text", Anchor: "slide:1/shape:3", Match: "Before", Text: "After"},
		Operation{Action: "replace_text", Anchor: "slide:2/shape:3", Match: "38", Text: "39"},
		Operation{Action: "replace_text", Anchor: "slide:2/shape:4", Match: "Platforms", Text: "Systems"},
		Operation{Action: "replace_text", Anchor: "slide:3/shape:3", Match: "clearer", Text: "simpler"},
		Operation{Action: "set_notes", Slide: 2, Text: "Updated notes"},
	)
	after := read(t, f)
	for part, raw := range before.Parts {
		want := string(raw)
		switch part {
		case "ppt/slides/slide1.xml":
			want = strings.Replace(want, ">Chapter<", ">Next chapter<", 1)
			want = strings.Replace(want, ">Before<", ">After<", 1)
		case "ppt/slides/slide2.xml":
			want = strings.Replace(want, ">38<", ">39<", 1)
			want = strings.Replace(want, ">Platforms<", ">Systems<", 1)
		case "ppt/slides/slide3.xml":
			want = strings.Replace(want, "clearer", "simpler", 1)
		case "ppt/notesSlides/notesSlide2.xml":
			want = strings.Replace(want, "Original notes", "Updated notes", 1)
		}
		if string(after.Parts[part]) != want {
			t.Fatal("edit changed unrelated layout or package bytes", part)
		}
	}
	apply(t, f, h, Operation{Action: "duplicate_slide", Slide: 2}, Operation{Action: "set_notes", Slide: 3, Text: "Independent notes"})
	p := read(t, f)
	if !strings.Contains(string(p.Parts["ppt/notesSlides/notesSlide2.xml"]), "Updated notes") {
		t.Fatal("duplicated themed slide shared notes")
	}
}
