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
	if count != len(forwardingCases)+3 || len(doc.Methods) != count {
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
