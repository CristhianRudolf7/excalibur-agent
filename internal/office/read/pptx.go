package read

import (
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func (a *archive) pptx(doc *docextract.Document) error {
	n, err := a.xml("ppt/presentation.xml")
	if err != nil {
		return err
	}
	if err := a.checkPages(len(n.all("sldId"))); err != nil {
		return err
	}
	rels := a.rels("ppt/presentation.xml")
	for i, id := range n.all("sldId") {
		part := rels[id.relID()]
		s, err := a.xml(part)
		p := docextract.Page{}
		if err != nil {
			p.Warnings = append(p.Warnings, err.Error())
			doc.Pages = append(doc.Pages, p)
			continue
		}
		sr := a.rels(part)
		if s.first("sld").attr("show") == "0" || s.first("sld").attr("show") == "false" {
			p.Warnings = append(p.Warnings, "hidden slide")
		}
		tree := s.first("spTree")
		if tree != nil {
			var walk func(*node)
			walk = func(shape *node) {
				anchor := fmt.Sprintf("slide:%d/shape:%s", i+1, shape.first("cNvPr").attr("id"))
				switch shape.name {
				case "sp":
					ph := shape.first("ph").attr("type")
					var title []string
					for _, para := range shape.all("p") {
						b := docextract.Block{Kind: docextract.KindSlideText, Text: runText(para), Anchor: anchor}
						pr := para.first("pPr")
						if pr != nil && pr.first("buNone") == nil && (pr.first("buChar") != nil || pr.first("buAutoNum") != nil || pr.attr("lvl") != "") {
							b.Level = min(9, max(1, integer(pr.attr("lvl"))+1))
						}
						if b.Text != "" {
							p.Blocks = append(p.Blocks, b)
							title = append(title, b.Text)
						}
					}
					if ph == "title" || ph == "ctrTitle" {
						p.Title = strings.Join(title, " ")
					}
					return
				case "pic":
					p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindImage, Text: shape.first("cNvPr").attr("descr"), Anchor: anchor, Image: sr[shape.first("blip").attr("embed")]})
					return
				case "graphicFrame":
					for _, table := range shape.all("tbl") {
						var rows [][]string
						for _, tr := range table.all("tr") {
							var row []string
							for _, tc := range tr.children {
								if tc.name == "tc" {
									row = append(row, runText(tc))
								}
							}
							rows = append(rows, row)
						}
						p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindTable, Text: markdown(rows), Anchor: anchor})
					}
					return
				}
				for _, c := range shape.children {
					walk(c)
				}
			}
			walk(tree)
		}
		for _, rel := range sortedRelationships(sr) {
			if !strings.Contains(rel, "/notesSlides/") || !strings.HasSuffix(rel, ".xml") {
				continue
			}
			notes, err := a.xml(rel)
			if err != nil {
				p.Warnings = append(p.Warnings, err.Error())
				continue
			}
			for _, shape := range notes.all("sp") {
				ph := shape.first("ph").attr("type")
				if ph == "sldNum" || ph == "hdr" || ph == "ftr" || ph == "dt" {
					continue
				}
				for _, para := range shape.all("p") {
					if text := runText(para); text != "" {
						p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindNote, Text: text, Anchor: fmt.Sprintf("slide:%d/notes", i+1)})
					}
				}
			}
		}
		doc.Pages = append(doc.Pages, p)
	}
	return nil
}
