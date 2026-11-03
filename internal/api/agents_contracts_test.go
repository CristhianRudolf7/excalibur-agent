package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// EXCALIBUR_UPDATE_AGENT_CONTRACTS=1 regenerates only additive agent schemas.
func TestAgentsContracts(t *testing.T) {
	models := map[string]any{"Agent": Agent{}, "AvatarSpec": AvatarSpec{}, "AgentsCreateParams": AgentsCreateParams{}, "AgentsUpdateParams": AgentsUpdateParams{}, "AgentsListParams": AgentsListParams{}, "AgentIDParams": AgentIDParams{}, "AgentsListResult": AgentsListResult{}, "AgentsFavoriteResult": AgentsFavoriteResult{}, "AgentsMarkReadResult": AgentsMarkReadResult{}, "AgentsUnreadEvent": AgentsUnreadEvent{}}
	defs := map[string]any{}
	for name, model := range models {
		schema := classroomSchema(reflect.TypeOf(model), true)
		raw, _ := json.Marshal(schema)
		raw = []byte(strings.ReplaceAll(string(raw), "#/$defs/PersonalAgent", "#/$defs/Agent"))
		_ = json.Unmarshal(raw, &schema)
		props := schema["properties"].(map[string]any)
		if name == "Agent" || name == "AgentsUnreadEvent" {
			props["last_message_at"] = map[string]any{"type": []string{"string", "null"}, "format": "date-time"}
			props["unread"] = map[string]any{"type": "integer", "minimum": 0}
		}
		if name == "AvatarSpec" {
			props["variant"] = map[string]any{"enum": []string{"hood", "long-hair", "cap", "bow", "beard", "headphones"}}
			props["accessory"] = map[string]any{"enum": []string{"none", "glasses", "sunglasses", "earring", "scarf", "star"}}
			for _, key := range []string{"tone", "hair", "highlight", "outfit", "accent", "background"} {
				props[key] = map[string]any{"type": "string", "pattern": "^#[0-9a-fA-F]{6}$"}
			}
		}
		defs[name] = schema
	}
	methodSchemas := map[string]any{}
	for _, spec := range []struct{ name, params, result string }{{MethodAgentsList, "AgentsListParams", "AgentsListResult"}, {MethodAgentsGet, "AgentIDParams", "Agent"}, {MethodAgentsCreate, "AgentsCreateParams", "Agent"}, {MethodAgentsUpdate, "AgentsUpdateParams", "Agent"}, {MethodAgentsArchive, "AgentIDParams", "AgentsFavoriteResult"}, {MethodAgentsRestore, "AgentIDParams", "Agent"}, {MethodAgentsSetFavorite, "AgentIDParams", "AgentsFavoriteResult"}, {MethodAgentsMarkRead, "AgentIDParams", "AgentsMarkReadResult"}} {
		methodSchemas[spec.name] = map[string]any{"params": map[string]any{"$ref": "#/$defs/" + spec.params}, "result": map[string]any{"$ref": "#/$defs/" + spec.result}}
	}
	doc := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "agents.json", "$defs": defs, "methods": methodSchemas}
	path := filepath.Join(contractsDir(t), "agents.json")
	if os.Getenv("EXCALIBUR_UPDATE_AGENT_CONTRACTS") == "1" {
		raw, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		for _, filename := range []string{"methods.json", "events.json"} {
			path := filepath.Join(contractsDir(t), filename)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var existing struct {
				Defs map[string]json.RawMessage `json:"$defs"`
			}
			if err = json.Unmarshal(raw, &existing); err != nil {
				t.Fatal(err)
			}
			for key, value := range defs {
				encoded, _ := json.Marshal(value)
				if old, ok := existing.Defs[key]; ok {
					raw = []byte(strings.Replace(string(raw), string(old), string(encoded), 1))
				} else {
					at := strings.LastIndex(string(raw), "\n  }")
					if at < 0 {
						t.Fatal("missing definitions")
					}
					raw = []byte(string(raw[:at]) + ",\n    " + `"` + key + `": ` + string(encoded) + string(raw[at:]))
				}
			}
			if err = os.WriteFile(path, raw, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var actual any
	if err = json.Unmarshal(raw, &actual); err != nil {
		t.Fatal(err)
	}
	expectedRaw, _ := json.Marshal(doc)
	var expected any
	_ = json.Unmarshal(expectedRaw, &expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("agents.json differs from Go contracts")
	}
	for _, filename := range []string{"methods.json", "events.json"} {
		raw, err := os.ReadFile(filepath.Join(contractsDir(t), filename))
		if err != nil {
			t.Fatal(err)
		}
		var exported struct {
			Defs map[string]any `json:"$defs"`
		}
		if err = json.Unmarshal(raw, &exported); err != nil {
			t.Fatal(err)
		}
		for key, value := range defs {
			raw, _ := json.Marshal(value)
			var expected any
			_ = json.Unmarshal(raw, &expected)
			if !reflect.DeepEqual(exported.Defs[key], expected) {
				t.Errorf("%s: %s differs", filename, key)
			}
		}
	}
}
