package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Regenerate additive credential exports with EXCALIBUR_UPDATE_CREDENTIAL_CONTRACTS=1.
func TestCredentialContracts(t *testing.T) {
	models := []any{CredentialRequest{}, CredentialsPendingParams{}, CredentialsPendingResult{}, CredentialsSubmitParams{}, CredentialsCancelParams{}, CredentialsOKResult{}, CredentialsRequestedEvent{}, CredentialsResolvedEvent{}}
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
		var defs map[string]json.RawMessage
		if err = json.Unmarshal(doc["$defs"], &defs); err != nil {
			t.Fatal(err)
		}
		expected := map[string]any{}
		for _, model := range models {
			typ := reflect.TypeOf(model)
			expected[typ.Name()] = credentialSchema(typ)
		}
		if os.Getenv("EXCALIBUR_UPDATE_CREDENTIAL_CONTRACTS") == "1" {
			// Preserve unrelated export bytes, following the classroom generator.
			marker := `    { "name":`
			entries := []any{}
			if name == "methods.json" {
				for _, m := range Methods {
					if m.Family == "credentials" {
						entries = append(entries, m)
					}
				}
			} else {
				marker = `    { "type":`
				for _, e := range EventTypes {
					if strings.HasPrefix(string(e.Type), "credentials.") {
						entries = append(entries, e)
					}
				}
			}
			if !strings.Contains(string(raw), `"credentials.pending"`) && name == "methods.json" || !strings.Contains(string(raw), `"credentials.requested"`) && name == "events.json" {
				at := strings.Index(string(raw), marker)
				if at < 0 {
					t.Fatal("registry insertion marker missing")
				}
				lines := []string{}
				for _, entry := range entries {
					b, _ := json.Marshal(entry)
					lines = append(lines, "    "+string(b)+",")
				}
				raw = []byte(string(raw[:at]) + strings.Join(lines, "\n") + "\n" + string(raw[at:]))
			}
			for _, model := range models {
				key := reflect.TypeOf(model).Name()
				encoded, _ := json.Marshal(expected[key])
				if old, ok := defs[key]; ok {
					raw = []byte(strings.Replace(string(raw), string(old), string(encoded), 1))
					continue
				}
				at := strings.LastIndex(string(raw), "\n  }")
				if at < 0 {
					t.Fatal("definitions marker missing")
				}
				raw = []byte(string(raw[:at]) + ",\n    \"" + key + "\": " + string(encoded) + string(raw[at:]))
			}
			if !json.Valid(raw) {
				t.Fatal("invalid generated contracts")
			}
			if err = os.WriteFile(path, raw, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if err = json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		var actual map[string]any
		if err = json.Unmarshal(doc["$defs"], &actual); err != nil {
			t.Fatal(err)
		}
		for key, value := range expected {
			b, _ := json.Marshal(value)
			var normalized any
			_ = json.Unmarshal(b, &normalized)
			if !reflect.DeepEqual(actual[key], normalized) {
				t.Errorf("%s: %s differs from Go", name, key)
			}
		}
	}
}

func credentialSchema(typ reflect.Type) map[string]any {
	if typ.Kind() == reflect.Map {
		return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	}
	// The existing exporter handles structs, strings, booleans and slices.
	if typ.Kind() != reflect.Struct {
		return classroomSchema(typ, false)
	}
	props := map[string]any{}
	required := []string{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		props[tag] = credentialSchema(f.Type)
		required = append(required, tag)
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
