package tools

import (
	"encoding/json"
	"fmt"
	"sort"
)

// NormalizeSchemaArgs repairs only unambiguous schema-directed shapes and
// numeric bounds. Invalid JSON and wrong container types remain validation errors.
func NormalizeSchemaArgs(raw, schema json.RawMessage, enabled bool) (json.RawMessage, []string) {
	if !enabled {
		return raw, nil
	}
	var value any
	var spec map[string]any
	if json.Unmarshal(raw, &value) != nil || json.Unmarshal(schema, &spec) != nil {
		return raw, nil
	}
	var notes []string
	var walk func(any, map[string]any, string) any
	walk = func(v any, s map[string]any, path string) any {
		typ, _ := s["type"].(string)
		if text, ok := v.(string); ok && (typ == "object" || typ == "array") {
			var decoded any
			if json.Unmarshal([]byte(text), &decoded) == nil {
				_, object := decoded.(map[string]any)
				_, array := decoded.([]any)
				if (typ == "object" && object) || (typ == "array" && array) {
					v = decoded
					notes = append(notes, path+": decoded JSON string as "+typ)
				}
			}
		}
		if n, ok := v.(float64); ok && (typ == "integer" || typ == "number") {
			before := n
			if min, ok := s["minimum"].(float64); ok && n < min {
				n = min
			}
			if max, ok := s["maximum"].(float64); ok && n > max {
				n = max
			}
			if n != before {
				notes = append(notes, fmt.Sprintf("%s: clamped %g to %g", path, before, n))
				v = n
			}
		}
		switch obj := v.(type) {
		case map[string]any:
			props, _ := s["properties"].(map[string]any)
			keys := make([]string, 0, len(obj))
			for k := range obj {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if child, ok := props[k].(map[string]any); ok {
					obj[k] = walk(obj[k], child, path+"."+k)
				}
			}
		case []any:
			if child, ok := s["items"].(map[string]any); ok {
				for i := range obj {
					obj[i] = walk(obj[i], child, fmt.Sprintf("%s[%d]", path, i))
				}
			}
		}
		return v
	}
	value = walk(value, spec, "$")
	if len(notes) == 0 {
		return raw, nil
	}
	out, err := json.Marshal(value)
	if err != nil {
		return raw, nil
	}
	return out, notes
}
