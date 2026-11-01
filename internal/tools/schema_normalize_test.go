package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNestedSchemaNormalization(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"arguments":{"type":"object","properties":{"card":{"type":"object"},"ops":{"type":"array","items":{"type":"object"}},"yield_ms":{"type":"integer","minimum":250,"maximum":30000}}}}}`)
	for _, yield := range []string{"120000", "60000", "0"} {
		raw := json.RawMessage(`{"arguments":"{\"card\":\"{}\",\"ops\":\"[{}]\",\"yield_ms\":` + yield + `}"}`)
		out, notes := NormalizeSchemaArgs(raw, schema, true)
		if err := ValidateAgainstSchema(out, schema); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		if len(notes) != 4 {
			t.Fatalf("notes: %v", notes)
		}
		disabled, n := NormalizeSchemaArgs(raw, schema, false)
		if string(disabled) != string(raw) || len(n) != 0 {
			t.Fatal("disabled changed arguments")
		}
	}
	arraySchema := json.RawMessage(`{"type":"array","items":{"type":"object","properties":{"card":{"type":"object"}}}}`)
	arrayOut, arrayNotes := NormalizeSchemaArgs(json.RawMessage(`["{\"card\":\"{}\"}"]`), arraySchema, true)
	if len(arrayNotes) != 2 || ValidateAgainstSchema(arrayOut, arraySchema) != nil {
		t.Fatalf("array items: %s %v", arrayOut, arrayNotes)
	}
	for _, raw := range []string{`{"arguments":"[]"}`, `{"arguments":"broken"}`, `{"arguments":"null"}`} {
		out, notes := NormalizeSchemaArgs(json.RawMessage(raw), schema, true)
		if len(notes) != 0 || ValidateAgainstSchema(out, schema) == nil {
			t.Fatalf("accepted wrong type: %s", out)
		}
	}
}

func TestPipelineNormalizationNotes(t *testing.T) {
	def := echoDef()
	def.OutputBudget = 4096
	def.InputSchema = json.RawMessage(`{"type":"object","properties":{"ops":{"type":"array"},"yield_ms":{"type":"integer","maximum":30000}},"required":["ops"]}`)
	reg := scriptedRegistry(def, func(_ context.Context, inv Invocation) (Result, error) {
		if string(inv.Arguments) != `{"ops":[],"yield_ms":30000}` {
			t.Errorf("arguments: %s", inv.Arguments)
		}
		return Result{Status: StatusOK, Output: "invoked"}, nil
	})
	ex := NewExecutor(reg)
	journal := &recordJournal{}
	ex.Journal = journal
	res, err := ex.Execute(context.Background(), def.Name, json.RawMessage(`{"ops":"[]","yield_ms":60000}`), ExecuteOptions{})
	if err != nil || res.Status != StatusOK || !strings.Contains(res.Output, "decoded JSON string") || !strings.Contains(res.Output, "clamped 60000 to 30000") || journal.results != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestRecordedNestedContainerFields(t *testing.T) {
	for _, tc := range []struct {
		name        string
		schema, raw string
	}{
		{"call_tool", string(callToolSchema), `{"name":"exec","arguments":"{\"command\":\"printf ok\"}"}`},
		{"task_db", `{"type":"object","properties":{"ops":{"type":"array","minItems":1}},"required":["ops"]}`, `{"ops":"[{\"op\":\"query\"}]"}`},
		{"subagent", `{"type":"object","properties":{"action":{"type":"string"},"card":{"type":"object"}},"required":["action","card"]}`, `{"action":"spawn","card":"{\"objective\":\"inspect\"}"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := echoDef()
			def.Name = tc.name
			def.InputSchema = json.RawMessage(tc.schema)
			def.OutputBudget = 4096
			ex := NewExecutor(scriptedRegistry(def, func(_ context.Context, inv Invocation) (Result, error) {
				if err := ValidateAgainstSchema(inv.Arguments, def.InputSchema); err != nil {
					t.Error(err)
				}
				return Result{Status: StatusOK, Output: "ok"}, nil
			}))
			res, err := ex.Execute(context.Background(), tc.name, json.RawMessage(tc.raw), ExecuteOptions{})
			if err != nil || res.Status != StatusOK || !strings.Contains(res.Output, "decoded JSON string") {
				t.Fatalf("%+v %v", res, err)
			}
		})
	}
}
