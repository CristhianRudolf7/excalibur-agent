package robotlab

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

func TestRobotLabSchemasCoverRegisteredSurface(t *testing.T) {
	raw, err := os.ReadFile("../../packages/contracts/robotlab.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]struct{ Params, Result map[string]any } `json:"methods"`
		Defs    map[string]any                                     `json:"$defs"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, m := range api.Methods {
		if !strings.HasPrefix(m.Name, "robotlab.") {
			continue
		}
		count++
		schema, ok := doc.Methods[m.Name]
		if !ok || len(schema.Params) == 0 || len(schema.Result) == 0 {
			t.Fatalf("missing method schema: %s", m.Name)
		}
	}
	if count != 20 || len(doc.Methods) != count {
		t.Fatalf("method counts: %d/%d", count, len(doc.Methods))
	}
	// Check local references recursively; unresolved shapes must not ship.
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for k, item := range v {
				if k == "$ref" {
					ref, ok := item.(string)
					if !ok || !strings.HasPrefix(ref, "#/$defs/") {
						t.Fatalf("unexpected reference %v", item)
					}
					if _, ok := doc.Defs[strings.TrimPrefix(ref, "#/$defs/")]; !ok {
						t.Fatalf("unresolved %s", ref)
					}
				} else {
					walk(item)
				}
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	var tree any
	_ = json.Unmarshal(raw, &tree)
	walk(tree)
}
