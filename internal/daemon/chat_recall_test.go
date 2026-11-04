package daemon

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestNormalizeChatRecallArgsDefaultsScopes(t *testing.T) {
	cases := []struct {
		name, in string
		want     map[string]any
	}{
		{"history_search", `{}`, map[string]any{"scopes": []any{"general"}}},
		{"history_search", ``, map[string]any{"scopes": []any{"general"}}},
		{"memory_search", `{"query":"bachiller","scope":"all"}`, map[string]any{"query": "bachiller", "scopes": []any{"general"}}},
		{"memory_search", `{"query":"x","scope":"project:p1"}`, map[string]any{"query": "x", "scopes": []any{"project:p1"}}},
		{"memory_search", `{"query":"x","scopes":["general"]}`, map[string]any{"query": "x", "scopes": []any{"general"}}},
	}
	for _, c := range cases {
		var got map[string]any
		if err := json.Unmarshal(normalizeChatRecallArgs(c.name, json.RawMessage(c.in)), &got); err != nil {
			t.Fatalf("%s %s: %v", c.name, c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %s = %v, want %v", c.name, c.in, got, c.want)
		}
	}
	if out := string(normalizeChatRecallArgs("web_search", json.RawMessage(`{"q":"a"}`))); out != `{"q":"a"}` {
		t.Errorf("other tools must be untouched, got %s", out)
	}
}

func TestAnnotateChatRecallResultAddsNoteOnlyWithItems(t *testing.T) {
	res := annotateChatRecallResult("history_search", tools.Result{Status: tools.StatusOK, Output: `{"items":[{"id":"m1"}]}`})
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil || out["note"] == nil {
		t.Fatalf("expected note, got %s", res.Output)
	}
	empty := annotateChatRecallResult("history_search", tools.Result{Status: tools.StatusOK, Output: `{"items":[]}`})
	if empty.Output != `{"items":[]}` {
		t.Fatalf("empty results must be unchanged, got %s", empty.Output)
	}
}
