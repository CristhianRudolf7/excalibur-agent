package whiteboard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestStyleShortcuts(t *testing.T) {
	for _, tc := range []struct{ key, value, canonical string }{
		{"fill", `"red"`, "fill"}, {"backgroundColor", `"red"`, "fill"},
		{"stroke", `"blue"`, "stroke"}, {"strokeColor", `"blue"`, "stroke"},
		{"fontSize", `24`, "font_size"}, {"font_size", `24`, "font_size"},
		{"strokeWidth", `3`, "stroke_width"}, {"stroke_width", `3`, "stroke_width"},
		{"opacity", `0.7`, "opacity"}, {"textAlign", `"center"`, "text_align"}, {"fontFamily", `"Helvetica"`, "font_family"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			s := memoryService()
			b := board(t, s)
			for _, action := range []string{"add", "set_scene", "update"} {
				input := raw(`{"id":"test","type":"text","text":"Label","` + tc.key + `":` + tc.value + `}`)
				got, err := s.Apply(context.Background(), b.ID, -1, newID(), "agent", []Op{{Op: action, Elements: []json.RawMessage{input}}}, false, 0)
				if err != nil {
					t.Fatal(action, err)
				}
				data, _ := json.Marshal(got.Elements[0].Style)
				var style map[string]json.RawMessage
				_ = json.Unmarshal(data, &style)
				if string(style[tc.canonical]) != tc.value {
					t.Fatalf("%s: %s", action, data)
				}
				if !strings.Contains(string(input), `"`+tc.key+`"`) {
					t.Fatal("caller input mutated")
				}
			}
		})
	}
}

func TestStylePrecedenceAndPartialUpdate(t *testing.T) {
	s := memoryService()
	b := board(t, s)
	got, err := s.Apply(context.Background(), b.ID, -1, newID(), "agent", []Op{{Op: "add", Elements: []json.RawMessage{raw(`{"id":"text","type":"text","fill":"red","backgroundColor":"blue","fontSize":12,"font_size":18,"style":{"fill":"green","stroke":"black"}}`)}}}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if e := got.Elements[0]; e.Style.Fill != "green" || *e.Style.FontSize != 18 {
		t.Fatal(e.Style)
	}
	got, err = s.Apply(context.Background(), b.ID, -1, newID(), "agent", []Op{{Op: "update", Elements: []json.RawMessage{raw(`{"id":"text","fontSize":24}`)}}}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if e := got.Elements[0]; e.Style.Fill != "green" || e.Style.Stroke != "black" || *e.Style.FontSize != 24 {
		t.Fatal("partial style lost", e.Style)
	}
}

func TestInvalidElementFieldsReturnHelpfulToolErrors(t *testing.T) {
	for _, element := range []string{
		`{"id":"x","type":"rect","colour":"red"}`,
		`{"id":"x","type":"text","style":{"fontSize":12}}`,
		`{"id":"x","type":"chart","chart":{"kind":"bar","labels":["x"],"values":[1],"typo":1}}`,
	} {
		s := memoryService()
		board(t, s)
		tool := &Tool{Service: s, ConversationID: "conv-test"}
		r, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: raw(`{"action":"add","elements":[` + element + `]}`)})
		if err != nil || r.Status != tools.StatusInputError || !strings.Contains(r.Output, "unknown field") || !strings.Contains(r.Output, "allowed element fields:") || !strings.Contains(r.Output, "allowed style fields:") {
			t.Fatalf("%+v %v", r, err)
		}
	}
	for _, element := range []string{`{"id":"x","type":"chart","chart":{"kind":"bar","labels":["A"],"values":[null]}}`, `null`, `[]`, `{"id":"x","type":"text","fontSize":"large"}`, `{"id":"x","type":"rect","style":true}`, `{"id":"x","type":"rect","opacity":2}`} {
		s := memoryService()
		b := board(t, s)
		if _, err := s.Apply(context.Background(), b.ID, -1, newID(), "agent", []Op{{Op: "add", Elements: []json.RawMessage{raw(element)}}}, false, 0); err == nil {
			t.Fatal("accepted invalid input", element)
		}
	}
}
