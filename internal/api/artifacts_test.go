package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArtifactEventContractFields(t *testing.T) {
	checkArtifactContractFields(t, "events.json", Artifact{}, ArtifactCreatedEvent{}, ArtifactReadyEvent{})
}

func checkArtifactContractFields(t *testing.T, file string, models ...any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contractsDir(t), file))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		typ := reflect.TypeOf(model)
		def, ok := schema.Defs[typ.Name()]
		if !ok {
			t.Errorf("%s missing %s", file, typ.Name())
			continue
		}
		required := map[string]bool{}
		for _, field := range def.Required {
			required[field] = true
		}
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if def.Properties[name] == nil {
				t.Errorf("%s missing %s.%s", file, typ.Name(), name)
			}
			if required[name] == strings.Contains(tag, "omitempty") {
				t.Errorf("%s.%s required mismatch", typ.Name(), name)
			}
		}
		if len(def.Properties) != typ.NumField() {
			t.Errorf("%s.%s property count mismatch", file, typ.Name())
		}
	}
}

func TestArtifactMethodContractFields(t *testing.T) {
	checkArtifactContractFields(t, "methods.json", ArtifactsGetParams{}, ArtifactsGetResult{}, ArtifactsListParams{}, ArtifactsListResult{})
}
