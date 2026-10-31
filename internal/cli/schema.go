package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ValidateAgainstSchema validates a JSON document against the schema
// file passed via --output-schema (Sections 3.6, 6.5). The validator
// implements the subset of JSON Schema that delegation contracts
// need: object properties with required members, nested objects and
// arrays, scalar types (string, number, integer, boolean, null) and
// enum sets. Unknown keywords are ignored so schemas authored for
// full validators still check their structural core.
func ValidateAgainstSchema(doc []byte, schemaPath string) error {
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("cli: read output schema: %w", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fmt.Errorf("cli: parse output schema: %w", err)
	}
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return fmt.Errorf("cli: result is not valid JSON: %w", err)
	}
	if err := checkValue(v, schema, "$"); err != nil {
		return fmt.Errorf("cli: result does not validate against %s: %w", schemaPath, err)
	}
	return nil
}

func checkValue(v any, schema map[string]any, path string) error {
	if enum, ok := schema["enum"]; ok {
		allowed, ok := enum.([]any)
		if !ok {
			return fmt.Errorf("%s: bad enum", path)
		}
		want, _ := json.Marshal(v)
		for _, a := range allowed {
			got, _ := json.Marshal(a)
			if string(want) == string(got) {
				return nil
			}
		}
		return fmt.Errorf("%s: value not in enum", path)
	}
	typ, _ := schema["type"].(string)
	switch typ {
	case "", "any":
		return nil
	case "null":
		if v != nil {
			return fmt.Errorf("%s: want null", path)
		}
		return nil
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s: want string", path)
		}
		return nil
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: want boolean", path)
		}
		return nil
	case "integer":
		f, ok := v.(float64)
		if !ok || f != float64(int64(f)) {
			return fmt.Errorf("%s: want integer", path)
		}
		return nil
	case "number":
		if _, ok := v.(float64); !ok {
			return fmt.Errorf("%s: want number", path)
		}
		return nil
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: want array", path)
		}
		items, _ := schema["items"].(map[string]any)
		if items == nil {
			return nil
		}
		for i, item := range arr {
			if err := checkValue(item, items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want object", path)
		}
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				name, _ := r.(string)
				if _, ok := obj[name]; !ok {
					return fmt.Errorf("%s: missing required property %q", path, name)
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for name, sub := range props {
			subMap, _ := sub.(map[string]any)
			if subMap == nil {
				continue
			}
			if val, ok := obj[name]; ok {
				if err := checkValue(val, subMap, path+"."+name); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%s: unsupported schema type %q", path, strings.TrimSpace(typ))
	}
}
