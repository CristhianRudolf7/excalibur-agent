package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestChangesJSONContract(t *testing.T) {
	content := ""
	ev := MessageEvent{Changes: []FileChange{{Path: "empty", Status: "added", Hunks: []FileHunk{{OldStart: 0, NewStart: 1, NewLines: 1, Lines: []string{"+"}}}, Content: &content}}}
	ev.Changes = append(ev.Changes, FileChange{Path: "empty.txt", Status: "added", Hunks: []FileHunk{}, Content: &content})
	if !reflect.DeepEqual(CloneChanges(ev.Changes), ev.Changes) {
		t.Fatal("clone changed array shape")
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"content":""`) || !strings.Contains(string(raw), `"old_start":0`) {
		t.Fatalf("JSON: %s", raw)
	}
	var got MessageEvent
	if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, ev) {
		t.Fatalf("roundtrip: %+v %v", got, err)
	}
	raw, err = os.ReadFile(filepath.Join(contractsDir(t), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schemas struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schemas); err != nil {
		t.Fatal(err)
	}
	for name, typ := range map[string]reflect.Type{"FileChange": reflect.TypeOf(FileChange{}), "FileHunk": reflect.TypeOf(FileHunk{})} {
		for i := 0; i < typ.NumField(); i++ {
			field := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if schemas.Defs[name].Properties[field] == nil {
				t.Errorf("missing %s.%s", name, field)
			}
		}
	}
	for _, name := range []string{"MessageEvent", "ConversationMessage"} {
		if schemas.Defs[name].Properties["changes"] == nil {
			t.Errorf("missing %s.changes", name)
		}
	}
}
