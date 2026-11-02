package read

import (
	"fmt"
	"path"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func (a *archive) epub(doc *docextract.Document) error {
	c, err := a.xml("META-INF/container.xml")
	if err != nil {
		return err
	}
	part := c.first("rootfile").attr("full-path")
	pkg, err := a.xml(part)
	if err != nil {
		return err
	}
	doc.Title = pkg.first("title").text()
	doc.Author = pkg.first("creator").text()
	items := map[string]string{}
	for _, v := range pkg.all("item") {
		items[v.attr("id")] = path.Join(path.Dir(part), v.attr("href"))
	}
	if err := a.checkPages(len(pkg.all("itemref"))); err != nil {
		return err
	}
	for _, ref := range pkg.all("itemref") {
		chapter, err := a.xml(items[ref.attr("idref")])
		p := docextract.Page{}
		if err != nil {
			p.Warnings = append(p.Warnings, err.Error())
			doc.Pages = append(doc.Pages, p)
			continue
		}
		p.Title = chapter.first("title").text()
		index := 0
		var walk func(*node)
		walk = func(v *node) {
			if v == nil {
				return
			}
			if v.name == "script" || v.name == "style" {
				return
			}
			level := 0
			if len(v.name) == 2 && v.name[0] == 'h' && v.name[1] >= '1' && v.name[1] <= '6' {
				level = int(v.name[1] - '0')
			}
			if v.name == "p" || v.name == "li" || level > 0 {
				index++
				anchor := v.attr("id")
				if anchor == "" {
					anchor = fmt.Sprintf("p%d", index)
				}
				b := paragraph(v.text(), anchor)
				if level > 0 {
					b.Kind = docextract.KindHeading
					b.Level = level
					if p.Title == "" {
						p.Title = b.Text
					}
				}
				if v.name == "li" {
					b.Kind = docextract.KindList
					b.Level = 1
				}
				if strings.TrimSpace(b.Text) != "" {
					p.Blocks = append(p.Blocks, b)
				}
				return
			}
			for _, c := range v.children {
				walk(c)
			}
		}
		walk(chapter.first("body"))
		doc.Pages = append(doc.Pages, p)
	}
	return nil
}
