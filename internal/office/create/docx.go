package create

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
)

// ParagraphXML emits a standalone Word paragraph fragment with an optional paraId.
func ParagraphXML(b Block, id string) string {
	var s strings.Builder
	s.WriteString(`<w:p`)
	if id != "" {
		s.WriteString(` w14:paraId="` + esc(id) + `"`)
	}
	s.WriteString(`>`)
	style := b.Style
	if b.Kind == "heading" {
		style = fmt.Sprintf("Heading%d", b.Level)
	}
	props := ""
	if style != "" {
		props += `<w:pStyle w:val="` + esc(style) + `"/>`
	}
	if b.Kind == "bullet" || b.Kind == "numbered" {
		n := 1
		if b.Kind == "numbered" {
			n = 2
		}
		props += fmt.Sprintf(`<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, b.Level, n)
	}
	if props != "" {
		s.WriteString(`<w:pPr>` + props + `</w:pPr>`)
	}
	runs := b.Runs
	if len(runs) == 0 {
		runs = []Run{{Text: b.Text}}
	}
	for _, r := range runs {
		s.WriteString(`<w:r>`)
		p := ""
		if r.Bold {
			p += `<w:b/>`
		}
		if r.Italic {
			p += `<w:i/>`
		}
		if r.Code {
			p += `<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/>`
		}
		if p != "" {
			s.WriteString(`<w:rPr>` + p + `</w:rPr>`)
		}
		s.WriteString(`<w:t xml:space="preserve">` + esc(r.Text) + `</w:t></w:r>`)
	}
	s.WriteString(`</w:p>`)
	return s.String()
}

// TableXML creates a basic Word table, including a repeatable bold header row.
func TableXML(rows [][]string, header bool) string {
	var s strings.Builder
	s.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr>`)
	s.WriteString(`<w:tblGrid>`)
	if len(rows) > 0 {
		for range rows[0] {
			s.WriteString(`<w:gridCol w:w="2400"/>`)
		}
	}
	s.WriteString(`</w:tblGrid>`)
	for i, row := range rows {
		s.WriteString(`<w:tr>`)
		if i == 0 && header {
			s.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for _, cell := range row {
			s.WriteString(`<w:tc><w:tcPr><w:tcW w:w="2400" w:type="dxa"/></w:tcPr>` + ParagraphXML(Block{Runs: []Run{{Text: cell, Bold: i == 0 && header}}}, "") + `</w:tc>`)
		}
		s.WriteString(`</w:tr>`)
	}
	s.WriteString(`</w:tbl>`)
	return s.String()
}
func imageData(load ImageLoader, name string) ([]byte, string, error) {
	b, e := load(name)
	if e != nil {
		return nil, "", e
	}
	if len(b) > 16<<20 {
		return nil, "", fmt.Errorf("image exceeds 16 MiB")
	}
	cfg, kind, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || (kind != "png" && kind != "jpeg") || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 20000 || cfg.Height > 20000 {
		return nil, "", fmt.Errorf("image must be bounded PNG or JPEG")
	}
	if kind == "jpeg" {
		kind = "jpg"
	}
	return b, kind, nil
}
func docx(s Spec, load ImageLoader) (*opc.Package, error) {
	p := opc.New()
	root(p, "word/document.xml")
	types := map[string]string{"word/document.xml": "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "word/styles.xml": "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml", "word/numbering.xml": "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"}
	rels := []opc.Relationship{relation("styles", "styles", "styles.xml"), relation("numbering", "numbering", "numbering.xml")}
	styles := `<w:styles xmlns:w="` + W + `"><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:rPr><w:b/><w:sz w:val="40"/></w:rPr></w:style>`
	for i := 1; i <= 3; i++ {
		styles += fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr><w:b/><w:sz w:val="%d"/></w:rPr></w:style>`, i, i, i-1, 36-i*4)
	}
	p.Parts["word/styles.xml"] = []byte(styles + `</w:styles>`)
	numbering := `<w:numbering xmlns:w="` + W + `">`
	for n := 1; n <= 2; n++ {
		numbering += fmt.Sprintf(`<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="multilevel"/>`, n)
		for level := 0; level < 9; level++ {
			format, text := "bullet", "•"
			if n == 2 {
				format = "decimal"
				text = fmt.Sprintf("%%%d.", level+1)
			}
			numbering += fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`, level, format, text, (level+1)*720)
		}
		numbering += `</w:abstractNum>`
	}
	for n := 1; n <= 2; n++ {
		numbering += fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="%d"/></w:num>`, n, n)
	}
	p.Parts["word/numbering.xml"] = []byte(numbering + `</w:numbering>`)
	var body strings.Builder
	blocks := s.Blocks
	if s.Title != "" {
		blocks = append([]Block{{Kind: "paragraph", Text: s.Title, Style: "Title"}}, blocks...)
	}
	for i, b := range blocks {
		id := fmt.Sprintf("%08X", i+1)
		switch b.Kind {
		case "paragraph", "heading", "bullet", "numbered", "":
			if b.Kind == "heading" && (b.Level < 1 || b.Level > 3) {
				return nil, fmt.Errorf("heading level must be 1..3")
			}
			if b.Level < 0 || b.Level > 8 {
				return nil, fmt.Errorf("list level must be 0..8")
			}
			body.WriteString(ParagraphXML(b, id))
		case "table":
			if len(b.Rows) == 0 || len(b.Rows[0]) == 0 {
				return nil, fmt.Errorf("table needs rows and columns")
			}
			for _, row := range b.Rows {
				if len(row) != len(b.Rows[0]) {
					return nil, fmt.Errorf("table rows must have equal widths")
				}
			}
			body.WriteString(TableXML(b.Rows, b.Header))
		case "page_break":
			body.WriteString(`<w:p w14:paraId="` + id + `"><w:r><w:br w:type="page"/></w:r></w:p>`)
		case "image":
			data, ext, e := imageData(load, b.Image)
			if e != nil {
				return nil, e
			}
			rid := fmt.Sprintf("image%d", i+1)
			p.Parts["word/media/"+rid+"."+ext] = data
			rels = append(rels, relation(rid, "image", "media/"+rid+"."+ext))
			body.WriteString(fmt.Sprintf(`<w:p w14:paraId="%s"><w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><wp:extent cx="5486400" cy="3657600"/><wp:docPr id="%d" name="Image"/><a:graphic xmlns:a="%s"><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr><pic:cNvPr id="0" name="Image"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="5486400" cy="3657600"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`, id, i+1, A, rid))
		default:
			return nil, fmt.Errorf("unknown block kind %q", b.Kind)
		}
	}
	width, height := 11906, 16838
	switch strings.ToLower(s.Page.Size) {
	case "letter":
		width, height = 12240, 15840
	case "", "a4":
	default:
		return nil, fmt.Errorf("page size must be A4 or Letter")
	}
	margin := s.Page.MarginTwips
	if margin == 0 {
		margin = 1440
	}
	if margin < 0 || margin*2 >= width {
		return nil, fmt.Errorf("invalid page margin")
	}
	body.WriteString(fmt.Sprintf(`<w:sectPr><w:pgSz w:w="%d" w:h="%d"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>`, width, height, margin, margin, margin, margin))
	p.Parts["word/document.xml"] = []byte(`<w:document xmlns:w="` + W + `" xmlns:r="` + R + `" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"><w:body>` + body.String() + `</w:body></w:document>`)
	p.SetRelationships("word/_rels/document.xml.rels", rels)
	p.Types(types)
	return p, nil
}
