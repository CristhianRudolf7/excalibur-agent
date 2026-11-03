package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCollectionContractShapes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(contractsDir(t), "collections.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	cases := map[string]any{"Field": CollectionField{}, "Collection": Collection{}, "Filter": CollectionFilter{}, "SummaryCard": SummaryCard{}, "RecordRow": RecordRow{}, "ImportMapping": ImportMapping{}, "ImportConflict": ImportConflict{}, "ImportFix": ImportFix{}, "ImportReport": ImportReport{}, "CollectionID": CollectionsIDParams{}, "CollectionResult": CollectionResult{}, "RecordResult": RecordResult{}, "ListCollections": CollectionsListResult{}, "CreateCollection": CollectionsCreateParams{}, "UpdateCollection": CollectionsUpdateParams{}, "AlterFields": CollectionsFieldsParams{}, "AlterFieldsResult": CollectionsFieldsResult{}, "ListRecords": RecordsListParams{}, "ListRecordsResult": RecordsListResult{}, "RecordID": RecordsIDParams{}, "GetRecordResult": RecordsGetResult{}, "CreateRecord": RecordsCreateParams{}, "UpdateRecord": RecordsUpdateParams{}, "SummaryResult": CollectionsSummaryResult{}, "Import": CollectionsImportParams{}, "ImportResult": CollectionsImportResult{}, "Export": CollectionsExportParams{}, "ExportResult": CollectionsExportResult{}, "Changed": CollectionsChangedEvent{}}
	var check func(reflect.Type, map[string]any, string)
	check = func(typ reflect.Type, schema map[string]any, path string) {
		t.Helper()
		if reference, ok := schema["$ref"].(string); ok {
			schema = document.Defs[strings.TrimPrefix(reference, "#/$defs/")]
		}
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		switch typ.Kind() {
		case reflect.Struct:
			props, _ := schema["properties"].(map[string]any)
			required := map[string]bool{}
			for _, v := range schema["required"].([]any) {
				required[v.(string)] = true
			}
			if len(props) != typ.NumField() {
				t.Fatalf("%s field count: %d != %d", path, len(props), typ.NumField())
			}
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				tag := strings.Split(f.Tag.Get("json"), ",")
				name := tag[0]
				property, ok := props[name].(map[string]any)
				if !ok {
					t.Fatalf("%s.%s missing", path, name)
				}
				if required[name] != (len(tag) == 1) {
					t.Fatalf("%s.%s required mismatch", path, name)
				}
				check(f.Type, property, path+"."+name)
			}
		case reflect.Slice:
			if schema["type"] != "array" {
				t.Fatalf("%s must be array", path)
			}
			check(typ.Elem(), schema["items"].(map[string]any), path+"[]")
		case reflect.Map:
			if schema["type"] != "object" {
				t.Fatalf("%s must be object", path)
			}
		case reflect.String:
			if schema["type"] != "string" && schema["enum"] == nil {
				if types, ok := schema["type"].([]any); !ok || len(types) != 2 {
					t.Fatalf("%s must be string", path)
				}
			}
		case reflect.Bool:
			if schema["type"] != "boolean" {
				t.Fatalf("%s must be boolean", path)
			}
		case reflect.Int:
			if schema["type"] != "integer" {
				t.Fatalf("%s must be integer", path)
			}
		case reflect.Float64:
			if schema["type"] != "number" {
				t.Fatalf("%s must be number", path)
			}
		}
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) { check(reflect.TypeOf(value), document.Defs[name], name) })
	}
}
