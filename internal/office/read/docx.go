package read

import (
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func heading(s string) int {
	s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
	if s == "title" {
		return 1
	}
	if strings.HasPrefix(s, "heading") {
		n := integer(strings.TrimPrefix(s, "heading"))
		if n >= 1 && n <= 6 {
			return n
		}
	}
	return 0
}
func (a *archive) docx(doc *docextract.Document) error {
	n, err := a.xml("word/document.xml")
	if err != nil {
		return err
	}
	styles := map[string]int{}
	if s, err := a.xml("word/styles.xml"); err == nil {
		for _, v := range s.all("style") {
			level := heading(v.attr("styleId"))
			if h := heading(v.first("name").attr("val")); h > 0 {
				level = h
			}
			if o := v.first("outlineLvl"); o != nil && integer(o.attr("val")) < 6 {
				level = integer(o.attr("val")) + 1
			}
			styles[v.attr("styleId")] = level
		}
	}
	p := docextract.Page{}
	index := 0
	rels := a.rels("word/document.xml")
	var walk func(*node)
	walk = func(v *node) {
		switch v.name {
		case "p":
			index++
			b := paragraph(runText(v), v.attr("paraId"))
			if b.Anchor == "" {
				b.Anchor = fmt.Sprintf("p%d", index)
			}
			pr := v.first("pPr")
			level := heading(pr.first("pStyle").attr("val"))
			if l := styles[pr.first("pStyle").attr("val")]; l > 0 {
				level = l
			}
			if o := pr.first("outlineLvl"); o != nil && integer(o.attr("val")) < 6 {
				level = integer(o.attr("val")) + 1
			}
			if level > 0 {
				b.Kind = docextract.KindHeading
				b.Level = level
			} else if pr.first("numPr") != nil {
				b.Kind = docextract.KindList
				b.Level = min(9, max(1, integer(pr.first("ilvl").attr("val"))+1))
			}
			for _, hint := range []struct{ tag, style string }{{"b", "bold"}, {"i", "italic"}} {
				if x := v.first(hint.tag); x != nil && x.attr("val") != "0" && x.attr("val") != "false" && x.attr("val") != "off" {
					b.Style = append(b.Style, hint.style)
				}
			}
			if b.Text != "" {
				p.Blocks = append(p.Blocks, b)
			}
			for _, draw := range v.all("drawing") {
				p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindImage, Text: draw.first("docPr").attr("descr"), Anchor: b.Anchor, Image: rels[draw.first("blip").attr("embed")]})
			}
			return
		case "tbl":
			var rows [][]string
			for _, r := range v.children {
				if r.name != "tr" {
					continue
				}
				var row []string
				for _, c := range r.children {
					if c.name == "tc" {
						var ps []string
						for _, cp := range c.all("p") {
							ps = append(ps, runText(cp))
						}
						row = append(row, strings.Join(ps, "\n"))
					}
				}
				rows = append(rows, row)
			}
			p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindTable, Text: markdown(rows)})
			return
		}
		for _, c := range v.children {
			walk(c)
		}
	}
	body := n.first("body")
	if body == nil {
		return fmt.Errorf("DOCX has no body")
	}
	walk(body)
	for _, part := range []string{"word/footnotes.xml", "word/endnotes.xml"} {
		if a.files[part] == nil {
			continue
		}
		notes, err := a.xml(part)
		if err != nil {
			return err
		}
		tag := "footnote"
		if strings.Contains(part, "endnotes") {
			tag = "endnote"
		}
		for _, note := range notes.all(tag) {
			if note.attr("type") == "separator" || note.attr("type") == "continuationSeparator" {
				continue
			}
			p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindNote, Text: runText(note), Anchor: tag + ":" + note.attr("id")})
		}
	}
	doc.Pages = []docextract.Page{p}
	return nil
}
