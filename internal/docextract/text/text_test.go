package text

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func TestTextFormats(t *testing.T) {
	for _, tc := range []struct {
		format, input, first, kind string
		level                      int
	}{
		{"txt", "First\nSecond", "First", docextract.KindParagraph, 0},
		{"md", "## Heading\nText", "Heading", docextract.KindHeading, 2},
		{"html", `<html><head><title>Skip</title></head><body><h3 class=title>A &amp; B</h3><p>some <b>bold</b><br>more</p><script>unsafe</script></body></html>`, "A & B", docextract.KindHeading, 3},
		{"csv", "Name,Value\n\"a,b\",2\n", "Name", docextract.KindCell, 0},
		{"json", `{"a":1}`, "{\n  \"a\": 1\n}", docextract.KindParagraph, 0},
	} {
		t.Run(tc.format, func(t *testing.T) {
			d, err := New(tc.format).Extract(context.Background(), strings.NewReader(tc.input), int64(len(tc.input)), docextract.Options{})
			if err != nil {
				t.Fatal(err)
			}
			p := d.Pages[0]
			if d.Paginated || d.PageCount != 1 || p.Number != 1 || p.Method != docextract.MethodNative || p.Blocks[0].Text != tc.first || p.Blocks[0].Kind != tc.kind || p.Blocks[0].Level != tc.level || p.Blocks[0].Anchor == "" {
				t.Fatal(d)
			}
			if tc.format == "csv" {
				if p.Blocks[2].Text != "a,b" || p.Blocks[2].Anchor != "Sheet1!A2" || p.Blocks[4].Kind != docextract.KindTable {
					t.Fatal(p)
				}
			}
		})
	}
}
func TestImages(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 12, 8))
	img.Set(1, 1, color.White)
	for _, format := range []string{"png", "jpeg"} {
		var b bytes.Buffer
		var err error
		if format == "png" {
			err = png.Encode(&b, img)
		} else {
			err = jpeg.Encode(&b, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		d, err := New(format).Extract(context.Background(), bytes.NewReader(b.Bytes()), int64(b.Len()), docextract.Options{})
		if err != nil {
			t.Fatal(err)
		}
		p := d.Pages[0]
		if p.Width != 12 || p.Height != 8 || p.Method != docextract.MethodNone || p.Quality != 0 {
			t.Fatal(p)
		}
	}
}
func TestBoundsCancellationAndInvalid(t *testing.T) {
	for _, tc := range []struct{ format, s string }{{"json", "{"}, {"csv", "\"unterminated"}, {"png", "invalid"}, {"unsupported", "x"}} {
		if _, err := New(tc.format).Extract(context.Background(), strings.NewReader(tc.s), int64(len(tc.s)), docextract.Options{}); err == nil {
			t.Fatalf("accepted invalid %s", tc.format)
		}
	}
	if _, err := New("txt").Extract(context.Background(), strings.NewReader(""), MaxSize+1, docextract.Options{}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New("txt").Extract(ctx, strings.NewReader(""), 0, docextract.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	called := false
	d, err := New("txt").Extract(context.Background(), strings.NewReader("hello"), 5, docextract.Options{Pages: []int{2}, Progress: func(done, total int) { called = done == 1 && total == 1 }})
	if err != nil || len(d.Pages) != 0 || !called {
		t.Fatal(d, err)
	}
	t.Setenv("EXCALIBUR_TEXT_READ", "0")
	if _, err := New("txt").Extract(context.Background(), strings.NewReader(""), 0, docextract.Options{}); err == nil {
		t.Fatal("switch ignored")
	}
}
func TestHTMLMalformedAndActiveContent(t *testing.T) {
	b := HTMLBlocks(`<h1>Title</h1><p title="a > b">visible &lt;!--</p><!-- hidden --><style>hide</style><p>after`)
	if len(b) != 3 || b[1].Text != "visible <!--" || b[2].Text != "after" {
		t.Fatal(b)
	}
}

func TestCSVWideRecordAndLineBudget(t *testing.T) {
	s := strings.Repeat("x,", 20000) + "last"
	d, err := New("csv").Extract(context.Background(), strings.NewReader(s), int64(len(s)), docextract.Options{})
	if err != nil || len(d.Pages[0].Blocks) != 20001 || len(d.Pages[0].Warnings) == 0 {
		t.Fatal(err)
	}
	s = strings.Repeat("x\n", 100001)
	if _, err := New("txt").Extract(context.Background(), strings.NewReader(s), int64(len(s)), docextract.Options{}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}
