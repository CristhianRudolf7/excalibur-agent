package robotlab

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

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
	if count != len(forwardingCases)+2 || len(doc.Methods) != count {
		t.Fatalf("method counts: registry=%d schema=%d want=%d", count, len(doc.Methods), len(forwardingCases)+2)
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

func TestEmbeddedSchemaMatchesSource(t *testing.T) {
	raw, err := os.ReadFile("../../packages/contracts/robotlab.json")
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal([]byte(contractJSON), &b) != nil {
		t.Fatal("invalid schema")
	}
	ar, _ := json.Marshal(a)
	br, _ := json.Marshal(b)
	if string(ar) != string(br) {
		t.Fatal("run python3 scripts/gen-robotlab-contracts.py")
	}
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
}

func TestGoSessionParamsPreserveStructuredGoal(t *testing.T) {
	params := api.RobotLabSessionStartParams{
		Robot: "dog", Environment: "room", Task: "Bring the ball back",
		Goal: api.RobotLabGoal(`{"type":"object_near","object":"ball","target":{"robot_start":true},"radius":0.5}`),
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeParams("robotlab.session.start", raw)
	if err != nil {
		t.Fatalf("typed API params violate the wire schema: %v", err)
	}
	if _, ok := decoded["goal"].(map[string]any); !ok {
		t.Fatal("goal was encoded as bytes instead of an object")
	}
}

func TestGoAttemptResetParamsPreserveSkillReason(t *testing.T) {
	for _, reason := range []string{"", "No action was taken before interruption."} {
		params := api.RobotLabAttemptResetParams{Session: "s", Reflection: "Reassess the approach.", NoSkillReason: reason}
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeParams(api.MethodRobotLabAttemptReset, raw)
		if err != nil {
			t.Fatalf("typed API params violate the wire schema: %v", err)
		}
		got, present := decoded["no_skill_reason"]
		if reason == "" && present || reason != "" && got != reason {
			t.Fatal("optional skill reason changed during encoding")
		}
	}
}

func TestToolSchemaContainsOnlyReferencedDefinitions(t *testing.T) {
	var schema struct {
		Defs map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(toolSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Defs) != 4 {
		t.Fatalf("unexpected input definition count: %d", len(schema.Defs))
	}
	for _, name := range []string{"RobotLabEnvironment", "RobotLabObject", "RobotLabGoal", "RobotLabTarget"} {
		if schema.Defs[name] == nil {
			t.Fatal("missing input definition: " + name)
		}
	}
}

func TestV3GoalsAndTextEnvironmentInputs(t *testing.T) {
	for _, tc := range []struct {
		name, goal string
		valid      bool
	}{
		{"landing", `{"type":"robot_on","target":{"object":"descanso"},"feet":4,"stable_s":1}`, true},
		{"one foot", `{"type":"robot_on","target":{"object":"descanso"},"feet":1}`, true},
		{"placement", `{"type":"object_on","object":"caja","target":{"object":"estante"},"stable_s":1}`, true},
		{"default stability", `{"type":"object_on","object":"caja","target":{"object":"estante"}}`, true},
		{"too few feet", `{"type":"robot_on","target":{"object":"descanso"},"feet":0}`, false},
		{"too many feet", `{"type":"robot_on","target":{"object":"descanso"},"feet":5}`, false},
		{"fractional feet", `{"type":"robot_on","target":{"object":"descanso"},"feet":2.5}`, false},
		{"negative stability", `{"type":"object_on","object":"caja","target":{"object":"estante"},"stable_s":-1}`, false},
		{"landing stability", `{"type":"robot_on","target":{"object":"descanso"},"stable_s":-1}`, false},
		{"missing target", `{"type":"robot_on","feet":4}`, false},
		{"point target", `{"type":"robot_on","target":{"point":[0,0]},"feet":4}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(api.RobotLabSessionStartParams{Robot: "dog", Environment: "escalera-estrecha", Task: "Reach the landing", Goal: api.RobotLabGoal(tc.goal)})
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeParams(api.MethodRobotLabSessionStart, raw)
			if (err == nil) != tc.valid {
				t.Fatalf("goal validation: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		source string
		valid  bool
	}{
		{`{"type":"text"}`, true},
		{`{"type":"text","notes":"Supplied in chat"}`, true},
		{`{"type":"image"}`, false},
		{`{"type":"video"}`, false},
		{`{"type":"text","media_id":"old"}`, false},
	} {
		raw := json.RawMessage(`{"environment":{"version":1,"id":"table","name":"Table","kind":"table","objects":[],"source":` + tc.source + `},"create":true}`)
		_, err := decodeParams("robotlab.env.save", raw)
		if (err == nil) != tc.valid {
			t.Fatalf("source %s: %v", tc.source, err)
		}
	}
}

func TestV3RobotChoices(t *testing.T) {
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	for _, method := range []string{"robotlab.session.start", "robotlab.env.preview"} {
		properties := contract.Methods[method].Params["properties"].(map[string]any)
		choices := properties["robot"].(map[string]any)["enum"].([]any)
		if len(choices) != 2 || choices[0] != "arm" || choices[1] != "dog" {
			t.Fatalf("%s robot choices: %v", method, choices)
		}
	}
}

func TestActionLogStateSurvivesTypedRoundTrip(t *testing.T) {
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	raw := []byte(`{"id":"entry","t":"2026-11-04T00:00:00Z","session":"stairs","attempt":2,"kind":"act","summary":"Moved feet","sim_time":1,"state":{"support":["FL","FR","RL"],"margin":0.025}}`)
	var entry api.RobotLabLogEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	state, ok := value["state"].(map[string]any)
	if !ok || state["margin"] != 0.025 {
		t.Fatalf("lost replay state: %s", encoded)
	}
	schema, err := json.Marshal(schemaWithDefs(map[string]any{"$ref": "#/$defs/RobotLabLogEntry"}))
	if err != nil {
		t.Fatal(err)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatal(err)
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(value); err != nil {
		t.Fatal(err)
	}
	delete(value, "state")
	if err := resolved.Validate(value); err != nil {
		t.Fatalf("legacy log rejected: %v", err)
	}
}
