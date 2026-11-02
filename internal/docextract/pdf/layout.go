package pdf

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/klippa-app/go-pdfium/responses"
)

func qualityMin() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("EXCALIBUR_PDF_QUALITY_MIN"), 64); err == nil && v >= 0 && v <= 1 {
		return v
	}
	return .6
}

// PageNeedsOCR applies the same threshold as extraction. An existing invisible
// OCR text layer is native text and is assessed with the same quality checks.
func PageNeedsOCR(p docextract.Page) bool {
	return p.Method == docextract.MethodNone || p.Quality < qualityMin()
}

var cidPattern = regexp.MustCompile(`\(cid:\d+\)`)

func quality(blocks []docextract.Block, area, imageCoverage float64) float64 {
	var text strings.Builder
	for _, b := range blocks {
		text.WriteString(b.Text)
		text.WriteByte('\n')
	}
	s := text.String()
	n, bad := 0, 0
	for _, r := range s {
		if (unicode.IsSpace(r) && !unicode.IsControl(r)) || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		n++
		if unicode.Is(unicode.Co, r) || unicode.Is(unicode.Cs, r) || r == utf8.RuneError || unicode.IsControl(r) || (!unicode.IsGraphic(r) && !unicode.Is(unicode.Cf, r)) {
			bad++
		}
	}
	// Count the entire literal placeholder as garbled, without double counting.
	for _, m := range cidPattern.FindAllString(s, -1) {
		bad += utf8.RuneCountInString(m)
	}
	if n == 0 || area <= 0 {
		return 0
	}
	clean := math.Max(0, 1-float64(bad)/float64(n))
	// A clean title page remains useful; a few characters over a scanned image do not.
	density := math.Min(1, float64(n)/(area*.0001))
	return math.Max(0, math.Min(1, clean*(.7+.3*density)*(1-imageCoverage*(1-density))))
}

func charLines(chars []*responses.GetPageTextStructuredChar, page int, height float64) []docextract.Block {
	var lines []docextract.Block
	var text strings.Builder
	var box docextract.Box
	flush := func() {
		s := strings.TrimSpace(text.String())
		if s != "" {
			lines = append(lines, docextract.Block{Kind: docextract.KindParagraph, Text: s, Boxes: []docextract.Box{box}})
		}
		text.Reset()
	}
	for _, c := range chars {
		if strings.ContainsAny(c.Text, "\r\n") {
			flush()
			continue
		}
		b := docextract.Box{Page: page, X0: c.PointPosition.Left, X1: c.PointPosition.Right, Y0: height - c.PointPosition.Top, Y1: height - c.PointPosition.Bottom}
		if math.IsNaN(b.X0+b.X1+b.Y0+b.Y1) || math.IsInf(b.X0+b.X1+b.Y0+b.Y1, 0) {
			continue
		}
		if text.Len() > 0 && !unicodeSpace(c.Text) {
			h := math.Max(5, box.Y1-box.Y0)
			if math.Abs(b.Y1-box.Y1) > h*.7 || b.X0-box.X1 > h*2 || b.X1 < box.X0-h {
				flush()
			}
		}
		if text.Len() == 0 {
			if unicodeSpace(c.Text) {
				continue
			}
			box = b
		} else {
			box.X0 = math.Min(box.X0, b.X0)
			box.X1 = math.Max(box.X1, b.X1)
			box.Y0 = math.Min(box.Y0, b.Y0)
			box.Y1 = math.Max(box.Y1, b.Y1)
		}
		text.WriteString(c.Text)
	}
	flush()
	return lines
}
func unicodeSpace(s string) bool { return strings.TrimSpace(s) == "" }

var digits = regexp.MustCompile(`\d+`)
var pageNumber = regexp.MustCompile(`(?i)^(?:page\s+)?(?:\d+|[ivxlcdm]+)(?:\s+(?:of|/)\s*\d+)?$`)
var tocLine = regexp.MustCompile(`(?:\.{2,}\s*|\s+)(?:\d+|[ivxlcdmIVXLCDM]+)\s*$`)

func lineKey(b docextract.Block, p docextract.Page) string {
	if len(b.Boxes) == 0 || p.Height <= 0 {
		return ""
	}
	y := (b.Boxes[0].Y0 + b.Boxes[0].Y1) / 2 / p.Height
	text := strings.TrimSpace(b.Text)
	if (y <= .15 || y >= .85) && pageNumber.MatchString(text) {
		text = digits.ReplaceAllString(text, "#")
		if !digits.MatchString(strings.TrimSpace(b.Text)) {
			text = "#"
		}
	}
	return fmt.Sprintf("%d:%s", int(math.Round(y*50)), text)
}
func cleanup(d *docextract.Document) {
	counts := map[string]int{}
	for _, p := range d.Pages {
		seen := map[string]bool{}
		for _, b := range p.Blocks {
			k := lineKey(b, p)
			if k != "" && !seen[k] {
				counts[k]++
				seen[k] = true
			}
		}
	}
	for i := range d.Pages {
		p := &d.Pages[i]
		var kept []docextract.Block
		for _, b := range p.Blocks {
			k := lineKey(b, *p)
			if len(d.Pages) > 1 && k != "" && counts[k]*2 > len(d.Pages) {
				continue
			}
			kept = append(kept, b)
		}
		hits := 0
		for _, b := range kept {
			if tocLine.MatchString(b.Text) {
				hits++
			}
		}
		if hits >= 3 && hits*2 >= len(kept) {
			p.Blocks = []docextract.Block{}
			p.Warnings = append(p.Warnings, "toc_page")
			continue
		}
		p.Blocks = paragraphs(kept, p.Width, p.Number)
	}
}
func paragraphs(lines []docextract.Block, width float64, page int) []docextract.Block {
	// Find a vertical whitespace gap between text rectangles. Ignore spanning
	// headings when looking for columns, then retain them ahead of the columns.
	starts := append([]docextract.Block(nil), lines...)
	sort.SliceStable(starts, func(i, j int) bool { return starts[i].Boxes[0].X0 < starts[j].Boxes[0].X0 })
	split := 0.0
	gap := math.Max(24, width*.05)
	for i := 1; i < len(starts); i++ {
		left, right := starts[i-1].Boxes[0], starts[i].Boxes[0]
		if right.X0-left.X1 > gap && left.X0 < width*.5 && right.X0 > width*.4 {
			split = (left.X1 + right.X0) / 2
			gap = right.X0 - left.X1
		}
	}
	column := func(b docextract.Block) int {
		if split > 0 && b.Boxes[0].X0 >= split {
			return 1
		}
		return 0
	}
	sort.SliceStable(lines, func(i, j int) bool {
		ci, cj := column(lines[i]), column(lines[j])
		if ci != cj {
			return ci < cj
		}
		a, b := lines[i].Boxes[0], lines[j].Boxes[0]
		if math.Abs(a.Y0-b.Y0) > 3 {
			return a.Y0 < b.Y0
		}
		return a.X0 < b.X0
	})
	out := []docextract.Block{}
	for i, b := range lines {
		if len(out) > 0 {
			prev := &out[len(out)-1]
			last := prev.Boxes[len(prev.Boxes)-1]
			next := b.Boxes[0]
			h := math.Max(5, last.Y1-last.Y0)
			if column(lines[i-1]) == column(b) && next.Y0-last.Y1 >= -3 && next.Y0-last.Y1 <= h*.9 && math.Abs(next.X0-last.X0) < h*2 {
				if strings.HasSuffix(prev.Text, "-") {
					prev.Text = strings.TrimSuffix(prev.Text, "-") + b.Text
				} else {
					prev.Text += " " + b.Text
				}
				prev.Boxes = append(prev.Boxes, b.Boxes...)
				continue
			}
		}
		b.Anchor = fmt.Sprintf("p%d/b%d", page, len(out)+1)
		out = append(out, b)
	}
	return out
}
