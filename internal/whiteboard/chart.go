package whiteboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Chart is an input-only convenience. Boards and events contain native elements.
type Chart struct {
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Unit   string    `json:"unit"`
	Labels []string  `json:"labels"`
	Values []float64 `json:"values"`
	Colors []string  `json:"colors,omitempty"`
}

type chartElement struct {
	Element
	Chart *Chart `json:"chart,omitempty"`
}

var chartPalette = []string{"#4f86e8", "#27a899", "#edaa45", "#a27ade", "#e47791", "#51abc4"}

func number(v float64) *float64 { return &v }
func chartValue(unit string, v float64) string {
	digits := 0
	if math.Abs(v-math.Round(v)) > 1e-9 {
		digits = 2
	}
	value := fmt.Sprintf("%.*f", digits, v)
	if unit != "" {
		return strings.TrimSpace(unit) + " " + value
	}
	return value
}

func expandChart(e Element, c *Chart) ([]Element, error) {
	e.Type = "rect"
	if err := validElement(e); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fmt.Errorf("chart payload required")
	}
	switch c.Kind {
	case "bar", "column", "line", "pie":
	default:
		return nil, fmt.Errorf("chart kind must be bar, column, pie or line")
	}
	n := len(c.Values)
	if n < 1 || n > 24 {
		return nil, fmt.Errorf("chart must contain 1 to 24 categories")
	}
	if len(c.Labels) != n {
		return nil, fmt.Errorf("chart labels length must equal values length")
	}
	if len(c.Colors) > 24 {
		return nil, fmt.Errorf("chart colors must contain at most 24 entries")
	}
	for i, color := range c.Colors {
		if strings.TrimSpace(color) == "" {
			return nil, fmt.Errorf("chart color %d must not be empty", i)
		}
	}
	maxValue, minValue := 0.0, 0.0
	for i, v := range c.Values {
		if math.IsNaN(v) || math.IsInf(v, 0) || (c.Kind != "line" && v < 0) {
			if c.Kind == "line" {
				return nil, fmt.Errorf("chart value %d must be finite", i)
			}
			return nil, fmt.Errorf("chart value %d must be finite and non-negative", i)
		}
		if strings.TrimSpace(c.Labels[i]) == "" {
			return nil, fmt.Errorf("chart label %d must not be empty", i)
		}
		maxValue = math.Max(maxValue, v)
		minValue = math.Min(minValue, v)
	}
	// Normalize before summing/scaling to keep all finite input values usable.
	scale := math.Max(math.Abs(minValue), math.Abs(maxValue))
	if scale == 0 {
		scale = 1
	}
	total := 0.0
	for _, v := range c.Values {
		total += v / scale
	}

	if e.Width == 0 {
		e.Width = 560
	}
	if e.Height == 0 {
		e.Height = 340
	}
	// Grow the layout for dense or long labels instead of shrinking the text.
	longest := 0
	for _, label := range c.Labels {
		longest = max(longest, len([]rune(label)))
	}
	labelWidth := math.Max(80, float64(longest)*8)
	valueLabels := make([]string, n)
	valueWidth := 100.0
	for i, v := range c.Values {
		valueLabels[i] = chartValue(c.Unit, v)
		if c.Kind == "pie" {
			percent := 0.0
			if total > 0 {
				percent = (v / scale) / total * 100
			}
			valueLabels[i] += fmt.Sprintf(" (%.1f%%)", percent)
		}
		valueWidth = math.Max(valueWidth, float64(len([]rune(valueLabels[i])))*9)
	}

	if c.Kind == "bar" || c.Kind == "pie" {
		e.Width = math.Max(e.Width, labelWidth+valueWidth+200)
		e.Height = math.Max(e.Height, 88+float64(n)*34)
	} else {
		e.Width = math.Max(e.Width, 56+float64(n)*math.Max(math.Max(labelWidth, valueWidth)+12, 112))
		e.Height = math.Max(e.Height, 240)
	}
	e.Type = "rect"
	e.GroupID = e.ID
	e.Style = &Style{Fill: "#ffffff", Stroke: "#cbd5e1", StrokeWidth: number(1), Roughness: number(0)}
	if err := validElement(e); err != nil {
		return nil, err
	}
	out := []Element{e}
	shape := func(suffix, kind string, x, y, w, h float64, style *Style) int {
		out = append(out, Element{ID: e.ID + "-" + suffix, Type: kind, X: e.X + x, Y: e.Y + y, Width: w, Height: h, Style: style, GroupID: e.ID, Author: e.Author, Z: e.Z, FrameID: e.FrameID, Locked: e.Locked})
		return len(out) - 1
	}
	text := func(suffix, value string, x, y, w, size float64, align string) {
		i := shape(suffix, "text", x, y, w, size*1.25, &Style{Stroke: "#1e293b", FontSize: number(size), FontFamily: "Helvetica", TextAlign: align, Roughness: number(0)})
		out[i].Text = value
	}
	color := func(i int) string {
		if len(c.Colors) > 0 {
			return c.Colors[i%len(c.Colors)]
		}
		return chartPalette[i%len(chartPalette)]
	}
	fill := func(i int) *Style {
		return &Style{Fill: color(i), Stroke: color(i), StrokeWidth: number(1), Roughness: number(0)}
	}
	axis := func(x, y, w, h float64) {
		i := shape("axis", "line", x, y, w, h, &Style{Stroke: "#64748b", StrokeWidth: number(1), Roughness: number(0)})
		out[i].Points = [][2]float64{{0, 0}, {w, h}}
	}
	text("title", c.Title, 20, 14, e.Width-40, 22, "left")
	if maxValue == minValue {
		maxValue = minValue + 1
	}
	if c.Kind == "bar" || c.Kind == "pie" {
		left := labelWidth + 24
		plotWidth := e.Width - left - valueWidth - 30
		row := (e.Height - 88) / float64(n)
		axis(left, 56, 0, e.Height-92)
		for i, v := range c.Values {
			y := 58 + float64(i)*row
			shape(fmt.Sprintf("bar-%d", i), "rect", left, y+3, plotWidth*(v/maxValue), 22, fill(i))
			text(fmt.Sprintf("label-%d", i), c.Labels[i], 16, y, labelWidth, 16, "left")
			// Values live outside the bars and remain readable even for zero values.
			text(fmt.Sprintf("value-%d", i), valueLabels[i], left+plotWidth+10, y, valueWidth, 16, "left")
		}
	} else {
		left, top := 28.0, 82.0
		plotWidth, plotHeight := e.Width-56, e.Height-140
		slot := plotWidth / float64(n)
		span := maxValue/scale - minValue/scale
		zeroY := top + plotHeight*(maxValue/scale/span)
		axis(left, zeroY, plotWidth, 0)
		var points [][2]float64
		for i, v := range c.Values {
			x := left + slot*(float64(i)+0.5)
			y := top + plotHeight*(maxValue/scale/span-v/scale/span)
			if c.Kind == "column" {
				shape(fmt.Sprintf("bar-%d", i), "rect", x-slot*0.3, y, slot*0.6, zeroY-y, fill(i))
			} else {
				shape(fmt.Sprintf("point-%d", i), "ellipse", x-4, y-4, 8, 8, fill(i))
				points = append(points, [2]float64{x - left, y - top})
			}
			text(fmt.Sprintf("label-%d", i), c.Labels[i], left+slot*float64(i), e.Height-42, slot, 16, "center")
			text(fmt.Sprintf("value-%d", i), valueLabels[i], left+slot*float64(i), y-26, slot, 16, "center")
		}
		if c.Kind == "line" {
			i := shape("line", "line", left, top, plotWidth, plotHeight, &Style{Stroke: color(0), StrokeWidth: number(2), Roughness: number(0)})
			// Native linear coordinates begin at the first point.
			minX, maxX, minY, maxY := points[0][0], points[0][0], points[0][1], points[0][1]
			for _, p := range points {
				minX = math.Min(minX, p[0])
				maxX = math.Max(maxX, p[0])
				minY = math.Min(minY, p[1])
				maxY = math.Max(maxY, p[1])
			}
			out[i].Width = maxX - minX
			out[i].Height = maxY - minY
			out[i].X += points[0][0]
			out[i].Y += points[0][1]
			for _, p := range points {
				out[i].Points = append(out[i].Points, [2]float64{p[0] - points[0][0], p[1] - points[0][1]})
			}
			if len(out[i].Points) == 1 {
				out[i].Points = append(out[i].Points, [2]float64{0, 0})
			}
		}
	}
	for _, member := range out {
		if err := validElement(member); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func decodeChart(raw json.RawMessage) (chartElement, error) {
	var e chartElement
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, inputFieldsError(err)
	}
	// encoding/json otherwise silently turns a null numeric entry into zero.
	var input struct {
		Chart struct {
			Values []json.RawMessage `json:"values"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return e, err
	}
	for i, value := range input.Chart.Values {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return e, fmt.Errorf("chart value %d must be a finite number", i)
		}
	}
	return e, nil
}
