package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Regenerate only this feature's additive exports with
// EXCALIBUR_UPDATE_CLASSROOM_CONTRACTS=1 go test ./internal/api -run TestClassroomContracts.
func TestClassroomContracts(t *testing.T) {
	models := []any{
		AppContext{}, ConversationSendParams{},
		ClassroomQuestionView{}, ClassroomQuizView{}, ClassroomClass{}, ClassroomTopicView{}, ClassroomUnitView{}, ClassroomExamState{}, ClassroomCourseView{}, ClassroomCourseSummary{}, ClassroomAnswer{}, ClassroomAnswerResult{}, ClassroomAttemptView{},
		ClassroomCourseParams{}, ClassroomTopicParams{}, ClassroomSyllabusTopic{}, ClassroomSyllabusUnit{}, ClassroomUpdateSyllabusParams{}, ClassroomRequestQuizParams{}, ClassroomGetQuizParams{}, ClassroomSubmitParams{}, ClassroomAttemptsParams{},
		ClassroomListResult{}, ClassroomOpenTopicResult{}, ClassroomRequestQuizResult{}, ClassroomSubmitResult{}, ClassroomAttemptsResult{}, ClassroomEvent{}, ClassroomCourseEvent{}, ClassroomClassReadyEvent{}, ClassroomQuizReadyEvent{}, ClassroomAttemptGradedEvent{}, ClassroomFeedbackReadyEvent{}, ClassroomTopicCompletedEvent{},
	}
	defs := map[string]any{}
	for _, model := range models {
		typ := reflect.TypeOf(model)
		defs[typ.Name()] = classroomSchema(typ, true)
	}
	for _, name := range []string{"methods.json", "events.json"} {
		path := filepath.Join(contractsDir(t), name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err = json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("EXCALIBUR_UPDATE_CLASSROOM_CONTRACTS") == "1" {
			// Keep unrelated exports byte-for-byte to avoid noisy schema changes.
			marker := "    { \"name\": \"whiteboard.list\""
			entries := []any{}
			if name == "methods.json" {
				for _, m := range Methods {
					if m.Family == "classroom" {
						entries = append(entries, m)
					}
				}
			} else {
				marker = "    { \"type\": \"whiteboard.opened\""
				for _, e := range EventTypes {
					if strings.HasPrefix(string(e.Type), "classroom.") {
						entries = append(entries, e)
					}
				}
			}
			lines := []string{}
			for _, entry := range entries {
				b, _ := json.Marshal(entry)
				lines = append(lines, "    "+string(b)+",")
			}
			if !strings.Contains(string(raw), `"classroom.list"`) && name == "methods.json" || !strings.Contains(string(raw), `"classroom.course_created"`) && name == "events.json" {
				at := strings.Index(string(raw), marker)
				if at < 0 {
					t.Fatal("export insertion marker missing")
				}
				raw = []byte(string(raw[:at]) + strings.Join(lines, "\n") + "\n" + string(raw[at:]))
			}
			if err = json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			var existing map[string]json.RawMessage
			if err = json.Unmarshal(doc["$defs"], &existing); err != nil {
				t.Fatal(err)
			}
			// Replace previously generated definitions in place or append on first run.
			for _, model := range models {
				key := reflect.TypeOf(model).Name()
				encoded, _ := json.Marshal(defs[key])
				old, ok := existing[key]
				if ok {
					raw = []byte(strings.Replace(string(raw), string(old), string(encoded), 1))
					continue
				}
				at := strings.LastIndex(string(raw), "\n  }")
				if at < 0 {
					t.Fatal("definitions closing marker missing")
				}
				raw = []byte(string(raw[:at]) + ",\n    \"" + key + "\": " + string(encoded) + string(raw[at:]))
			}
			if !json.Valid(raw) {
				t.Fatal("generated invalid contract")
			}
			if err = os.WriteFile(path, raw, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if err = json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		var exported map[string]any
		if err = json.Unmarshal(doc["$defs"], &exported); err != nil {
			t.Fatal(err)
		}
		for key, want := range defs {
			b, _ := json.Marshal(want)
			var normalized any
			_ = json.Unmarshal(b, &normalized)
			if !reflect.DeepEqual(exported[key], normalized) {
				t.Errorf("%s definition %s differs from Go client contract", name, key)
			}
		}
	}
}

func classroomSchema(typ reflect.Type, root bool) map[string]any {
	if typ.Kind() == reflect.Pointer {
		return classroomSchema(typ.Elem(), false)
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if !root && typ.Kind() == reflect.Struct {
		return map[string]any{"$ref": "#/$defs/" + typ.Name()}
	}
	switch typ.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": classroomSchema(typ.Elem(), false)}
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous {
				embedded := classroomSchema(field.Type, true)
				for k, v := range embedded["properties"].(map[string]any) {
					properties[k] = v
				}
				required = append(required, embedded["required"].([]string)...)
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			properties[tag[0]] = classroomSchema(field.Type, false)
			if len(tag) == 1 {
				required = append(required, tag[0])
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	default:
		panic("unsupported classroom schema type " + typ.String())
	}
}
