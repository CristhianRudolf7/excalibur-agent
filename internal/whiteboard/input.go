package whiteboard

import (
	"encoding/json"
	"fmt"
	"strings"
)

const allowedElementFields = "id, type, x, y, width, height, points, text, latex, mermaid, code, language, src, start_binding, end_binding, style, group_id, frame_id, z, locked, author, chart"
const allowedStyleFields = "stroke, fill, stroke_width, stroke_style, roughness, font_size, font_family, text_align, opacity"
const allowedChartFields = "kind, title, unit, labels, values, colors"

// Explicit nested style wins; canonical top-level names win over aliases.
var styleShortcuts = [][2]string{
	{"fill", "fill"}, {"stroke", "stroke"},
	{"font_size", "font_size"}, {"fontSize", "font_size"},
	{"stroke_width", "stroke_width"}, {"strokeWidth", "stroke_width"},
	{"opacity", "opacity"}, {"textAlign", "text_align"},
	{"fontFamily", "font_family"}, {"backgroundColor", "fill"}, {"strokeColor", "stroke"},
}

func normalizeElement(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("element must be an object: %w", err)
	}
	if fields == nil {
		return nil, fmt.Errorf("element must be an object; allowed element fields: %s", allowedElementFields)
	}
	var style map[string]json.RawMessage
	if v, ok := fields["style"]; ok {
		if err := json.Unmarshal(v, &style); err != nil {
			return nil, fmt.Errorf("style must be an object: %w; allowed style fields: %s", err, allowedStyleFields)
		}
	}
	moved := false
	for _, shortcut := range styleShortcuts {
		if v, ok := fields[shortcut[0]]; ok {
			if style == nil {
				style = map[string]json.RawMessage{}
			}
			if _, explicit := style[shortcut[1]]; !explicit {
				style[shortcut[1]] = v
			}
			delete(fields, shortcut[0])
			moved = true
		}
	}
	if moved {
		fields["style"], _ = json.Marshal(style)
	}
	return json.Marshal(fields)
}

func inputFieldsError(err error) error {
	if err != nil && strings.Contains(err.Error(), "unknown field") {
		return fmt.Errorf("%w; allowed element fields: %s (or top-level fill, stroke, fontSize/font_size, strokeWidth/stroke_width, opacity, textAlign, fontFamily, backgroundColor, strokeColor); allowed style fields: %s; allowed chart fields: %s", err, allowedElementFields, allowedStyleFields, allowedChartFields)
	}
	return err
}

func mergeStyle(fields, patch map[string]json.RawMessage) {
	var oldStyle, newStyle map[string]json.RawMessage
	if json.Unmarshal(fields["style"], &oldStyle) != nil || json.Unmarshal(patch["style"], &newStyle) != nil || oldStyle == nil || newStyle == nil {
		return
	}
	for k, v := range newStyle {
		oldStyle[k] = v
	}
	patch["style"], _ = json.Marshal(oldStyle)
}
