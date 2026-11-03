package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Regenerate this additive export with EXCALIBUR_UPDATE_GOOGLE_CONTRACTS=1.
func TestGoogleContracts(t *testing.T) {
	path := filepath.Join(contractsDir(t), "methods.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	models := []any{GoogleStatusResult{}, GoogleConnectStartResult{}, GoogleConnectCompleteParams{}}
	if os.Getenv("EXCALIBUR_UPDATE_GOOGLE_CONTRACTS") == "1" {
		for _, m := range models {
			name := reflect.TypeOf(m).Name()
			encoded, _ := json.Marshal(classroomSchema(reflect.TypeOf(m), true))
			var doc struct {
				Defs map[string]json.RawMessage `json:"$defs"`
			}
			_ = json.Unmarshal(raw, &doc)
			if old, ok := doc.Defs[name]; ok {
				raw = []byte(strings.Replace(string(raw), string(old), string(encoded), 1))
				continue
			}
			at := strings.LastIndex(string(raw), "\n  }")
			if at < 0 {
				t.Fatal("definition marker missing")
			}
			raw = []byte(string(raw[:at]) + ",\n    \"" + name + "\": " + string(encoded) + string(raw[at:]))
		}
		if err := os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var doc struct {
		Defs map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		typ := reflect.TypeOf(m)
		encoded, _ := json.Marshal(classroomSchema(typ, true))
		var want any
		_ = json.Unmarshal(encoded, &want)
		if !reflect.DeepEqual(doc.Defs[typ.Name()], want) {
			t.Errorf("%s differs from Go contract", typ.Name())
		}
	}
}
