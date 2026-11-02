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

type screenGrid struct {
	width, height, scale, cols, rows int
}

type screenReading struct {
	grid           screenGrid
	lines          []string
	confidence     float64
	lineConfidence []float64
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
	var lineScores strings.Builder
	for row, confidence := range best.lineConfidence {
		if best.lines[row] != "" {
			fmt.Fprintf(&lineScores, " %d:%.3f", row+1, confidence)
		}
	}
	header := fmt.Sprintf("screen_text_grid: %dx%d\nscreen_text_mean_confidence: %.3f\nscreen_text_line_confidence:%s\nscreen_text:\n", best.grid.cols, best.grid.rows, best.confidence, lineScores.String())
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

// Compare both public-domain embedded faces on each grid. Center the shorter
// face in a taller cell (or trim its blank edge rows for a shorter cell), so
// the physical grid does not force the identity of the font inside it.
func screenFontsForHeight(height int) []screenGlyph {
	native, other := screenFont16, screenFont14
	offset := 1
	if height == 14 {
		native, other = screenFont14, screenFont16
		offset = -1
	}
	fonts := append([]screenGlyph(nil), native...)
	for _, source := range other {
		glyph := screenGlyph{code: source.code, char: source.char}
		sourceHeight := 16
		if height == 16 {
			sourceHeight = 14
		}
		for y := 0; y < height; y++ {
			if sy := y - offset; sy >= 0 && sy < sourceHeight {
				glyph.rows[y] = source.rows[sy]
			}
		}
		fonts = append(fonts, glyph)
	}
	return fonts
}

func readScreenGrid(ctx context.Context, img image.Image, g screenGrid) (screenReading, error) {
	font := screenFontsForHeight(g.height)
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
	// Read and match nonuniform cells before resolving spaces: the local
	// palette and winning polarity distinguish inverse spaces from full blocks.
	cells := make([]screenRaster, g.cols*g.rows)
	matches := make([]screenMatch, len(cells))
	cache := make(map[[16]uint16]screenMatch)
	active, good, distinctive, lowColor, highColor := 0, 0, 0, 0, 0
	var totalConfidence float64
	lineConfidence := make([]float64, g.rows)
	for row := 0; row < g.rows; row++ {
		if err := ctx.Err(); err != nil {
			return screenReading{}, err
		}
		lineActive := 0
		for col := 0; col < g.cols; col++ {
			i := row*g.cols + col
			cell := screenCell(img, g, col, row, background)
			cells[i] = cell
			if cell.paletteOK {
				lowColor++
			} else {
				highColor++
				// Even if every remaining cell passed, the required palette fraction
				// would be impossible. Avoid glyph matching on photographic rasters.
				if highColor*100 > len(cells)*2 {
					return screenReading{}, nil
				}
			}
			if cell.uniform {
				continue
			}
			match := screenMatch{char: '?', ambiguous: true}
			if cell.paletteOK {
				var cached bool
				match, cached = cache[cell.rows]
				if !cached {
					match = matchScreenCell(cell.rows, g, font)
					cache[cell.rows] = match
				}
			}
			matches[i] = match
			active++
			lineActive++
			totalConfidence += match.confidence
			lineConfidence[row] += match.confidence
			if !match.ambiguous {
				good++
				if match.char != ' ' && (match.char < '▀' || match.char > '▟') {
					distinctive++
				}
			}
			if (active-good)*5 > len(cells) {
				return screenReading{}, nil
			}
		}
		if lineActive > 0 {
			lineConfidence[row] /= float64(lineActive)
		}
	}
	if active < 3 || distinctive < 3 || good*5 < active*4 || lowColor*100 < g.cols*g.rows*98 {
		return screenReading{}, nil
	}
	confidence := totalConfidence / float64(active)
	if confidence < 0.85 {
		return screenReading{}, nil
	}
	lines := make([]string, g.rows)
	for row := range lines {
		var line strings.Builder
		for col := 0; col < g.cols; col++ {
			i := row*g.cols + col
			char := matches[i].char
			if cells[i].uniform {
				char = screenUniformChar(cells, matches, g, col, row, background)
			}
			line.WriteRune(char)
		}
		lines[row] = strings.TrimRight(line.String(), " ")
	}
	return screenReading{grid: g, lines: lines, confidence: confidence, lineConfidence: lineConfidence}, nil
}

type screenRaster struct {
	rows               [16]uint16
	colors             [2]uint32 // background/foreground before matching either polarity
	paletteOK, uniform bool
}

type screenMatch struct {
	char       rune
	confidence float64
	inverted   bool
	ambiguous  bool
}

// Uniform cells have no glyph evidence. Use the two dominant colors of
// nearby nonuniform cells and their matched polarity, preferring horizontal
// neighbors on the same attribute run. Solid blocks retain the foreground
// color of their neighbors; inverse spaces retain their background color.
// With no local evidence, fall back to the screen-wide background.
func screenUniformChar(cells []screenRaster, matches []screenMatch, g screenGrid, col, row int, background uint32) rune {
	color := cells[row*g.cols+col].colors[0]
	if !imageChanged(color, background) {
		return ' '
	}
	space, block := 0, 0
	for _, direction := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		for distance := 1; distance <= 16; distance++ {
			x, y := col+direction[0]*distance, row+direction[1]*distance
			if x < 0 || x >= g.cols || y < 0 || y >= g.rows {
				break
			}
			i := y*g.cols + x
			cell, match := cells[i], matches[i]
			if cell.uniform {
				continue
			}
			if !cell.paletteOK || match.ambiguous {
				break
			}
			bg, fg := cell.colors[0], cell.colors[1]
			if match.inverted {
				bg, fg = fg, bg
			}
			weight := 17 - distance
			if direction[1] == 0 {
				weight *= 2
			}
			if !imageChanged(color, bg) {
				space += weight
			} else if !imageChanged(color, fg) {
				block += weight
			}
			break
		}
	}
	if space > block {
		return ' '
	}
	return '█'
}

// Reduce integer-scaled cells by sampling pixel centers, then cluster colors
// with the existing per-channel inspection tolerance. A third color (cursor,
// highlight or small noise) goes to its nearest foreground/background color.
// More than 3 colors is accepted only for at most two stray pixels per cell.
func screenCell(img image.Image, g screenGrid, col, row int, background uint32) screenRaster {
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
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if counts[j] > counts[i] || counts[j] == counts[i] && colors[j] < colors[i] {
				counts[i], counts[j] = counts[j], counts[i]
				colors[i], colors[j] = colors[j], colors[i]
			}
		}
	}
	cell := screenRaster{colors: [2]uint32{colors[0], colors[1]}, paletteOK: n <= 3 && extra == 0 || n == 4 && counts[3]+extra <= 2, uniform: n == 1}
	if cell.uniform {
		return cell
	}
	// This ordering preserves the global polarity for exact complement ties
	// such as half blocks. Every glyph is still matched in both polarities.
	if screenColorDistance(colors[1], background) < screenColorDistance(colors[0], background) {
		colors[0], colors[1] = colors[1], colors[0]
	}
	cell.colors = [2]uint32{colors[0], colors[1]}
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			if screenColorDistance(pixels[y][x], colors[1]) < screenColorDistance(pixels[y][x], colors[0]) {
				cell.rows[y] |= 1 << (g.width - 1 - x)
			}
		}
	}
	return cell
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

// Match every glyph in both polarities. Central columns have twice the
// weight of outer columns; one-pixel horizontal contour differences cost 35%
// of an unrelated pixel error. Whole-cell shifts and a one-column expansion
// allow independently drawn VGA fonts without embedding BIOS font tables.
// Both ink and background normalize the score, so a sparse inverse glyph
// cannot obtain high confidence merely by resembling a full block.
func matchScreenCell(cell [16]uint16, g screenGrid, font []screenGlyph) screenMatch {
	// Fast path also preserves the screen-wide polarity for complementary
	// CP437 glyphs (top/bottom and left/right half blocks).
	mask := uint16((1 << g.width) - 1)
	for _, invert := range []bool{false, true} {
		for _, glyph := range font {
			exact := true
			for y := 0; y < g.height; y++ {
				row := cell[y]
				if invert {
					row ^= mask
				}
				if row != screenGlyphRow(glyph, g.width, y) {
					exact = false
					break
				}
			}
			if exact {
				return screenMatch{char: glyph.char, confidence: 1, inverted: invert}
			}
		}
	}
	best := screenMatch{char: '?', confidence: -1}
	second := -1.0
	candidates := make(map[rune]screenMatch, len(font))
	center := uint16(0x3c)
	if g.width == 9 {
		center <<= 1
	}
	weight := func(row uint16) int { return bits.OnesCount16(row) + bits.OnesCount16(row&center) }
	area := weight(mask) * g.height
	for _, glyph := range font {
		candidate := screenMatch{char: glyph.char, confidence: -1}
		for _, invert := range []bool{false, true} {
			var observed [16]uint16
			observedWeight := 0
			for y := 0; y < g.height; y++ {
				observed[y] = cell[y]
				if invert {
					observed[y] ^= mask
				}
				observedWeight += weight(observed[y])
			}
			for _, expand := range []bool{false, true} {
				// Expansion is intended for printable glyphs, never connected borders.
				if expand && glyph.code >= 0x80 {
					continue
				}
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						// CP437 borders keep their exact grid alignment.
						if glyph.code >= 0x80 && (dx != 0 || dy != 0) {
							continue
						}
						expectedWeight := 0
						distance := 0.0
						for y := 0; y < g.height; y++ {
							var want uint16
							if sy := y - dy; sy >= 0 && sy < g.height {
								if expand {
									r := uint16(glyph.rows[sy])
									// Widen the middle while moving the right half outward.
									want = ((r & 0xf0) << 1) | (r & 0x0f)
									if g.width == 8 {
										want >>= 1
									}
								} else {
									want = screenGlyphRow(glyph, g.width, sy)
								}
							}
							if dx < 0 {
								want >>= 1
							} else if dx > 0 {
								want <<= 1
							}
							want &= mask
							row := observed[y]
							nearWant := ((want << 1) | (want >> 1)) & mask
							nearRow := ((row << 1) | (row >> 1)) & mask
							distance += float64(weight(row^want)) - .65*float64(weight((row&^want)&nearWant)+weight((want&^row)&nearRow))
							expectedWeight += weight(want)
						}
						// Uniform candidates are handled using neighborhood palettes instead.
						if expectedWeight == 0 || expectedWeight == area {
							continue
						}
						denominator := max(8, min(observedWeight, area-observedWeight), min(expectedWeight, area-expectedWeight))
						penalty := .025 * float64(screenAbs(dx)+screenAbs(dy))
						if expand {
							penalty += .03
						}
						confidence := max(0, 1-distance/float64(denominator)-penalty)
						if confidence > candidate.confidence {
							candidate.confidence = confidence
							candidate.inverted = invert
						}
					}
				}
			}
		}
		if previous, ok := candidates[candidate.char]; !ok || candidate.confidence > previous.confidence {
			candidates[candidate.char] = candidate
		}
	}
	// Rank distinct characters after combining all faces and transformations.
	// A second rendering of the same letter must never be its own runner-up.
	for _, glyph := range font {
		candidate, ok := candidates[glyph.char]
		if !ok {
			continue
		}
		delete(candidates, glyph.char)
		if candidate.confidence > best.confidence {
			second = best.confidence
			best = candidate
		} else if candidate.confidence > second {
			second = candidate.confidence
		}
	}

	// Exact rasters are reliable. Imperfect rasters need separation from the
	// next distinct character; a close tie is exposed as uncertainty.
	if best.confidence < 1 && (best.confidence < .65 || best.confidence-second < max(.01, .2*(1-best.confidence))) {
		best.char = '?'
		best.ambiguous = true
	}
	return best
}

func screenAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
