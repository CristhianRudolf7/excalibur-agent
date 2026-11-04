package whiteboard

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func chartInput(kind string) json.RawMessage {
	return raw(fmt.Sprintf(`{"id":"expenses","type":"chart","x":40,"y":80,"width":560,"height":340,"chart":{"kind":%q,"title":"Weekly expenses","unit":"S/","labels":["Food","Travel","Other"],"values":[69.5,20,0]}}`, kind))
}
func byID(t *testing.T, b Board, id string) Element {
	t.Helper()
	for _, e := range b.Elements {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("missing element %s", id)
	return Element{}
}
func TestChartExpansion(t *testing.T) {
	for _, kind := range []string{"bar", "column", "pie", "line"} {
		t.Run(kind, func(t *testing.T) {
			s := memoryService()
			b := board(t, s)
			got, err := s.Apply(context.Background(), b.ID, b.Version, newID(), "agent", []Op{{Op: "add", Elements: []json.RawMessage{chartInput(kind)}}}, false, 0)
			if err != nil {
				t.Fatal(err)
			}
			if byID(t, got, "expenses-title").Text != "Weekly expenses" || byID(t, got, "expenses-label-0").Text != "Food" {
				t.Fatal("missing title/category")
			}
			value := "S/ 69.50"
			if kind == "pie" {
				value += " (77.7%)"
			}
			if byID(t, got, "expenses-value-0").Text != value {
				t.Fatal("bad value", got.Elements)
			}
			if kind != "pie" && byID(t, got, "expenses-value-1").Text != "S/ 20" {
				t.Fatal("integer formatting")
			}
			byID(t, got, "expenses-axis")
			primitive := "expenses-bar-0"
			if kind == "line" {
				primitive = "expenses-point-0"
				line := byID(t, got, "expenses-line")
				if len(line.Points) != 3 || line.Points[0] != [2]float64{0, 0} {
					t.Fatal(line)
				}
			}
			if e := byID(t, got, primitive); e.Style.Fill == "" || e.Width <= 0 || e.Height <= 0 {
				t.Fatal("unfilled/empty shape", e)
			}
			for _, e := range got.Elements {
				if e.Type == "chart" || e.GroupID != "expenses" || e.Author != "agent" {
					t.Fatal("not native/grouped", e)
				}
				if e.Type == "text" && (e.Style.FontSize == nil || *e.Style.FontSize < 16) {
					t.Fatal("unreadable text", e)
				}
			}
			// Events carry native elements; replay must exactly match the persisted board.
			replay := Board{}
			for _, event := range s.Bus.Replay(types.Cursor{}) {
				if string(event.Type) != "whiteboard.ops" {
					continue
				}
				var payload api.WhiteboardOpsEvent
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if err := replayChartOps(&replay, payload.Ops, payload.Author); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(replay.Elements, got.Elements) {
				t.Fatal("event replay differs")
			}
		})
	}
}

func TestChartValidation(t *testing.T) {
	base := Chart{Kind: "bar", Labels: []string{"Food"}, Values: []float64{1}}
	cases := []struct {
		name, want string
		change     func(*Chart)
	}{
		{"kind", "kind", func(c *Chart) { c.Kind = "scatter" }},
		{"empty", "1 to 24", func(c *Chart) { c.Values = nil }},
		{"too many", "1 to 24", func(c *Chart) { c.Values = make([]float64, 25) }},
		{"labels", "labels length", func(c *Chart) { c.Labels = nil }},
		{"blank label", "label 0", func(c *Chart) { c.Labels = []string{" "} }},
		{"colors", "at most 24", func(c *Chart) { c.Colors = make([]string, 25) }},
		{"negative", "non-negative", func(c *Chart) { c.Values = []float64{-1} }},
		{"NaN", "finite", func(c *Chart) { c.Values = []float64{math.NaN()} }},
		{"infinity", "finite", func(c *Chart) { c.Values = []float64{math.Inf(1)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.change(&c)
			_, err := expandChart(Element{ID: "c", Author: "agent"}, &c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
	for _, kind := range []string{"bar", "column", "pie", "line"} {
		c := base
		c.Kind = kind
		c.Values = []float64{0}
		if _, err := expandChart(Element{ID: "c", Author: "agent"}, &c); err != nil {
			t.Fatal(kind, err)
		}
		c.Values = []float64{-4}
		_, err := expandChart(Element{ID: "c", Author: "agent"}, &c)
		if (err == nil) != (kind == "line") {
			t.Fatalf("%s negative: %v", kind, err)
		}
	}
	c := base
	c.Labels = make([]string, 24)
	c.Values = make([]float64, 24)
	for i := range c.Labels {
		c.Labels[i] = "A long category label"
	}
	elements, err := expandChart(Element{ID: "c", Author: "agent", Width: 560, Height: 340}, &c)
	if err != nil || len(elements) != 75 || elements[0].Height < 24*34 {
		t.Fatal("dense chart", len(elements), err)
	}
	c = base
	c.Colors = []string{"#123456"}
	elements, err = expandChart(Element{ID: "c", Author: "agent"}, &c)
	if err != nil || elements[3].Style.Fill != "#123456" {
		t.Fatal("custom palette", err)
	}
	c.Labels = []string{"A", "B"}
	c.Values = []float64{1, 2}
	elements, err = expandChart(Element{ID: "c", Author: "agent"}, &c)
	if err != nil || byID(t, Board{Elements: elements}, "c-bar-1").Style.Fill != "#123456" {
		t.Fatal("repeating palette", err)
	}

	if _, err := expandChart(Element{ID: "c", Author: "agent"}, nil); err == nil {
		t.Fatal("accepted missing chart")
	}
}

func TestChartGroupLifecycle(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	b := board(t, s)
	apply := func(op Op, author string) Board {
		t.Helper()
		got, err := s.Apply(ctx, b.ID, -1, newID(), author, []Op{op}, false, 0)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	b = apply(Op{Op: "set_scene", Elements: []json.RawMessage{chartInput("bar"), raw(`{"id":"other","type":"text","text":"Keep"}`)}}, "user")
	// Chart identity and geometry survive a storage/service restart.
	s = NewService(NewStore(db), s.Bus)
	b = apply(Op{Op: "update", Elements: []json.RawMessage{raw(`{"id":"expenses","chart":{"kind":"line","title":"Updated","labels":["One","Two"],"values":[-5,3]}}`)}}, "agent")
	if byID(t, b, "expenses-title").Text != "Updated" || byID(t, b, "expenses").X != 40 || len(b.Elements) != 11 {
		t.Fatal(b.Elements)
	}
	for _, e := range b.Elements {
		if e.GroupID == "expenses" && e.Author != "user" {
			t.Fatal("lost provenance", e)
		}
	}
	old := byID(t, b, "expenses-label-0")
	b = apply(Op{Op: "update", Elements: []json.RawMessage{raw(`{"id":"expenses","x":140,"y":180}`)}}, "agent")
	moved := byID(t, b, "expenses-label-0")
	if moved.X != old.X+100 || moved.Y != old.Y+100 {
		t.Fatal("group did not move")
	}
	before := b
	_, err := s.Apply(ctx, b.ID, -1, newID(), "agent", []Op{{Op: "update", Elements: []json.RawMessage{raw(`{"id":"expenses","chart":{"kind":"bar","labels":["x"],"values":[-1]}}`)}}}, false, 0)
	code(t, err, api.CodeInvalidParams)
	got, _ := s.Store.Get(ctx, b.ID)
	if !reflect.DeepEqual(got, before) {
		t.Fatal("invalid update changed scene")
	}
	b = apply(Op{Op: "remove", IDs: []string{"expenses"}}, "agent")
	if len(b.Elements) != 1 || b.Elements[0].ID != "other" {
		t.Fatal("group remove", b.Elements)
	}
	// Removing by the chart ID is expanded into explicit member IDs for clients.
	var replay Board
	for _, event := range s.Bus.Replay(types.Cursor{}) {
		if string(event.Type) == "whiteboard.ops" {
			var p api.WhiteboardOpsEvent
			_ = json.Unmarshal(event.Payload, &p)
			if err := replayChartOps(&replay, p.Ops, p.Author); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(replay.Elements, b.Elements) {
		t.Fatal("lifecycle replay differs")
	}
}

func TestChartCollisionsAndExpandedLimitAreAtomic(t *testing.T) {
	for _, op := range []Op{
		{Op: "add", Elements: []json.RawMessage{chartInput("bar"), chartInput("bar")}},
		{Op: "add", Elements: []json.RawMessage{raw(`{"id":"expenses-title","type":"rect"}`), chartInput("bar")}},
		{Op: "add", Elements: func() []json.RawMessage {
			var rows []json.RawMessage
			for i := 0; i < 50; i++ {
				rows = append(rows, raw(strings.Replace(string(chartInput("bar")), `"expenses"`, fmt.Sprintf(`"c%d"`, i), 1)))
			}
			return rows
		}()},
	} {
		s := memoryService()
		b := board(t, s)
		_, err := s.Apply(context.Background(), b.ID, -1, newID(), "agent", []Op{op}, false, 0)
		code(t, err, api.CodeInvalidParams)
		got, _ := s.Store.Get(context.Background(), b.ID)
		if len(got.Elements) != 0 || got.Version != b.Version {
			t.Fatal("partial chart write")
		}
	}
}

// Events are trusted native patches; like the web client, replay preserves the
// supplied element authorship rather than applying the API execution boundary.
func replayChartOps(b *Board, ops []Op, author string) error {
	for _, op := range ops {
		if op.Op == "add" {
			for _, raw := range op.Elements {
				var e Element
				if err := json.Unmarshal(raw, &e); err != nil {
					return err
				}
				if err := applyNativeOps(b, []Op{{Op: "add", Elements: []json.RawMessage{raw}}}, e.Author); err != nil {
					return err
				}
			}
		} else if err := applyNativeOps(b, []Op{op}, author); err != nil {
			return err
		}
	}
	return nil
}

func TestChartToolDrawsInOneCall(t *testing.T) {
	for _, action := range []string{"add", "set_scene"} {
		t.Run(action, func(t *testing.T) {
			s := memoryService()
			tool := &Tool{Service: s, ConversationID: "conv-test"}
			args := raw(`{"action":"` + action + `","elements":[` + string(chartInput("bar")) + `]}`)
			result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: args})
			if err != nil || result.Status != tools.StatusOK {
				t.Fatalf("%+v %v", result, err)
			}
			id, err := s.Current(context.Background(), "conv-test")
			if err != nil || id == "" {
				t.Fatal("no active board", err)
			}
			b, err := s.Store.Get(context.Background(), id)
			if err != nil || byID(t, b, "expenses-value-0").Text != "S/ 69.50" {
				t.Fatal(b, err)
			}
			if want := chartTitleOf(t, chartInput("bar")); want != "" && b.Title != want {
				t.Fatalf("auto-opened board title = %q, want the chart title %q", b.Title, want)
			}
			// A second call draws on the same board rather than opening another.
			result, err = tool.Invoke(context.Background(), tools.Invocation{Arguments: raw(`{"action":"add","elements":[{"id":"note","type":"text","text":"Done"}]}`)})
			if err != nil || result.Status != tools.StatusOK {
				t.Fatal(result, err)
			}
			boards, _ := s.Store.List(context.Background(), "conv-test")
			if len(boards) != 1 {
				t.Fatal("opened extra board")
			}
		})
	}
	s := memoryService()
	tool := &Tool{Service: s, ConversationID: "conv-test"}
	result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: raw(`{"action":"add","elements":[{"id":"bad","type":"chart","chart":{"kind":"bar","labels":["x"],"values":[-1]}}]}`)})
	boards, _ := s.Store.List(context.Background(), "conv-test")
	if err != nil || result.Status != tools.StatusInputError || len(boards) != 0 {
		t.Fatal("invalid chart created a board", result, err)
	}
	if !strings.Contains(tool.Definition().Description, `"type":"chart"`) {
		t.Fatal("missing chart example")
	}
}

func TestChartFiniteExtremesAndSinglePoint(t *testing.T) {
	for _, kind := range []string{"bar", "column", "pie", "line"} {
		values := []float64{math.MaxFloat64, math.MaxFloat64}
		if kind == "line" {
			values[0] = -math.MaxFloat64
		}
		elements, err := expandChart(Element{ID: "c", Author: "agent"}, &Chart{Kind: kind, Labels: []string{"A", "B"}, Values: values})
		if err != nil {
			t.Fatal(kind, err)
		}
		for _, e := range elements {
			if err := validElement(e); err != nil {
				t.Fatal(kind, e, err)
			}
		}
		if kind == "pie" {
			for _, e := range elements {
				if strings.Contains(e.ID, "-value-") && !strings.Contains(e.Text, "50.0%") {
					t.Fatal("overflowed percentage", e.Text)
				}
			}
		}
	}
	elements, err := expandChart(Element{ID: "c", Author: "agent"}, &Chart{Kind: "line", Labels: []string{"A"}, Values: []float64{0}})
	if err != nil {
		t.Fatal(err)
	}
	if e := byID(t, Board{Elements: elements}, "c-line"); len(e.Points) != 2 || e.Width != 0 || e.Height != 0 {
		t.Fatal("invalid singleton line", e)
	}
}

func TestChartGroupStyleUpdatePreservesMemberStyles(t *testing.T) {
	s := memoryService()
	b := board(t, s)
	ctx := context.Background()
	b, err := s.Apply(ctx, b.ID, -1, newID(), "agent", []Op{{Op: "add", Elements: []json.RawMessage{chartInput("column")}}}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	before := byID(t, b, "expenses-bar-0")
	b, err = s.Apply(ctx, b.ID, -1, newID(), "agent", []Op{{Op: "update", Elements: []json.RawMessage{raw(`{"id":"expenses","opacity":0.8,"width":1120,"height":680}`)}}}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range b.Elements {
		if e.Style.Opacity == nil || *e.Style.Opacity != 0.8 {
			t.Fatal("group style not applied", e)
		}
	}
	bar := byID(t, b, "expenses-bar-0")
	if bar.Style.Fill != before.Style.Fill || bar.Width != 2*before.Width || bar.Height != 2*before.Height {
		t.Fatal("resize/style lost", bar)
	}
	if *byID(t, b, "expenses-title").Style.FontSize != 22 {
		t.Fatal("font style lost")
	}
}

func chartTitleOf(t *testing.T, input json.RawMessage) string {
	t.Helper()
	var element struct {
		Chart struct {
			Title string `json:"title"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(input, &element); err != nil {
		t.Fatal(err)
	}
	return element.Chart.Title
}
