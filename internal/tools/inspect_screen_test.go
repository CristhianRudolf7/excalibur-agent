package tools

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func renderScreen(t *testing.T, g screenGrid, lines []string, foreground, background color.Color) *image.NRGBA {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, g.cols*g.width*g.scale, g.rows*g.height*g.scale))
	draw.Draw(img, img.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	font := screenFont16
	if g.height == 14 {
		font = screenFont14
	}
	for y, line := range lines {
		for x, char := range []rune(line) {
			var glyph *screenGlyph
			for i := range font {
				if font[i].char == char {
					glyph = &font[i]
					break
				}
			}
			if glyph == nil {
				t.Fatalf("missing test glyph %q", char)
			}
			for py := 0; py < g.height; py++ {
				for px := 0; px < g.width; px++ {
					if screenGlyphRow(*glyph, g.width, py)&(1<<(g.width-1-px)) != 0 {
						box := image.Rect((x*g.width+px)*g.scale, (y*g.height+py)*g.scale, (x*g.width+px+1)*g.scale, (y*g.height+py+1)*g.scale)
						draw.Draw(img, box, image.NewUniform(foreground), image.Point{}, draw.Src)
					}
				}
			}
		}
	}
	return img
}

func recoveredScreen(t *testing.T, img image.Image) (string, string) {
	t.Helper()
	out, err := inspectScreenText(context.Background(), img)
	if err != nil {
		t.Fatal(err)
	}
	_, text, ok := strings.Cut(out, "screen_text:\n")
	if !ok {
		t.Fatalf("screen not detected: %q", out)
	}
	return strings.TrimSuffix(text, "\n"), out
}

func TestInspectScreenTextGeometriesAndColors(t *testing.T) {
	lines := []string{"", "  ┌──────────────────┐", "  │Display ready 123!│", "  │ AaZz 0/()[]<>?   │", "  └──────────────────┘", "", "  ╔═══╦═══╗ ░▒▓█▀▄▌▐", "  ║abc║XYZ║", "  ╚═══╩═══╝"}
	want := strings.Join(lines[1:], "\n")
	for _, g := range []screenGrid{{9, 16, 1, 80, 25}, {8, 16, 1, 80, 25}, {8, 16, 1, 80, 30}, {8, 14, 1, 80, 25}, {8, 16, 2, 80, 25}, {9, 16, 2, 80, 25}, {8, 16, 1, 100, 35}} {
		for _, palette := range [][2]color.Color{{color.White, color.NRGBA{B: 168, A: 255}}, {color.Black, color.White}, {color.NRGBA{R: 255, G: 255, A: 255}, color.NRGBA{R: 128, A: 255}}} {
			img := renderScreen(t, g, lines, palette[0], palette[1])
			got, out := recoveredScreen(t, img)
			if got != want || !strings.Contains(out, "screen_text_mean_confidence: 1.000") {
				t.Fatalf("%+v %v: got %q want %q; %s", g, palette, got, want, out)
			}
		}
	}
}

func TestInspectScreenPrintableASCIIAndBorders(t *testing.T) {
	var ascii strings.Builder
	for c := byte(32); c <= 126; c++ {
		ascii.WriteByte(c)
	}
	const borders = "░▒▓│┤╡╢╖╕╣║╗╝╜╛┐└┴┬├─┼╞╟╚╔╩╦╠═╬╧╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀"
	for _, width := range []int{8, 9} {
		lines := []string{ascii.String()[:60], ascii.String()[60:], borders}
		got, _ := recoveredScreen(t, renderScreen(t, screenGrid{width, 16, 1, 80, 25}, lines, color.White, color.Black))
		if got != strings.Join(lines, "\n") {
			t.Fatalf("%d-column glyphs: %q", width, got)
		}
	}
	// Independent reference for the embedded VGA capital A, including the
	// blank ninth column. Border C4 repeats its final bit into that column.
	for _, glyph := range screenFont16 {
		if glyph.char == 'A' && glyph.rows != [16]byte{0, 0, 0x10, 0x38, 0x6c, 0xc6, 0xc6, 0xfe, 0xc6, 0xc6, 0xc6, 0xc6} {
			t.Fatal("VGA A raster changed")
		}
		if glyph.char == 'A' && screenGlyphRow(glyph, 9, 7)&1 != 0 || glyph.code == 0xc4 && screenGlyphRow(glyph, 9, 7)&1 != 1 {
			t.Fatal("incorrect ninth-column rule")
		}
	}
}

func TestInspectScreenNoiseAndUnknownCells(t *testing.T) {
	g := screenGrid{9, 16, 1, 80, 25}
	const line = "Reading the screen before acting"
	img := renderScreen(t, g, []string{line}, color.White, color.Black)
	// A flipped pixel per non-space glyph must preserve recognition.
	for col, c := range line {
		if c != ' ' {
			x, y := col*9, 0
			img.Set(x, y, color.White)
		}
	}
	got, out := recoveredScreen(t, img)
	if got != line || strings.Contains(out, "screen_text_mean_confidence: 1.000") {
		t.Fatalf("noisy recovery: %q (%s)", got, out)
	}
	// Replace one glyph with unsupported noise: retain the screen but expose
	// uncertainty at that cell, rather than inventing a letter.
	rng := rand.New(rand.NewSource(1))
	for y := 0; y < 16; y++ {
		for x := 0; x < 9; x++ {
			img.Set(x+9, y, []color.Color{color.White, color.Black}[rng.Intn(2)])
		}
	}
	got, _ = recoveredScreen(t, img)
	if got != "R?"+line[2:] {
		t.Fatalf("unknown cell: %q", got)
	}
}

func TestInspectScreenThirdColorAndReverseVideo(t *testing.T) {
	g := screenGrid{9, 16, 1, 80, 25}
	img := renderScreen(t, g, []string{"Normal text", "Reverse text"}, color.White, color.Black)
	for y := 16; y < 32; y++ {
		for x := 0; x < 12*9; x++ {
			if imageRGB(img, x, y) == 0 {
				img.Set(x, y, color.White)
			} else {
				img.Set(x, y, color.Black)
			}
		}
	}
	// A cursor/highlight color on one background pixel must not obscure text.
	img.Set(0, 0, color.NRGBA{R: 128, A: 255})
	got, _ := recoveredScreen(t, img)
	// A uniform reverse-video space has the same raster as a full block.
	if got != "Normal text\nReverse█text" {
		t.Fatalf("third color/reverse video: %q", got)
	}
}

func TestInspectScreenRejectsGraphicsAndCancellation(t *testing.T) {
	for _, mode := range []string{"gradient", "noise", "two-color", "rectangle", "blank"} {
		img := image.NewNRGBA(image.Rect(0, 0, 720, 400))
		rng := rand.New(rand.NewSource(2))
		for y := 0; y < 400; y++ {
			for x := 0; x < 720; x++ {
				var c color.Color = color.Black
				switch mode {
				case "gradient":
					c = color.NRGBA{R: byte(x), G: byte(y), B: byte(x + y), A: 255}
				case "noise":
					c = color.NRGBA{R: byte(rng.Intn(256)), G: byte(rng.Intn(256)), B: byte(rng.Intn(256)), A: 255}
				case "two-color":
					c = []color.Color{color.Black, color.White}[rng.Intn(2)]
				case "rectangle":
					if x >= 180 && x < 540 && y >= 80 && y < 320 {
						c = color.White
					}
				}
				img.Set(x, y, c)
			}
		}
		out, err := inspectScreenText(context.Background(), img)
		if err != nil || out != "" {
			t.Fatalf("%s reported text: %q %v", mode, out, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspectScreenText(ctx, renderScreen(t, screenGrid{9, 16, 1, 80, 25}, []string{"Canceled"}, color.White, color.Black)); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestInspectImageScreenTextAutomaticRequestedAndBounded(t *testing.T) {
	root := t.TempDir()
	tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}}
	img := renderScreen(t, screenGrid{9, 16, 1, 80, 25}, []string{"Read this screen"}, color.White, color.Black)
	imageFixture(t, root, "text.png", img)
	for _, args := range []map[string]any{{"path": "text.png"}, {"path": "text.png", "text": true}} {
		r := fileCall(t, tool, args)
		requireImageOutput(t, r, "screen_text_grid: 80x25\nscreen_text_mean_confidence: 1.000\nscreen_text:\nRead this screen\n")
		if strings.Index(r.Output, "screen_text") > strings.Index(r.Output, "ASCII (") {
			t.Fatal("screen text must precede thumbnail")
		}
	}
	imageFixture(t, root, "blank.png", uniformImage(color.Black))
	r := fileCall(t, tool, map[string]any{"path": "blank.png", "text": true})
	requireImageOutput(t, r, "screen_text: unavailable (no supported character grid detected)")
	if r := fileCall(t, tool, map[string]any{"path": "blank.png"}); strings.Contains(r.Output, "screen_text") {
		t.Fatal("non-text image produced automatic text")
	}
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = strings.Repeat("║", 80)
	}
	_, out := recoveredScreen(t, renderScreen(t, screenGrid{8, 16, 1, 80, 30}, lines, color.White, color.Black))
	if len(out) > screenTextMaxBytes || !utf8.ValidString(out) || !strings.Contains(out, "[screen_text truncated]") {
		t.Fatalf("unbounded or invalid screen text: %d", len(out))
	}
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "0")
	if r := fileCall(t, tool, map[string]any{"path": "text.png", "text": true}); r.Status != StatusDenied {
		t.Fatal("screen reader ignored disabled tool")
	}
}
