package robot

import (
	"image"
	"image/color"
	"image/draw"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// pixelGrid keeps the original dimensions and coordinate origin. Sparse,
// translucent lines preserve object colors; edge labels use the existing font.
func pixelGrid(src image.Image) *image.RGBA {
	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	line := image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 65})
	for x := 0; x < width; x += 100 {
		draw.Draw(out, image.Rect(x, 0, x+1, height), line, image.Point{}, draw.Over)
	}
	for y := 0; y < height; y += 100 {
		draw.Draw(out, image.Rect(0, y, width, y+1), line, image.Point{}, draw.Over)
	}
	label := func(text string, x, y int) {
		w := font.MeasureString(basicfont.Face7x13, text).Ceil()
		x = max(0, min(x, width-w-2))
		y = max(0, min(y, height-14))
		draw.Draw(out, image.Rect(x, y, x+w+2, y+14), image.NewUniform(color.NRGBA{A: 125}), image.Point{}, draw.Over)
		d := font.Drawer{Dst: out, Src: image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 220}), Face: basicfont.Face7x13, Dot: fixed.P(x+1, y+11)}
		d.DrawString(text)
	}
	for x := 0; x < width; x += 100 {
		label(strconv.Itoa(x), x+3, 0)
		label(strconv.Itoa(x), x+3, height-14)
	}
	for y := 100; y < height; y += 100 {
		label(strconv.Itoa(y), 0, y+3)
		label(strconv.Itoa(y), width-24, y+3)
	}
	return out
}
