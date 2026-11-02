package read

import (
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func odfText(n *node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for _, v := range n.content {
		switch x := v.(type) {
		case string:
			b.WriteString(x)
		case *node:
			switch x.name {
			case "s":
				count := integer(x.attr("c"))
				if count < 1 {
					count = 1
				}
				if count > 10000 {
					count = 10000
				}
				b.WriteString(strings.Repeat(" ", count))
			case "note":
				continue
			case "frame":
				if x.first("image") == nil {
					b.WriteString(odfText(x))
				}
			case "tab":
				b.WriteByte('\t')
			case "line-break":
				b.WriteByte('\n')
			default:
				b.WriteString(odfText(x))
			}
		}
	}
	return b.String()
}
func (a *archive) odf(doc *docextract.Document) error {
	n, err := a.xml("content.xml")
	if err != nil {
		return err
	}
	if meta, err := a.xml("meta.xml"); err == nil {
		doc.Title = meta.first("title").text()
		doc.Author = meta.first("creator").text()
	}
	if doc.Format == "odt" {
		if n.first("spreadsheet") != nil {
			doc.Format = "ods"
		}
		if n.first("presentation") != nil {
			doc.Format = "odp"
		}
	}
	count := 0
	if doc.Format == "ods" {
		if err := a.checkPages(len(n.first("spreadsheet").all("table"))); err != nil {
			return err
		}
		for _, table := range n.first("spreadsheet").all("table") {
			p := docextract.Page{Title: table.attr("name")}
			rownum := 1
			stopped := false
			for _, row := range table.all("table-row") {
				repeat := integer(row.attr("number-rows-repeated"))
				if repeat < 1 {
					repeat = 1
				}
				if repeat > MaxCells {
					repeat = MaxCells + 1
				}
				for rr := 0; rr < repeat; rr++ {
					col := 1
					for _, cell := range row.children {
						if cell.name != "table-cell" && cell.name != "covered-table-cell" {
							continue
						}
						cr := integer(cell.attr("number-columns-repeated"))
						if cr < 1 {
							cr = 1
						}
						if cr > MaxCells {
							cr = MaxCells + 1
						}
						for cc := 0; cc < cr; cc++ {
							if count >= MaxCells {
								stopped = true
								break
							}
							value := strings.TrimSpace(odfText(cell))
							if value == "" {
								for _, key := range []string{"string-value", "value", "boolean-value", "date-value", "time-value"} {
									if value = cell.attr(key); value != "" {
										break
									}
								}
							}
							b := docextract.Block{Kind: docextract.KindCell, Text: value, Anchor: fmt.Sprintf("%s!%s%d", p.Title, column(col), rownum)}
							if f := cell.attr("formula"); f != "" {
								_, f, _ = strings.Cut(f, ":")
								b.Style = []string{"formula:" + f}
							}
							p.Blocks = append(p.Blocks, b)
							col++
							count++
						}
						if stopped {
							break
						}
					}
					rownum++
					if stopped {
						break
					}
				}
				if stopped {
					break
				}
			}
			if stopped {
				p.Warnings = append(p.Warnings, "cell limit reached (20000)")
			}
			sheetTable(&p)
			doc.Pages = append(doc.Pages, p)
		}
		return nil
	}
	var pages []*node
	if doc.Format == "odp" {
		pages = n.first("presentation").all("page")
	} else {
		pages = []*node{n.first("body")}
	}
	if err := a.checkPages(len(pages)); err != nil {
		return err
	}
	for i, page := range pages {
		p := docextract.Page{Title: page.attr("name")}
		if page.attr("visibility") == "hidden" {
			p.Warnings = append(p.Warnings, "hidden slide")
		}
		index := 0
		var walk func(*node, int)
		walk = func(v *node, depth int) {
			if v == nil {
				return
			}
			switch v.name {
			case "list":
				depth++
			case "frame":
				if doc.Format == "odp" && (v.attr("class") == "title" || v.attr("class") == "subtitle") && v.first("p") != nil {
					p.Title = strings.TrimSpace(odfText(v.first("p")))
				}
				if img := v.first("image"); img != nil {
					p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindImage, Text: v.first("desc").text(), Image: img.attr("href")})
					return
				}
			case "h", "p":
				index++
				b := paragraph(odfText(v), fmt.Sprintf("p%d", index))
				if v.name == "h" {
					b.Kind = docextract.KindHeading
					b.Level = integer(v.attr("outline-level"))
					if b.Level < 1 {
						b.Level = 1
					}
					if b.Level > 6 {
						b.Level = 6
					}
				} else if depth > 0 {
					b.Kind = docextract.KindList
					b.Level = depth
				} else if doc.Format == "odp" {
					b.Kind = docextract.KindSlideText
					b.Anchor = fmt.Sprintf("slide:%d/shape:%d", i+1, index)
				}
				if b.Text != "" {
					p.Blocks = append(p.Blocks, b)
				}
				for _, frame := range v.all("frame") {
					if img := frame.first("image"); img != nil {
						p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindImage, Text: frame.first("desc").text(), Image: img.attr("href")})
					}
				}
				for _, note := range v.all("note") {
					p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindNote, Text: strings.TrimSpace(odfText(note)), Anchor: "note:" + note.attr("id")})
				}
				return
			case "table":
				var rows [][]string
				for _, r := range v.all("table-row") {
					var row []string
					for _, c := range r.children {
						if c.name == "table-cell" {
							row = append(row, strings.TrimSpace(odfText(c)))
						}
					}
					rows = append(rows, row)
				}
				p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindTable, Text: markdown(rows)})
				return
			case "notes", "note":
				p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindNote, Text: strings.TrimSpace(odfText(v))})
				return
			case "image":
				p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindImage, Text: v.first("desc").text(), Image: v.attr("href")})
				return
			}
			for _, c := range v.children {
				walk(c, depth)
			}
		}
		walk(page, 0)
		doc.Pages = append(doc.Pages, p)
	}
	return nil
}
