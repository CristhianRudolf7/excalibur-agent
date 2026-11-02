package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPanelContractFields(t *testing.T) {
	for _, file := range []string{"methods.json", "events.json"} {
		raw, err := os.ReadFile(filepath.Join(contractsDir(t), file))
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Defs map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"$defs"`
		}
		if err = json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		models := []any{ActivityItem{}, ActivityProgress{}, ActivityUpdatedEvent{}}
		if file == "methods.json" {
			models = append(models, ActivityListParams{}, ActivityListResult{}, FilesOpenParams{}, FilesOpenResult{})
		}
		for _, model := range models {
			typ := reflect.TypeOf(model)
			for i := 0; i < typ.NumField(); i++ {
				key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
				if schema.Defs[typ.Name()].Properties[key] == nil {
					t.Errorf("%s missing %s.%s", file, typ.Name(), key)
				}
			}
		}
	}
}
