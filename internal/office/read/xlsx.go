package read

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

const MaxCells = 20000

func column(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}
func coordinates(s string) (int, int) {
	col := 0
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		col = col*26 + int(s[i]-'A'+1)
		i++
	}
	return integer(s[i:]), col
}
func sheetTable(p *docextract.Page) {
	cells := map[[2]int]string{}
	maxr, maxc := 0, 0
	for _, b := range p.Blocks {
		if b.Kind != docextract.KindCell {
			continue
		}
		pos := strings.LastIndex(b.Anchor, "!")
		if pos < 0 {
			continue
		}
		r, c := coordinates(b.Anchor[pos+1:])
		if r < 1 || c < 1 {
			continue
		}
		if r <= 200 && c <= 30 {
			cells[[2]int{r, c}] = b.Text
		}
		if r > 200 {
			r = 200
		}
		if c > 30 {
			c = 30
		}
		if r > maxr {
			maxr = r
		}
		if c > maxc {
			maxc = c
		}
	}
	rows := make([][]string, maxr)
	for r := 1; r <= maxr; r++ {
		rows[r-1] = make([]string, maxc)
		for c := 1; c <= maxc; c++ {
			rows[r-1][c-1] = cells[[2]int{r, c}]
		}
	}
	p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindTable, Text: markdown(rows), Anchor: p.Title + "!table"})
}
func (a *archive) xlsx(doc *docextract.Document) error {
	n, err := a.xml("xl/workbook.xml")
	if err != nil {
		return err
	}
	if err := a.checkPages(len(n.all("sheet"))); err != nil {
		return err
	}
	rels := a.rels("xl/workbook.xml")
	var shared []string
	if a.files["xl/sharedStrings.xml"] != nil {
		s, err := a.xml("xl/sharedStrings.xml")
		if err != nil {
			return err
		}
		for _, v := range s.all("si") {
			shared = append(shared, v.texts("t"))
		}
	}
	count := 0
	for _, sheet := range n.all("sheet") {
		p := docextract.Page{Title: sheet.attr("name")}
		part := rels[sheet.relID()]
		s, err := a.xml(part)
		if err != nil {
			p.Warnings = append(p.Warnings, err.Error())
			doc.Pages = append(doc.Pages, p)
			continue
		}
		for _, c := range s.all("c") {
			if count >= MaxCells {
				p.Warnings = append(p.Warnings, "cell limit reached (20000)")
				break
			}
			value := c.first("v").text()
			switch c.attr("t") {
			case "s":
				i, err := strconv.Atoi(value)
				if err != nil || i < 0 || i >= len(shared) {
					p.Warnings = append(p.Warnings, "invalid shared string index")
					value = ""
				} else {
					value = shared[i]
				}
			case "inlineStr":
				value = c.first("is").texts("t")
			case "b":
				if value == "1" {
					value = "true"
				} else {
					value = "false"
				}
			}
			ref := c.attr("r")
			r, col := coordinates(ref)
			if r < 1 || col < 1 {
				return fmt.Errorf("invalid cell reference %q", ref)
			}
			b := docextract.Block{Kind: docextract.KindCell, Text: value, Anchor: p.Title + "!" + ref}
			if f := c.first("f"); f != nil {
				b.Style = []string{"formula:=" + f.text()}
			}
			p.Blocks = append(p.Blocks, b)
			count++
		}
		sheetTable(&p)
		doc.Pages = append(doc.Pages, p)
	}
	return nil
}
