package create

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"unicode"
)

const (
	slideWidth    = 12192000
	slideHeight   = 6858000
	contentX      = 731520 // 0.8 inch margins.
	contentWidth  = slideWidth - 2*contentX
	contentY      = 1737360
	contentHeight = 4160520
)

type palette struct {
	accent, background, text, muted, card, border, tint, accentText, onAccent, header string
}

func presentationPalette(t Theme) (palette, error) {
	p := palette{accent: "6366F1", background: "F8FAFC", text: "0F172A", muted: "64748B", card: "FFFFFF", border: "E2E8F0"}
	if t.Style != "" && t.Style != "light" && t.Style != "dark" {
		return p, fmt.Errorf("theme style must be light or dark")
	}
	if t.Accent != "" {
		if len(t.Accent) != 7 || t.Accent[0] != '#' {
			return p, fmt.Errorf("theme accent must be #RRGGBB")
		}
		if _, err := strconv.ParseUint(t.Accent[1:], 16, 24); err != nil {
			return p, fmt.Errorf("theme accent must be #RRGGBB")
		}
		p.accent = strings.ToUpper(t.Accent[1:])
	}
	if t.Style == "dark" {
		p.background, p.text, p.muted = "0F172A", "F8FAFC", "94A3B8"
		p.card, p.border = "1E293B", "334155"
	}
	p.tint = mixColor(p.accent, p.card, 0.93)
	p.accentText = readableColor(p.accent, p.card)
	p.onAccent = "FFFFFF"
	if contrast(p.accent, p.onAccent) < 4.5 {
		p.onAccent = "0F172A"
	}
	p.header = readableColor(p.accent, "FFFFFF")
	return p, nil
}

func rgb(s string) [3]float64 {
	v, _ := strconv.ParseUint(s, 16, 24)
	return [3]float64{float64(v >> 16), float64((v >> 8) & 255), float64(v & 255)}
}

func mixColor(a, b string, amount float64) string {
	x, y := rgb(a), rgb(b)
	return fmt.Sprintf("%02X%02X%02X", int(math.Round(x[0]*(1-amount)+y[0]*amount)), int(math.Round(x[1]*(1-amount)+y[1]*amount)), int(math.Round(x[2]*(1-amount)+y[2]*amount)))
}

func luminance(s string) float64 {
	c := rgb(s)
	for i, v := range c {
		v /= 255
		if v <= 0.04045 {
			c[i] = v / 12.92
		} else {
			c[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return c[0]*0.2126 + c[1]*0.7152 + c[2]*0.0722
}

func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}

func readableColor(color, background string) string {
	target := "0F172A"
	if luminance(background) < 0.4 {
		target = "FFFFFF"
	}
	for contrast(color, background) < 4.5 && color != target {
		color = mixColor(color, target, 0.12)
	}
	return color
}

func solid(color string) string {
	return `<a:solidFill><a:srgbClr val="` + color + `"/></a:solidFill>`
}

type slideBox struct{ x, y, w, h int }
type slideParagraph struct {
	text, color  string
	size, level  int // Size in hundredths of a point.
	bold, bullet bool
	gap          int
}

func paragraphXML(p slideParagraph) string {
	var s strings.Builder
	margin, indent := 0, 0
	if p.bullet {
		margin, indent = 285750+p.level*228600, -228600
	}
	s.WriteString(fmt.Sprintf(`<a:p><a:pPr lvl="%d" marL="%d" indent="%d"><a:lnSpc><a:spcPct val="125000"/></a:lnSpc><a:spcAft><a:spcPts val="%d"/></a:spcAft>`, p.level, margin, indent, p.gap))
	if p.bullet {
		s.WriteString(`<a:buClr><a:srgbClr val="` + p.color + `"/></a:buClr><a:buSzPct val="85000"/><a:buFont typeface="Calibri"/><a:buChar char="•"/>`)
	} else {
		s.WriteString(`<a:buNone/>`)
	}
	s.WriteString(`</a:pPr>`)
	bold := "0"
	if p.bold {
		bold = "1"
	}
	for i, line := range strings.Split(p.text, "\n") {
		if i > 0 {
			s.WriteString(`<a:br/>`)
		}
		space := ""
		if strings.TrimSpace(line) != line {
			space = ` xml:space="preserve"`
		}
		s.WriteString(fmt.Sprintf(`<a:r><a:rPr lang="en-US" sz="%d" b="%s">%s<a:latin typeface="Calibri"/></a:rPr><a:t%s>%s</a:t></a:r>`, p.size, bold, solid(p.color), space, esc(line)))
	}
	s.WriteString(fmt.Sprintf(`<a:endParaRPr lang="en-US" sz="%d"/></a:p>`, p.size))
	return s.String()
}

// textBox keeps text and its card surface in the same shape, preserving edit
// anchors and avoiding decorations covering text in PowerPoint's paint order.
func textBox(id int, name string, box slideBox, paras []slideParagraph, fill, border, anchor string, padding int) string {
	var s strings.Builder
	geometry := "rect"
	if fill != "" {
		geometry = "roundRect"
	}
	s.WriteString(fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s"><a:avLst><a:gd name="adj" fmla="val 6000"/></a:avLst></a:prstGeom>`, id, esc(name), box.x, box.y, box.w, box.h, geometry))
	if fill == "" {
		s.WriteString(`<a:noFill/>`)
	} else {
		s.WriteString(solid(fill))
	}
	if border == "" {
		s.WriteString(`<a:ln><a:noFill/></a:ln>`)
	} else {
		s.WriteString(`<a:ln w="9525">` + solid(border) + `</a:ln>`)
	}
	s.WriteString(fmt.Sprintf(`</p:spPr><p:txBody><a:bodyPr wrap="square" lIns="%d" tIns="%d" rIns="%d" bIns="%d" anchor="%s"><a:normAutofit/></a:bodyPr><a:lstStyle/>`, padding, padding, padding, padding, anchor))
	if len(paras) == 0 {
		paras = []slideParagraph{{size: 2400, color: "0F172A"}}
	}
	for _, p := range paras {
		s.WriteString(paragraphXML(p))
	}
	s.WriteString(`</p:txBody></p:sp>`)
	return s.String()
}

// textLines conservatively estimates wrapping in Calibri and its common
// substitutes, including explicit newlines, wide Unicode and unbroken tokens.
func textLines(text string, width, size int) int {
	capacity := float64(width) / float64(max(size, 1)*127)
	capacity = math.Max(capacity, 0.5)
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		used := 0.0
		lines++
		for _, word := range strings.Fields(line) {
			w := 0.0
			for _, r := range word {
				switch {
				case r >= 0x2e80:
					w += 1.05
				case strings.ContainsRune("MW@%", r):
					w += 0.95
				case unicode.IsUpper(r):
					w += 0.7
				case strings.ContainsRune("il.,:;'!|", r):
					w += 0.3
				default:
					w += 0.56
				}
			}
			if used > 0 {
				if used+0.3+w > capacity {
					lines++
					used = 0
				} else {
					used += 0.3
				}
			}
			for w > capacity {
				lines++
				w -= capacity
			}
			used += w
		}
	}
	return lines
}

func paragraphsHeight(paras []slideParagraph, width int) int {
	height := 0
	for i, p := range paras {
		w := width
		if p.bullet {
			w -= 285750 + p.level*228600
		}
		height += int(math.Ceil(float64(textLines(p.text, w, p.size)*p.size) * 127 * 1.25))
		if i < len(paras)-1 {
			height += p.gap * 127
		}
	}
	return height
}

func fitParagraphs(paras []slideParagraph, box slideBox, padding, minimum int) ([]slideParagraph, error) {
	// Scale the whole group together so headings, metrics and body retain their
	// hierarchy. Leave 5% vertical slack for font substitution differences.
	atScale := func(scale int) []slideParagraph {
		out := append([]slideParagraph(nil), paras...)
		for i := range out {
			out[i].size = paras[i].size * scale / 100
			out[i].gap = paras[i].gap * scale / 100
		}
		return out
	}
	fits := func(out []slideParagraph) bool {
		return paragraphsHeight(out, box.w-2*padding) <= (box.h-2*padding)*95/100
	}
	if fits(paras) {
		return paras, nil
	}
	low, high := 1, 99
	for _, p := range paras {
		low = max(low, (minimum*100+p.size-1)/p.size)
	}
	var best []slideParagraph
	// Fit is monotonic as the entire group shrinks. Binary search bounds the
	// work even for a spec containing megabytes of text that cannot fit.
	for low <= high {
		scale := (low + high) / 2
		out := atScale(scale)
		if fits(out) {
			best, low = out, scale+1
		} else {
			high = scale - 1
		}
	}
	if best == nil {
		return nil, fmt.Errorf("text cannot fit on slide; shorten it or move detail to notes")
	}
	return best, nil
}

func bulletParagraphs(items []Bullet, p palette, heading bool) []slideParagraph {
	out := make([]slideParagraph, 0, len(items))
	headingIndex := -1
	if heading {
		for i, b := range items {
			if b.Level == 0 {
				headingIndex = i
				break
			}
		}
	}
	for i, b := range items {
		r := slideParagraph{text: b.Text, size: 2400, color: p.text, bullet: true, level: b.Level, gap: 1300}
		if b.Level > 0 {
			r.size, r.color = 2100, p.muted
		}
		if i == headingIndex {
			r.size, r.color, r.bold, r.bullet, r.gap = 2500, p.accentText, true, false, 1800
		}
		out = append(out, r)
	}
	return out
}

func accentBar(id int, box slideBox, color string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Accent"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom>%s<a:ln><a:noFill/></a:ln></p:spPr></p:sp>`, id, box.x, box.y, box.w, box.h, solid(color))
}

func renderSlide(slide Slide, number int, p palette, load ImageLoader) (shapes, background string, data []byte, ext string, err error) {
	background = p.background
	var s strings.Builder
	titleBox := slideBox{contentX, 502920, contentWidth, 914400}
	title := []slideParagraph{{text: slide.Title, size: 3400, color: p.text, bold: true}}
	if slide.Kind == "title" || slide.Kind == "section" {
		background = "0F172A"
		color := "FFFFFF"
		if slide.Kind == "section" {
			background, color = p.accent, p.onAccent
		}
		titleBox = slideBox{contentX, 1600200, 10058400, 2286000}
		title = []slideParagraph{{text: slide.Title, size: 5400, color: color, bold: true}}
	}
	title, err = fitParagraphs(title, titleBox, 0, 1800)
	if err != nil {
		return
	}
	// The first two shapes retain cNvPr IDs 2 (title) and 3 (main body),
	// matching the pre-theme document_edit anchors. The group-tree ID is 1.
	s.WriteString(textBox(2, "Title", titleBox, title, "", "", "ctr", 0))
	bodyBox := slideBox{contentX, contentY, contentWidth, contentHeight}
	addText := func(id int, name string, box slideBox, paras []slideParagraph, fill, border, anchor string, padding, minimum int) {
		if err != nil {
			return
		}
		var fitted []slideParagraph
		fitted, err = fitParagraphs(paras, box, padding, minimum)
		if err == nil {
			s.WriteString(textBox(id, name, box, fitted, fill, border, anchor, padding))
		}
	}
	switch slide.Kind {
	case "title", "section":
		if slide.Kind == "section" && strings.TrimSpace(slide.Title) == "" {
			err = fmt.Errorf("section needs a title")
			return
		}
		color := "CBD5E1"
		if slide.Kind == "section" {
			color = p.onAccent
		}
		addText(3, "Subtitle", slideBox{contentX, 4114800, 10058400, 1097280}, []slideParagraph{{text: slide.Subtitle, size: 2400, color: color}}, "", "", "t", 0, 1200)
		barColor := p.accent
		if slide.Kind == "section" {
			barColor = p.onAccent
		}
		s.WriteString(accentBar(90, slideBox{contentX, 1249680, 640080, 54864}, barColor))
		// A restrained rail adds visual balance without overlapping the text.
		s.WriteString(accentBar(91, slideBox{11430000, 0, 762000, slideHeight}, mixColor(background, "FFFFFF", 0.06)))
	case "bullets":
		addText(3, "Bullets", bodyBox, bulletParagraphs(slide.Bullets, p, false), "", "", "t", 0, 1200)
	case "two_column":
		w := (contentWidth - 365760) / 2
		for i, items := range [][]Bullet{slide.Left, slide.Right} {
			name := "Left"
			if i == 1 {
				name = "Right"
			}
			addText(3+i, name, slideBox{contentX + i*(w+365760), contentY, w, contentHeight}, bulletParagraphs(items, p, true), p.card, p.border, "t", 320040, 1200)
		}
	case "cards":
		if len(slide.Cards) < 2 || len(slide.Cards) > 4 {
			err = fmt.Errorf("cards needs 2..4 cards")
			return
		}
		gap := 274320
		w := (contentWidth - gap*(len(slide.Cards)-1)) / len(slide.Cards)
		for i, c := range slide.Cards {
			if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Text) == "" {
				err = fmt.Errorf("each card needs title and text")
				return
			}
			paras := []slideParagraph{}
			if c.Value != "" {
				paras = append(paras, slideParagraph{text: c.Value, size: 4800, color: p.accentText, bold: true, gap: 1800})
			}
			paras = append(paras, slideParagraph{text: c.Title, size: 2400, color: p.text, bold: true, gap: 1200}, slideParagraph{text: c.Text, size: 2100, color: p.muted})
			addText(3+i, fmt.Sprintf("Card %d", i+1), slideBox{contentX + i*(w+gap), contentY + 228600, w, contentHeight - 457200}, paras, p.card, p.border, "t", 365760, 1200)
		}
	case "quote":
		if strings.TrimSpace(slide.Quote) == "" {
			err = fmt.Errorf("quote needs quote text")
			return
		}
		addText(3, "Quote", slideBox{contentX + 457200, contentY + 182880, contentWidth - 914400, 2926080}, []slideParagraph{{text: "“" + slide.Quote + "”", size: 3600, color: p.text}}, "", "", "ctr", 0, 1600)
		addText(4, "Attribution", slideBox{contentX + 457200, contentY + 3337560, contentWidth - 914400, 640080}, []slideParagraph{{text: slide.Attribution, size: 2000, color: p.accentText, bold: true}}, "", "", "t", 0, 1200)
		s.WriteString(accentBar(89, slideBox{contentX, contentY + 365760, 54864, contentHeight - 731520}, p.accent))
	case "image":
		data, ext, err = imageData(load, slide.Image)
		if err != nil {
			return
		}
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(data)) // imageData already validated it.
		frame := bodyBox
		if slide.Caption != "" {
			frame.h -= 640080
		}
		scale := math.Min(float64(frame.w)/float64(cfg.Width), float64(frame.h)/float64(cfg.Height))
		w, h := int(float64(cfg.Width)*scale), int(float64(cfg.Height)*scale)
		s.WriteString(fmt.Sprintf(`<p:pic><p:nvPicPr><p:cNvPr id="3" name="Image"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="image"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:ln w="12700">%s</a:ln></p:spPr></p:pic>`, frame.x+(frame.w-w)/2, frame.y+(frame.h-h)/2, w, h, solid(p.border)))
		if slide.Caption != "" {
			addText(4, "Caption", slideBox{contentX, contentY + contentHeight - 457200, contentWidth, 457200}, []slideParagraph{{text: slide.Caption, size: 1600, color: p.muted}}, "", "", "ctr", 0, 1000)
		}
	case "table":
		var table string
		table, err = slideTable(slide.Rows, bodyBox, p)
		s.WriteString(table)
	default:
		err = fmt.Errorf("unknown slide kind %q", slide.Kind)
		return
	}
	if slide.Kind != "title" && slide.Kind != "section" {
		s.WriteString(accentBar(90, slideBox{contentX, 1453896, 548640, 45720}, p.accent))
	}
	footerColor := p.muted
	if slide.Kind == "title" {
		footerColor = "94A3B8"
	}
	if slide.Kind == "section" {
		footerColor = p.onAccent
	}
	s.WriteString(textBox(99, "Slide number", slideBox{10881360, 6263640, 579120, 274320}, []slideParagraph{{text: fmt.Sprintf("%02d", number), size: 1100, color: footerColor}}, "", "", "ctr", 0))
	shapes = s.String()
	return
}

func slideTable(rows [][]string, box slideBox, p palette) (string, error) {
	if len(rows) == 0 || len(rows[0]) == 0 {
		return "", fmt.Errorf("table needs rows and columns")
	}
	cols := len(rows[0])
	weights := make([]int, cols)
	for _, row := range rows {
		if len(row) != cols {
			return "", fmt.Errorf("table rows must have equal width")
		}
		for c, cell := range row {
			// Cap outliers so one long cell does not starve neighboring columns.
			weights[c] = max(weights[c], min(42, max(8, len([]rune(cell)))))
		}
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	widths := make([]int, cols)
	remaining := box.w
	for c, w := range weights {
		widths[c] = box.w * w / total
		remaining -= widths[c]
	}
	widths[cols-1] += remaining
	const pad = 137160
	var heights []int
	font := 2000
	fullHeight := 0
	for ; font >= 1000; font -= 50 {
		heights = make([]int, len(rows))
		fullHeight = 0
		for r, row := range rows {
			for c, cell := range row {
				heights[r] = max(heights[r], int(float64(textLines(cell, widths[c]-2*pad, font)*font)*127*1.25)+2*pad)
			}
			heights[r] = max(heights[r], 502920)
			fullHeight += heights[r]
		}
		if fullHeight <= box.h {
			break
		}
	}
	if font < 1000 {
		return "", fmt.Errorf("table cannot fit on slide; split it into multiple slides")
	}
	var s strings.Builder
	s.WriteString(fmt.Sprintf(`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="3" name="Table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblPr firstRow="1" bandRow="1"/><a:tblGrid>`, box.x, box.y+(box.h-fullHeight)/2, box.w, fullHeight))
	for _, w := range widths {
		s.WriteString(fmt.Sprintf(`<a:gridCol w="%d"/>`, w))
	}
	s.WriteString(`</a:tblGrid>`)
	for r, row := range rows {
		s.WriteString(fmt.Sprintf(`<a:tr h="%d">`, heights[r]))
		fill, color := p.card, p.text
		if r%2 == 0 {
			fill = p.tint
		}
		if r == 0 {
			fill, color = p.header, "FFFFFF"
		}
		for _, cell := range row {
			s.WriteString(`<a:tc><a:txBody><a:bodyPr/><a:lstStyle/>` + paragraphXML(slideParagraph{text: cell, size: font, color: color, bold: r == 0}) + `</a:txBody>`)
			s.WriteString(fmt.Sprintf(`<a:tcPr marL="%d" marR="%d" marT="%d" marB="%d" anchor="ctr"><a:lnL><a:noFill/></a:lnL><a:lnR><a:noFill/></a:lnR><a:lnT><a:noFill/></a:lnT><a:lnB w="6350">%s</a:lnB>%s</a:tcPr></a:tc>`, pad, pad, pad, pad, solid(p.border), solid(fill)))
		}
		s.WriteString(`</a:tr>`)
	}
	s.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
	return s.String(), nil
}
