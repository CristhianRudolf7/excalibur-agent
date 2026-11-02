package tools

import (
	"context"
	"fmt"
	"image"
	"math/bits"
	"strings"
	"unicode/utf8"
)

const screenTextMaxBytes = 4096
const screenCellConfidence = 0.80

type screenGrid struct {
	width, height, scale, cols, rows int
}

type screenReading struct {
	grid       screenGrid
	lines      []string
	confidence float64
}

// inspectScreenText considers only bounded, whole-image cell grids. Both a
// low-color raster and strong nonuniform glyph matches are required: blank
// cells never raise detection confidence. This is a font matcher, not general
// OCR; unsupported fonts and graphical screens should use optional OCR.
func inspectScreenText(ctx context.Context, img image.Image) (string, error) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var best screenReading
	for _, scale := range []int{1, 2} {
		for _, height := range []int{16, 14} {
			for _, width := range []int{8, 9} {
				cw, ch := width*scale, height*scale
				if w%cw != 0 || h%ch != 0 {
					continue
				}
				grid := screenGrid{width, height, scale, w / cw, h / ch}
				if grid.cols < 20 || grid.cols > 160 || grid.rows < 5 || grid.rows > 60 {
					continue
				}
				reading, err := readScreenGrid(ctx, img, grid)
				if err != nil {
					return "", err
				}
				if reading.confidence > best.confidence {
					best = reading
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if best.lines == nil {
		return "", nil
	}
	text := strings.Trim(strings.Join(best.lines, "\n"), "\n")
	header := fmt.Sprintf("screen_text (grid %dx%d, mean_confidence %.3f):\n", best.grid.cols, best.grid.rows, best.confidence)
	const suffix = "\n[screen_text truncated]\n"
	if len(header)+len(text)+1 > screenTextMaxBytes {
		text = text[:screenTextMaxBytes-len(header)-len(suffix)]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		return header + text + suffix, nil
	}
	return header + text + "\n", nil
}

func readScreenGrid(ctx context.Context, img image.Image, g screenGrid) (screenReading, error) {
	font := screenFont16
	if g.height == 14 {
		font = screenFont14
	}
	// A bounded sample chooses a stable background for polarity ties and solid
	// cells. Individual glyphs may use either light or dark foreground colors.
	counts := map[uint32]int{}
	for y := 0; y < g.rows; y++ {
		for x := 0; x < g.cols; x++ {
			counts[imageRGB(img, x*g.width*g.scale, y*g.height*g.scale)]++
		}
	}
	var background uint32
	maxCount := 0
	for c, n := range counts {
		if n > maxCount || n == maxCount && c < background {
			background, maxCount = c, n
		}
	}
	lines := make([]string, g.rows)
	active, good, distinctive, lowColor := 0, 0, 0, 0
	var totalConfidence float64
	for row := 0; row < g.rows; row++ {
		if err := ctx.Err(); err != nil {
			return screenReading{}, err
		}
		var line strings.Builder
		for col := 0; col < g.cols; col++ {
			cell, paletteOK, uniform := screenCell(img, g, col, row, background)
			if paletteOK {
				lowColor++
			}
			if uniform {
				// Uniform non-background cells can represent a full block. They
				// cannot establish a text screen by themselves.
				if cell[0] == 0 {
					line.WriteByte(' ')
				} else {
					line.WriteRune('█')
				}
				continue
			}
			char, confidence := matchScreenCell(cell, g, font)
			if !paletteOK {
				confidence = 0
			}
			active++
			totalConfidence += confidence
			if confidence < screenCellConfidence {
				char = '?'
			} else {
				good++
				if char != ' ' && (char < '▀' || char > '▟') {
					distinctive++
				}
			}
			line.WriteRune(char)
		}
		lines[row] = strings.TrimRight(line.String(), " ")
	}
	if active < 3 || distinctive < 3 || good*5 < active*4 || lowColor*100 < g.cols*g.rows*98 {
		return screenReading{}, nil
	}
	confidence := totalConfidence / float64(active)
	if confidence < 0.85 {
		return screenReading{}, nil
	}
	return screenReading{g, lines, confidence}, nil
}

// Reduce integer-scaled cells by sampling pixel centers, then cluster colors
// with the existing per-channel inspection tolerance. A third color (cursor,
// highlight or small noise) goes to its nearest foreground/background color.
// More than 3 colors is accepted only for at most two stray pixels per cell.
func screenCell(img image.Image, g screenGrid, col, row int, background uint32) ([16]uint16, bool, bool) {
	var pixels [16][9]uint32
	var colors [4]uint32
	var counts [4]int
	n, extra := 0, 0
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			c := imageRGB(img, (col*g.width+x)*g.scale+g.scale/2, (row*g.height+y)*g.scale+g.scale/2)
			pixels[y][x] = c
			i := 0
			for i < n && imageChanged(c, colors[i]) {
				i++
			}
			if i == n {
				if n == len(colors) {
					extra++
					continue
				}
				colors[n] = c
				n++
			}
			counts[i]++
		}
	}
	// Put the two most common colors first, with deterministic ties.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if counts[j] > counts[i] || counts[j] == counts[i] && colors[j] < colors[i] {
				counts[i], counts[j] = counts[j], counts[i]
				colors[i], colors[j] = colors[j], colors[i]
			}
		}
	}
	var cell [16]uint16
	if n == 1 {
		if imageChanged(colors[0], background) {
			for y := 0; y < g.height; y++ {
				cell[y] = (1 << g.width) - 1
			}
		}
		return cell, true, true
	}
	// Prefer the global background for complement ties (half blocks/shading).
	if screenColorDistance(colors[1], background) < screenColorDistance(colors[0], background) {
		colors[0], colors[1] = colors[1], colors[0]
	}
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			if screenColorDistance(pixels[y][x], colors[1]) < screenColorDistance(pixels[y][x], colors[0]) {
				cell[y] |= 1 << (g.width - 1 - x)
			}
		}
	}
	return cell, n <= 3 && extra == 0 || n == 4 && counts[3]+extra <= 2, false
}

func screenColorDistance(a, b uint32) int {
	distance := 0
	for shift := 0; shift < 24; shift += 8 {
		d := int(a>>shift&255) - int(b>>shift&255)
		distance += d * d
	}
	return distance
}

func screenGlyphRow(glyph screenGlyph, width, y int) uint16 {
	row := uint16(glyph.rows[y])
	if width == 9 {
		row <<= 1
		if glyph.code >= 0xc0 && glyph.code <= 0xdf {
			row |= uint16(glyph.rows[y] & 1)
		}
	}
	return row
}

// Hamming distance is normalized by foreground size, rather than blank cell
// area, so sparse punctuation cannot make arbitrary rasters look confident.
func matchScreenCell(cell [16]uint16, g screenGrid, font []screenGlyph) (rune, float64) {
	bestChar, bestConfidence := '?', -1.0
	mask := uint16((1 << g.width) - 1)
	for _, invert := range []bool{false, true} {
		for _, glyph := range font {
			distance, observed, expected := 0, 0, 0
			for y := 0; y < g.height; y++ {
				row := cell[y]
				if invert {
					row ^= mask
				}
				want := screenGlyphRow(glyph, g.width, y)
				distance += bits.OnesCount16(row ^ want)
				observed += bits.OnesCount16(row)
				expected += bits.OnesCount16(want)
			}
			confidence := max(0, 1-float64(distance)/float64(max(8, observed, expected)))
			if confidence > bestConfidence {
				bestChar, bestConfidence = glyph.char, confidence
			}
		}
	}
	return bestChar, bestConfidence
}
