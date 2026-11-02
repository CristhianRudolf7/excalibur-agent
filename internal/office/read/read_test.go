package read

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func fixture(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, s := range parts {
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func extract(t *testing.T, format string, parts map[string]string) docextract.Document {
	t.Helper()
	b := fixture(t, parts)
	doc, err := New(format).Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Paginated || doc.PageCount != len(doc.Pages) {
		t.Fatal(doc)
	}
	return doc
}
func TestDOCX(t *testing.T) {
	d := extract(t, "docx", map[string]string{
		"docProps/core.xml":            `<core><title>Sample</title><creator>Ada</creator></core>`,
		"word/styles.xml":              `<styles><style styleId="custom"><name val="Heading 2"/></style></styles>`,
		"word/document.xml":            `<document xmlns:w14="urn:para"><body><p w14:paraId="AB"><pPr><pStyle val="custom"/></pPr><r><rPr><b/><i/></rPr><t>Heading</t></r></p><p><pPr><numPr><ilvl val="2"/></numPr></pPr><r><t>Item</t></r></p><tbl><tr><tc><p><r><t>Name</t></r></p></tc></tr><tr><tc><p><r><t>Ada</t></r></p></tc></tr></tbl><p><drawing><docPr descr="A cat"/><blip embed="image1"/></drawing></p></body></document>`,
		"word/_rels/document.xml.rels": `<Relationships><Relationship Id="image1" Target="media/cat.png"/></Relationships>`,
		"word/media/cat.png":           "unused image bytes",
		"word/footnotes.xml":           `<footnotes><footnote id="1"><p><r><t>Foot</t></r></p></footnote></footnotes>`,
		"word/endnotes.xml":            `<endnotes><endnote id="1"><p><r><t>End</t></r></p></endnote></endnotes>`,
		"word/header1.xml":             `<p><t>Never include</t></p>`,
	})
	b := d.Pages[0].Blocks
	if d.Title != "Sample" || d.Author != "Ada" || len(d.Outline) != 1 || b[0].Anchor != "AB" || b[0].Level != 2 || len(b[0].Style) != 2 {
		t.Fatal(d)
	}
	if b[1].Kind != docextract.KindList || b[1].Level != 3 || b[1].Anchor != "p2" {
		t.Fatal(b)
	}
	if b[2].Kind != docextract.KindTable || !strings.Contains(b[2].Text, "Ada") {
		t.Fatal(b)
	}
	if b[3].Kind != docextract.KindImage || b[3].Text != "A cat" || b[3].Image != "word/media/cat.png" {
		t.Fatal(b)
	}
	if b[4].Kind != docextract.KindNote || b[5].Text != "End" {
		t.Fatal(b)
	}
}
func TestXLSX(t *testing.T) {
	d := extract(t, "xlsx", map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="urn:rel"><sheets><sheet name="Data" sheetId="1" r:id="r1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="r1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><r><t>Hello </t></r><r><t>world</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c r="A1" t="s"><v>0</v></c><c r="B1" t="inlineStr"><is><t>Inline</t></is></c><c r="C1" t="b"><v>1</v></c></row><row><c r="A2"><f>SUM(B2:C2)</f><v>3.50</v></c><c r="AE201"><v>9</v></c></row></sheetData></worksheet>`,
	})
	b := d.Pages[0].Blocks
	if d.Pages[0].Title != "Data" || b[0].Text != "Hello world" || b[0].Anchor != "Data!A1" || b[1].Text != "Inline" || b[2].Text != "true" || b[3].Style[0] != "formula:=SUM(B2:C2)" || b[3].Text != "3.50" || b[4].Anchor != "Data!AE201" || b[5].Kind != docextract.KindTable {
		t.Fatal(d)
	}
}
func TestPPTX(t *testing.T) {
	d := extract(t, "pptx", map[string]string{
		"ppt/presentation.xml":             `<presentation xmlns:r="urn:rel"><sldIdLst><sldId id="400" r:id="second"/><sldId id="401" r:id="first"/></sldIdLst></presentation>`,
		"ppt/_rels/presentation.xml.rels":  `<Relationships><Relationship Id="first" Target="slides/slide1.xml"/><Relationship Id="second" Target="slides/slide2.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":            `<sld><spTree><sp><cNvPr id="8"/><ph type="title"/><p><t>First file</t></p></sp></spTree></sld>`,
		"ppt/slides/slide2.xml":            `<sld show="0"><spTree><sp><cNvPr id="5"/><ph type="title"/><p><t>First slide</t></p><p><pPr lvl="2"><buChar char="*"/></pPr><t>Bullet</t></p></sp><pic><cNvPr id="6" descr="Chart"/><blip embed="img"/></pic><graphicFrame><cNvPr id="7"/><tbl><tr><tc><p><t>Cell</t></p></tc></tr></tbl></graphicFrame></spTree></sld>`,
		"ppt/slides/_rels/slide2.xml.rels": `<Relationships><Relationship Id="img" Target="../media/image.png"/><Relationship Id="notes" Target="../notesSlides/notesSlide2.xml"/></Relationships>`,
		"ppt/notesSlides/notesSlide2.xml":  `<notes><sp><ph type="body"/><p><t>Speaker</t></p></sp></notes>`,
	})
	p := d.Pages[0]
	if len(d.Pages) != 2 || !strings.HasPrefix(p.Title, "First slide") || len(p.Warnings) != 1 || p.Blocks[0].Anchor != "slide:1/shape:5" || p.Blocks[1].Level != 3 || p.Blocks[2].Image != "ppt/media/image.png" || p.Blocks[3].Kind != docextract.KindTable || p.Blocks[4].Text != "Speaker" {
		t.Fatal(d)
	}
}
func TestODF(t *testing.T) {
	d := extract(t, "odt", map[string]string{"content.xml": `<document><body><text><h outline-level="2">Chapter</h><p>A <span>paragraph</span></p><list><list-item><p>One</p><list><list-item><p>Two</p></list-item></list></list-item></list><table><table-row><table-cell><p>Cell</p></table-cell></table-row></table><note><p>Note</p></note></text></body></document>`})
	b := d.Pages[0].Blocks
	if b[0].Level != 2 || b[1].Text != "A paragraph" || b[3].Level != 2 || b[4].Kind != docextract.KindTable || b[5].Kind != docextract.KindNote {
		t.Fatal(d)
	}
	d = extract(t, "ods", map[string]string{"content.xml": `<document><body><spreadsheet><table name="Sheet"><table-row><table-cell value="3" formula="of:=1+2"/><table-cell number-columns-repeated="2"><p>Shared</p></table-cell></table-row></table></spreadsheet></body></document>`})
	if d.Pages[0].Blocks[0].Style[0] != "formula:=1+2" || d.Pages[0].Blocks[2].Anchor != "Sheet!C1" {
		t.Fatal(d)
	}
	d = extract(t, "odp", map[string]string{"content.xml": `<document><body><presentation><page name="Slide"><frame><p>Text</p></frame><notes><p>Speaker</p></notes></page></presentation></body></document>`})
	if d.Pages[0].Title != "Slide" || d.Pages[0].Blocks[0].Kind != docextract.KindSlideText || d.Pages[0].Blocks[1].Kind != docextract.KindNote {
		t.Fatal(d)
	}
}
func TestEPUB(t *testing.T) {
	d := extract(t, "epub", map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="EPUB/book.opf"/></rootfiles></container>`,
		"EPUB/book.opf":          `<package><metadata><title>Book</title></metadata><manifest><item id="a" href="a.xhtml"/><item id="b" href="b.xhtml"/></manifest><spine><itemref idref="b"/><itemref idref="a"/></spine></package>`,
		"EPUB/a.xhtml":           `<html><body><p>Last</p></body></html>`, "EPUB/b.xhtml": `<html><head><title>Chapter</title></head><body><h1 id="start">Beginning</h1><p>Read <em>me</em></p><script>Ignore</script></body></html>`,
	})
	if d.Title != "Book" || d.Pages[0].Title != "Chapter" || d.Pages[0].Blocks[0].Anchor != "start" || d.Pages[0].Blocks[1].Text != "Read me" || d.Pages[1].Blocks[0].Text != "Last" {
		t.Fatal(d)
	}
}
func TestSafetyAndOptions(t *testing.T) {
	for _, name := range []string{"/absolute", "../parent", "safe/../bad", "C:/bad", "safe\\..\\bad"} {
		t.Run(name, func(t *testing.T) {
			b := fixture(t, map[string]string{name: "x"})
			if _, err := New("docx").Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{}); err == nil {
				t.Fatal("accepted unsafe entry")
			}
		})
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(os.ModeSymlink | 0777)
	w, _ := z.CreateHeader(h)
	_, _ = w.Write([]byte("target"))
	_ = z.Close()
	if _, err := open(context.Background(), bytes.NewReader(b.Bytes()), int64(b.Len())); err == nil {
		t.Fatal("accepted symlink")
	}
	b.Reset()
	z = zip.NewWriter(&b)
	for i := 0; i <= MaxEntries; i++ {
		_, _ = z.CreateHeader(&zip.FileHeader{Name: string(rune(1000 + i)), Method: zip.Store})
	}
	_ = z.Close()
	if _, err := open(context.Background(), bytes.NewReader(b.Bytes()), int64(b.Len())); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := parseXML(context.Background(), strings.NewReader(`<!DOCTYPE x [<!ENTITY a SYSTEM "file:///etc/passwd">]><x>&a;</x>`)); err == nil {
		t.Fatal("entity accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New("docx").Extract(ctx, bytes.NewReader(nil), 0, docextract.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_OFFICE_READ", "off")
	if _, err := New("docx").Extract(context.Background(), bytes.NewReader(nil), 0, docextract.Options{}); err == nil {
		t.Fatal("switch ignored")
	}
}

func TestPageLimitsSelectionAndPartialFailure(t *testing.T) {
	parts := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="urn:rel"><sheet name="Good" r:id="a"/><sheet name="Bad" r:id="b"/></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="a" Target="worksheets/a.xml"/><Relationship Id="b" Target="worksheets/b.xml"/></Relationships>`,
		"xl/worksheets/a.xml":        `<worksheet><c r="A1"><v>1</v></c></worksheet>`, "xl/worksheets/b.xml": `<worksheet>`,
	}
	b := fixture(t, parts)
	_, err := New("xlsx").Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{MaxPages: 1})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	progress := 0
	d, err := New("").Extract(context.Background(), bytes.NewReader(b), int64(len(b)), docextract.Options{Pages: []int{2}, Progress: func(done, total int) {
		progress = done
		if total != 2 {
			t.Error(total)
		}
	}})
	if err != nil || d.PageCount != 2 || len(d.Pages) != 1 || d.Pages[0].Number != 2 || len(d.Pages[0].Warnings) == 0 || progress != 2 {
		t.Fatal(d, err)
	}
}
func TestSpreadsheetCellAndTableLimits(t *testing.T) {
	var s strings.Builder
	s.WriteString(`<worksheet>`)
	for i := 1; i <= MaxCells+1; i++ {
		fmt.Fprintf(&s, `<c r="A%d"><v>%d</v></c>`, i, i)
	}
	s.WriteString(`</worksheet>`)
	d := extract(t, "xlsx", map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="urn:rel"><sheet name="Data!Set" r:id="a"/></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="a" Target="worksheets/a.xml"/></Relationships>`,
		"xl/worksheets/a.xml":        s.String(),
	})
	p := d.Pages[0]
	if len(p.Blocks) != MaxCells+1 || len(p.Warnings) == 0 {
		t.Fatal(len(p.Blocks), p.Warnings)
	}
	table := p.Blocks[MaxCells].Text
	if strings.Count(table, "\n") != 201 || strings.Contains(table, "201 |") || !strings.Contains(table, "200 |") {
		t.Fatal("incorrect table bounds")
	}
}
func TestArchiveSizeAndXMLDepthLimits(t *testing.T) {
	b := fixture(t, map[string]string{"part.xml": "<x/>"})
	idx := bytes.Index(b, []byte{'P', 'K', 1, 2})
	if idx < 0 {
		t.Fatal("missing central directory")
	}
	binary.LittleEndian.PutUint32(b[idx+24:idx+28], uint32(MaxUncompressedSize+1))
	if _, err := open(context.Background(), bytes.NewReader(b), int64(len(b))); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := parseXML(context.Background(), strings.NewReader(strings.Repeat("<x>", 130)+strings.Repeat("</x>", 130))); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := open(context.Background(), bytes.NewReader(nil), -1); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestODTInlineNotesAndImages(t *testing.T) {
	d := extract(t, "odt", map[string]string{"content.xml": `<document><body><text><p>Body<note id="n"><note-body><p>Footnote</p></note-body></note><frame><desc>Alt</desc><image href="Pictures/cat.png"/></frame></p></text></body></document>`})
	b := d.Pages[0].Blocks
	if len(b) != 3 || b[0].Text != "Body" || b[1].Kind != docextract.KindImage || b[1].Image != "Pictures/cat.png" || b[1].Text != "Alt" || b[2].Kind != docextract.KindNote || b[2].Text != "Footnote" {
		t.Fatal(b)
	}
}
