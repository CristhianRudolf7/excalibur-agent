package create

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
	"testing"
)

type layoutNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr   `xml:",any,attr"`
	Children []layoutNode `xml:",any"`
	Text     string       `xml:",chardata"`
}

func (n layoutNode) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func (n layoutNode) all(name string) []layoutNode {
	var out []layoutNode
	if n.XMLName.Local == name {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, c.all(name)...)
	}
	return out
}
func (n layoutNode) child(name string) layoutNode {
	for _, c := range n.Children {
		if c.XMLName.Local == name {
			return c
		}
	}
	return layoutNode{}
}
func (n layoutNode) one(name string) layoutNode {
	all := n.all(name)
	if len(all) > 0 {
		return all[0]
	}
	return layoutNode{}
}
func layoutXML(t *testing.T, b []byte) layoutNode {
	t.Helper()
	var n layoutNode
	if err := xml.Unmarshal(b, &n); err != nil {
		t.Fatal(err)
	}
	return n
}
func layoutInt(t *testing.T, n layoutNode, name string) int {
	t.Helper()
	v, err := strconv.Atoi(n.attr(name))
	if err != nil {
		t.Fatalf("%s.%s: %v", n.XMLName.Local, name, err)
	}
	return v
}
func wideImage(t *testing.T) ImageLoader {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1600, 400))); err != nil {
		t.Fatal(err)
	}
	return func(string) ([]byte, error) { return b.Bytes(), nil }
}

func TestPresentationLayoutsAndAnchorOrder(t *testing.T) {
	slides := []Slide{
		{Kind: "title", Title: "A better default", Subtitle: "A clear, modern presentation"},
		{Kind: "bullets", Title: "Results", Bullets: []Bullet{{Text: "Main idea"}, {Text: "Supporting idea", Level: 1}}},
		{Kind: "two_column", Title: "Compare", Left: []Bullet{{Text: "Before"}, {Text: "Manual work", Level: 1}}, Right: []Bullet{{Text: "After"}, {Text: "Automated work"}}},
		{Kind: "image", Title: "A screenshot", Image: "wide.png", Caption: "The whole image, without stretching"},
		{Kind: "table", Title: "Steps", Rows: [][]string{{"Step", "What happens next"}, {"1", "Choose a service"}, {"2", "Confirm the appointment"}}},
		{Kind: "section", Title: "Next chapter", Subtitle: "From idea to delivery"},
		{Kind: "cards", Title: "Key facts", Cards: []Card{{Title: "Tests", Text: "Passing across the suite", Value: "38"}, {Title: "Platforms", Text: "One consistent experience", Value: "3"}}},
		{Kind: "quote", Title: "What customers say", Quote: "Simple & effective <every day>.", Attribution: "A customer"},
	}
	for i := range slides {
		slides[i].Notes = "Detail belongs in speaker notes."
	}
	p, err := Build("pptx", Spec{Slides: slides}, wideImage(t))
	if err != nil {
		t.Fatal(err)
	}
	if issues := p.Validate(); len(issues) > 0 {
		t.Fatal(issues)
	}
	if _, err := p.Bytes(); err != nil {
		t.Fatal(err)
	}
	for part, raw := range p.Parts {
		if strings.HasSuffix(part, ".xml") || strings.HasSuffix(part, ".rels") {
			layoutXML(t, raw)
		}
	}
	for i, sl := range slides {
		t.Run(sl.Kind, func(t *testing.T) {
			n := layoutXML(t, p.Parts[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)])
			tree := n.one("spTree")
			var content []layoutNode
			for _, c := range tree.Children {
				if c.XMLName.Local == "sp" || c.XMLName.Local == "pic" || c.XMLName.Local == "graphicFrame" {
					content = append(content, c)
				}
			}
			if len(content) < 2 || content[0].one("cNvPr").attr("id") != "2" || content[0].one("cNvPr").attr("name") != "Title" || content[1].one("cNvPr").attr("id") != "3" {
				t.Fatalf("title/body anchor order: %+v", content)
			}
			if content[0].one("t").Text != sl.Title {
				t.Fatal("title text changed")
			}
			ids := map[string]bool{}
			for _, shape := range content {
				id := shape.one("cNvPr").attr("id")
				if ids[id] {
					t.Fatalf("duplicate shape ID %s", id)
				}
				ids[id] = true
				xf := shape.one("xfrm")
				off, ext := xf.one("off"), xf.one("ext")
				x, y := layoutInt(t, off, "x"), layoutInt(t, off, "y")
				w, h := layoutInt(t, ext, "cx"), layoutInt(t, ext, "cy")
				if x < 0 || y < 0 || w <= 0 || h <= 0 || x+w > slideWidth || y+h > slideHeight {
					t.Fatalf("shape %s exceeds canvas: %d %d %d %d", id, x, y, w, h)
				}
			}
			if content[len(content)-1].one("t").Text != fmt.Sprintf("%02d", i+1) {
				t.Fatal("slide number missing")
			}
			if !strings.Contains(string(p.Parts[fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", i+1)]), sl.Notes) {
				t.Fatal("notes missing")
			}
			body := content[1]
			switch sl.Kind {
			case "title":
				if n.one("bg").one("srgbClr").attr("val") != "0F172A" || content[0].one("rPr").attr("b") != "1" || content[0].one("rPr").attr("sz") != "5400" {
					t.Fatal("title treatment missing")
				}
			case "bullets":
				paras := body.all("p")
				for _, para := range paras {
					pr := para.one("pPr")
					if layoutInt(t, pr, "marL") <= 0 || layoutInt(t, pr, "indent") >= 0 || para.one("buChar").attr("char") != "•" || layoutInt(t, para.one("spcPct"), "val") < 120000 {
						t.Fatal("missing bullet indent or line spacing")
					}
				}
				if layoutInt(t, paras[1].one("rPr"), "sz") >= layoutInt(t, paras[0].one("rPr"), "sz") || paras[1].one("srgbClr").attr("val") == paras[0].one("srgbClr").attr("val") {
					t.Fatal("sub-bullet hierarchy missing")
				}
			case "two_column", "cards":
				if body.one("prstGeom").attr("prst") != "roundRect" || body.one("spPr").one("solidFill").one("srgbClr").attr("val") != "FFFFFF" {
					t.Fatal("card styling missing")
				}
				if body.one("rPr").attr("b") != "1" {
					t.Fatal("card heading/value must be bold")
				}
			case "image":
				ext := body.one("ext")
				if layoutInt(t, ext, "cx") != 4*layoutInt(t, ext, "cy") || body.one("picLocks").attr("noChangeAspect") != "1" {
					t.Fatal("image aspect ratio changed")
				}
				if content[2].one("t").Text != sl.Caption {
					t.Fatal("caption missing")
				}
			case "table":
				rows := body.all("tr")
				if rows[0].one("rPr").attr("b") != "1" || rows[0].one("rPr").one("srgbClr").attr("val") != "FFFFFF" {
					t.Fatal("table header must be white/bold")
				}
				if rows[1].one("tcPr").child("solidFill").one("srgbClr").attr("val") == rows[2].one("tcPr").child("solidFill").one("srgbClr").attr("val") {
					t.Fatal("zebra striping missing")
				}
				grid := body.all("gridCol")
				if layoutInt(t, grid[0], "w") >= layoutInt(t, grid[1], "w") {
					t.Fatal("column widths must reflect content")
				}
				xf := body.one("xfrm")
				if layoutInt(t, xf.one("off"), "y") != contentY+(contentHeight-layoutInt(t, xf.one("ext"), "cy"))/2 {
					t.Fatal("table must be vertically centered")
				}
			case "section":
				if n.one("bg").one("srgbClr").attr("val") != "6366F1" {
					t.Fatal("section must use accent background")
				}
			case "quote":
				if body.one("t").Text != "“"+sl.Quote+"”" || content[2].one("t").Text != sl.Attribution {
					t.Fatal("quote/attribution missing or escaped incorrectly")
				}
			}
		})
	}
}

func TestPresentationTheme(t *testing.T) {
	for _, tc := range []struct {
		theme      Theme
		accent, bg string
	}{
		{Theme{}, "6366F1", "F8FAFC"},
		{Theme{Accent: "#0d9488"}, "0D9488", "F8FAFC"},
		{Theme{Style: "dark", Accent: "#14B8A6"}, "14B8A6", "0F172A"},
		{Theme{Accent: "#FFFFFF"}, "FFFFFF", "F8FAFC"},
	} {
		p, err := Build("pptx", Spec{Theme: tc.theme, Slides: []Slide{{Kind: "bullets", Title: "Results"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		theme := layoutXML(t, p.Parts["ppt/theme/theme1.xml"])
		if theme.one("accent1").one("srgbClr").attr("val") != tc.accent || theme.one("dk1").one("srgbClr").attr("val") != "0F172A" || theme.one("majorFont").one("latin").attr("typeface") != "Calibri" || theme.one("minorFont").one("latin").attr("typeface") != "Calibri" {
			t.Fatal("palette/font scheme missing")
		}
		n := layoutXML(t, p.Parts["ppt/slides/slide1.xml"])
		if n.one("bg").one("srgbClr").attr("val") != tc.bg {
			t.Fatal("wrong slide background")
		}
		pal, err := presentationPalette(tc.theme)
		if err != nil {
			t.Fatal(err)
		}
		if contrast(pal.accentText, pal.card) < 4.5 || contrast(pal.header, "FFFFFF") < 4.5 {
			t.Fatal("accent text is unreadable")
		}
	}
}

func TestPresentationScalesDenseText(t *testing.T) {
	font := func(bullets []Bullet) int {
		t.Helper()
		p, err := Build("pptx", Spec{Slides: []Slide{{Kind: "bullets", Title: "Progress", Bullets: bullets}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		n := layoutXML(t, p.Parts["ppt/slides/slide1.xml"])
		return layoutInt(t, n.all("sp")[1].one("rPr"), "sz")
	}
	short := []Bullet{{Text: "One idea"}, {Text: "Another idea"}, {Text: "The next step"}}
	base := font(short)
	dense := make([]Bullet, 12)
	for i := range dense {
		dense[i] = Bullet{Text: fmt.Sprintf("Milestone %d is ready for delivery", i+1)}
	}
	if got := font(dense); got >= base || got < 1200 {
		t.Fatalf("dense font=%d, base=%d", got, base)
	}
	wrapped := make([]Bullet, 6)
	for i := range wrapped {
		wrapped[i] = Bullet{Text: strings.Repeat("A wider supporting statement with detail. ", 3)}
	}
	if got := font(wrapped); got >= base {
		t.Fatal("wrapped text did not shrink")
	}
	// Long tokens, Unicode, and explicit line breaks must also participate in fit.
	for _, text := range []string{strings.Repeat("W", 400), strings.Repeat("界", 400), strings.Repeat("A line\n", 12)} {
		if got := font([]Bullet{{Text: text}}); got >= base {
			t.Fatalf("text did not scale: %q", text)
		}
	}
}

func TestPresentationRejectsInvalidNewInputsAndOverflow(t *testing.T) {
	validCards := []Card{{Title: "One", Text: "First fact"}, {Title: "Two", Text: "Second fact"}}
	cases := []Spec{
		{Theme: Theme{Style: "invalid"}, Title: "Title"},
		{Theme: Theme{Accent: "6366F1"}, Title: "Title"},
		{Theme: Theme{Accent: "#12345G"}, Title: "Title"},
		{Theme: Theme{Accent: "#12345'"}, Title: "Title"},
		{Slides: []Slide{{Kind: "cards"}}},
		{Slides: []Slide{{Kind: "cards", Cards: validCards[:1]}}},
		{Slides: []Slide{{Kind: "cards", Cards: append(append([]Card{}, validCards...), validCards[0], validCards[0], validCards[0])}}},
		{Slides: []Slide{{Kind: "cards", Cards: []Card{{Title: "", Text: "Detail"}, validCards[1]}}}},
		{Slides: []Slide{{Kind: "cards", Cards: []Card{{Title: "Fact", Text: " "}, validCards[1]}}}},
		{Slides: []Slide{{Kind: "section", Title: " "}}},
		{Slides: []Slide{{Kind: "quote", Quote: " "}}},
		{Slides: []Slide{{Kind: "bullets", Bullets: []Bullet{{Text: "Bad level", Level: 9}}}}},
		{Slides: []Slide{{Kind: "bullets", Bullets: []Bullet{{Text: strings.Repeat("Too much text ", 10000)}}}}},
		{Slides: []Slide{{Kind: "table", Rows: [][]string{{"One", "Two"}, {"One"}}}}},
		{Slides: []Slide{{Kind: "table", Rows: make([][]string, 0)}}},
		{Slides: []Slide{{Kind: "quote", Quote: strings.Repeat("x", 4<<20)}}},
		{Slides: []Slide{{Kind: "image", Caption: strings.Repeat("x", 4<<20)}}},
		{Slides: []Slide{{Kind: "cards", Cards: []Card{{Title: strings.Repeat("x", 4<<20)}}}}},
	}
	for i, s := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if _, err := Build("pptx", s, nil); err == nil {
				t.Fatal("invalid spec accepted")
			}
		})
	}
	for n := 2; n <= 4; n++ {
		cards := make([]Card, n)
		for i := range cards {
			cards[i] = Card{Title: "Fact", Text: "Supporting detail", Value: "38"}
		}
		if _, err := Build("pptx", Spec{Slides: []Slide{{Kind: "cards", Cards: cards}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
}
