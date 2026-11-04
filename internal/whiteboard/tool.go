package whiteboard

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type Tool struct {
	Service        *Service
	ConversationID string
}

func (t *Tool) Definition() tools.Definition {
	return tools.Definition{
		Name: "whiteboard", Description: `Draw on the user's shared whiteboard with native shapes, labels, math, mermaid, code or sticky notes. Draw a labelled chart in ONE call (opens a board automatically): {"action":"add","elements":[{"id":"expenses","type":"chart","x":40,"y":80,"width":560,"height":340,"chart":{"kind":"bar","title":"Weekly expenses","unit":"S/","labels":["Food","Travel"],"values":[69.5,20]}}]}. Chart kinds: bar, column, line, pie (percentage legend); 1–24 labels/values, optional colors. Update with a new chart payload or remove by chart id to affect the whole group. Primitive styles use style:{"fill":"#4f86e8","font_size":20}; common top-level style shortcuts also work. Otherwise open before drawing. Use animate:true, step_ms:300 for reveals, focus/highlight while explaining. Read before editing user work; include_image:true waits up to 8s for a UI PNG. clear keep_user:true preserves user work.`,
		InputSchema: json.RawMessage(`{"type":"object","required":["action"],"properties":{"action":{"type":"string","enum":["open","close","add","update","remove","clear","set_scene","focus","highlight","annotate","step","read","export","list","rename","delete"]},"board_id":{"type":"string"},"title":{"type":"string"},"layout":{"enum":["panel","fullscreen"]},"elements":{"type":"array","maxItems":500,"items":{"type":"object"}},"ids":{"type":"array","maxItems":500,"items":{"type":"string"}},"animate":{"type":"boolean"},"step_ms":{"type":"integer","minimum":0,"maximum":10000},"keep_user":{"type":"boolean"},"rect":{"type":"object"},"zoom":{"type":"number","exclusiveMinimum":0},"style":{"enum":["laser","pulse","box"]},"duration_ms":{"type":"integer","minimum":0,"maximum":60000},"target_id":{"type":"string"},"text":{"type":"string"},"steps":{"type":"array","maxItems":500,"items":{"type":"object"}},"delay_ms":{"type":"integer","minimum":0,"maximum":10000},"include_image":{"type":"boolean"},"format":{"enum":["png","svg","json"]}},"additionalProperties":false}`),
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelCooperative, Timeout: 2 * time.Minute, OutputBudget: 32000, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"}, Group: "creative-tools", Keywords: []string{"teach", "explain", "diagram", "draw", "whiteboard"},
	}
}

type action struct {
	Action   string            `json:"action"`
	BoardID  string            `json:"board_id"`
	Title    string            `json:"title"`
	Layout   string            `json:"layout"`
	Elements []json.RawMessage `json:"elements"`
	IDs      []string          `json:"ids"`
	Animate  bool              `json:"animate"`
	StepMS   int               `json:"step_ms"`
	KeepUser bool              `json:"keep_user"`
	Rect     *struct {
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"rect"`
	Zoom         float64           `json:"zoom"`
	Style        string            `json:"style"`
	DurationMS   int               `json:"duration_ms"`
	TargetID     string            `json:"target_id"`
	Text         string            `json:"text"`
	Steps        []json.RawMessage `json:"steps"`
	DelayMS      int               `json:"delay_ms"`
	IncludeImage bool              `json:"include_image"`
	Format       string            `json:"format"`
	Op           string            `json:"op"`
}

func (t *Tool) ResourceKeys(raw json.RawMessage) ([]string, error) {
	return []string{"whiteboard"}, nil
}
func (t *Tool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	if !Enabled() {
		return tools.Result{Status: tools.StatusDenied, Output: "Whiteboard is disabled."}, nil
	}
	if !json.Valid(inv.Arguments) {
		return tools.Result{Status: tools.StatusInputError, Output: "invalid JSON arguments"}, nil
	}
	if len(inv.Arguments) > MaxBytes {
		return tools.Result{Status: tools.StatusInputError, Output: "input exceeds 2 MiB"}, nil
	}
	var a action
	dec := json.NewDecoder(bytes.NewReader(inv.Arguments))
	dec.DisallowUnknownFields()
	err := dec.Decode(&a)
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	conv := Conversation(ctx)
	if conv == "" {
		conv = t.ConversationID
	}
	if conv == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "conversation context required"}, nil
	}
	out, err := t.execute(ctx, conv, a)
	if err != nil {
		status := tools.StatusToolError
		var apiErr *api.Error
		if errors.As(err, &apiErr) && (apiErr.Code == api.CodeInvalidParams || apiErr.Code == api.CodeUnknownTarget) {
			status = tools.StatusInputError
		}
		if ctx.Err() != nil {
			status = tools.StatusCanceled
		}
		return tools.Result{Status: status, Output: err.Error()}, nil
	}
	raw, err := json.Marshal(out)
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, err
}
func (t *Tool) execute(ctx context.Context, conv string, a action) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := t.Service
	if s == nil {
		return nil, fmt.Errorf("whiteboard service unavailable")
	}
	if a.StepMS < 0 || a.StepMS > 10000 || a.DelayMS < 0 || a.DelayMS > 10000 || a.DurationMS < 0 || a.DurationMS > 60000 || len(a.IDs) > 500 {
		return nil, invalid("invalid timing or element count")
	}
	if a.Action == "list" {
		boards, err := s.Store.List(ctx, conv)
		return map[string]any{"boards": boards}, err
	}
	if a.Action == "open" {
		b, err := s.Open(ctx, conv, a.BoardID, a.Title, a.Layout)
		return map[string]any{"board_id": b.ID, "version": b.Version}, err
	}

	if a.BoardID == "" {
		var err error
		a.BoardID, err = s.Current(ctx, conv)
		if err != nil {
			return nil, err
		}
		if a.BoardID == "" {
			chartRequest := false
			chartTitle := ""
			if a.Action == "add" || a.Action == "set_scene" {
				for _, raw := range a.Elements {
					var element struct {
						Type  string `json:"type"`
						Chart struct {
							Title string `json:"title"`
						} `json:"chart"`
					}
					if json.Unmarshal(raw, &element) == nil && element.Type == "chart" {
						chartRequest = true
						if chartTitle == "" {
							chartTitle = strings.TrimSpace(element.Chart.Title)
						}
					}
				}
			}
			if !chartRequest {
				return nil, invalid("open a board first")
			}
			// Validate before creating a board so invalid charts leave no empty board.
			if _, err := applyOps(&Board{}, []Op{{Op: a.Action, Elements: a.Elements}}, "agent"); err != nil {
				return nil, invalid(err.Error())
			}
			title := a.Title
			if title == "" {
				// An automatically opened board takes the chart's own title
				// instead of the generic default.
				title = chartTitle
			}
			opened, err := s.Open(ctx, conv, "", title, "panel")
			if err != nil {
				return nil, err
			}
			a.BoardID = opened.ID
		}
	}

	b, err := s.Store.Get(ctx, a.BoardID)
	if err != nil {
		return nil, err
	}
	if b.ConversationID != conv {
		return nil, invalid("board belongs to another conversation")
	}
	compact := func(b Board) any {
		return map[string]any{"board_id": b.ID, "version": b.Version, "count": len(b.Elements)}
	}
	switch a.Action {
	case "add", "update", "remove", "clear", "set_scene":
		b, err = s.Apply(ctx, b.ID, -1, newID(), "agent", []Op{{Op: a.Action, Elements: a.Elements, IDs: a.IDs, KeepUser: a.KeepUser}}, a.Animate, a.StepMS)
		return compact(b), err
	case "read":
		return s.Read(ctx, b, a.IncludeImage)
	case "close":
		return compact(b), s.Close(ctx, b.ID)
	case "rename":
		b, err = s.Rename(ctx, b.ID, a.Title)
		return compact(b), err
	case "delete":
		err = s.Delete(ctx, b.ID)
		return map[string]any{"board_id": b.ID, "deleted": err == nil}, err
	case "focus", "highlight":
		if a.Zoom < 0 || a.Zoom > 100 {
			return nil, invalid("invalid zoom")
		}
		if a.Action == "highlight" {
			if len(a.IDs) == 0 {
				return nil, invalid("highlight ids required")
			}
			if a.Style == "" {
				a.Style = "pulse"
			}
			if a.DurationMS == 0 {
				a.DurationMS = 1500
			}
			if a.Style != "laser" && a.Style != "pulse" && a.Style != "box" {
				return nil, invalid("invalid highlight style")
			}
		}
		for _, id := range a.IDs {
			found := false
			for _, e := range b.Elements {
				if e.ID == id {
					found = true
				}
			}
			if !found {
				return nil, invalid("unknown element " + id)
			}
		}

		fields := map[string]any{"ids": a.IDs, "rect": a.Rect, "zoom": a.Zoom, "style": a.Style, "duration_ms": a.DurationMS}
		b, err = s.Present(ctx, b.ID, a.Action, fields)
		return compact(b), err

	case "annotate":
		var target *Element
		for i := range b.Elements {
			if b.Elements[i].ID == a.TargetID {
				target = &b.Elements[i]
			}
		}
		if target == nil || strings.TrimSpace(a.Text) == "" {
			return nil, invalid("target_id and text required")
		}
		x := target.X + target.Width + 40
		y := target.Y
		label, _ := json.Marshal(Element{ID: newID(), Type: "text", X: x, Y: y, Text: a.Text, Author: "agent"})
		arrow, _ := json.Marshal(Element{ID: newID(), Type: "arrow", X: x, Y: y + 20, Points: [][2]float64{{0, 0}, {target.X + target.Width/2 - x, target.Height/2 - 20}}, Author: "agent"})
		b, err = s.Apply(ctx, b.ID, -1, newID(), "agent", []Op{{Op: "add", Elements: []json.RawMessage{label, arrow}}}, false, 0)
		return compact(b), err
	case "step":
		if len(a.Steps) == 0 || len(a.Steps) > 500 {
			return nil, invalid("steps must contain 1 to 500 operations")
		}
		count := 0
		steps := []action{}
		for _, raw := range a.Steps {
			var step action
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if err := d.Decode(&step); err != nil {
				return nil, invalid(err.Error())
			}
			if step.Action == "" {
				step.Action = step.Op
			}
			switch step.Action {
			case "add", "update", "remove", "focus", "highlight":
			default:
				return nil, invalid("invalid step op")
			}
			count += len(step.Elements) + len(step.IDs)
			if count > 500 {
				return nil, invalid("max 500 elements per call")
			}
			step.BoardID = b.ID
			steps = append(steps, step)
		}
		for i, step := range steps {
			if i > 0 && a.DelayMS > 0 {
				timer := time.NewTimer(time.Duration(a.DelayMS) * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
			if _, err = t.execute(ctx, conv, step); err != nil {
				return nil, err
			}
		}
		b, err = s.Store.Get(ctx, b.ID)
		return compact(b), err
	case "export":
		if a.Format == "png" {
			snapshot, err := s.Read(ctx, b, true)
			if err != nil {
				return nil, err
			}
			ref, ok := snapshot["image_blob"].(string)
			if !ok {
				return nil, fmt.Errorf("PNG export timed out waiting for the UI snapshot")
			}
			return map[string]any{"blob_ref": ref, "mime": "image/png"}, nil
		}
		if s.Store.DB == nil {
			return nil, invalid("blob storage unavailable")
		}
		var data []byte
		mime := "application/json"
		switch a.Format {
		case "json":
			data, err = json.Marshal(b)
		case "svg":
			mime = "image/svg+xml"
			data = []byte(renderSVG(b))
		default:
			return nil, invalid("format must be png, svg or json")
		}
		if err != nil {
			return nil, err
		}
		ref, err := s.Store.DB.Blob().Put(ctx, bytes.NewReader(data))
		return map[string]any{"blob_ref": ref, "mime": mime}, err
	default:
		return nil, invalid("unknown whiteboard action")
	}
}
func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func renderSVG(b Board) string {
	var out strings.Builder
	minX, minY, maxX, maxY := 0.0, 0.0, 600.0, 400.0
	for _, e := range b.Elements {
		if e.X < minX {
			minX = e.X
		}
		if e.Y < minY {
			minY = e.Y
		}
		if e.X+e.Width+200 > maxX {
			maxX = e.X + e.Width + 200
		}
		if e.Y+e.Height+40 > maxY {
			maxY = e.Y + e.Height + 40
		}
		for _, p := range e.Points {
			x, y := e.X+p[0], e.Y+p[1]
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%g %g %g %g"><defs><marker id="arrowhead" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="context-stroke"/></marker></defs>`, minX-40, minY-40, maxX-minX+80, maxY-minY+80)
	for _, e := range b.Elements {
		stroke, fill := "black", "none"
		width := 2.0
		if e.Style != nil {
			if e.Style.Stroke != "" {
				stroke = e.Style.Stroke
			}
			if e.Style.Fill != "" {
				fill = e.Style.Fill
			}
			if e.Style.StrokeWidth != nil {
				width = *e.Style.StrokeWidth
			}
		}
		fmt.Fprintf(&out, `<g stroke="%s" fill="%s" stroke-width="%g">`, escape(stroke), escape(fill), width)
		switch e.Type {
		case "ellipse":
			fmt.Fprintf(&out, `<ellipse cx="%g" cy="%g" rx="%g" ry="%g"/>`, e.X+e.Width/2, e.Y+e.Height/2, e.Width/2, e.Height/2)
		case "line", "arrow", "freedraw":
			marker := ""
			if e.Type == "arrow" {
				marker = ` marker-end="url(#arrowhead)"`
			}
			fmt.Fprintf(&out, `<polyline fill="none"%s points="`, marker)
			points := e.Points
			if len(points) == 0 && e.Type != "freedraw" {
				points = [][2]float64{{0, 0}, {e.Width, e.Height}}
			}
			for _, p := range points {
				fmt.Fprintf(&out, "%g,%g ", e.X+p[0], e.Y+p[1])
			}
			out.WriteString(`"/>`)
		case "diamond":
			fmt.Fprintf(&out, `<polygon points="%g,%g %g,%g %g,%g %g,%g"/>`, e.X+e.Width/2, e.Y, e.X+e.Width, e.Y+e.Height/2, e.X+e.Width/2, e.Y+e.Height, e.X, e.Y+e.Height/2)
		case "image":
			fmt.Fprintf(&out, `<image x="%g" y="%g" width="%g" height="%g" href="%s"/>`, e.X, e.Y, e.Width, e.Height, escape(e.Src))
		default:
			fmt.Fprintf(&out, `<rect x="%g" y="%g" width="%g" height="%g"/>`, e.X, e.Y, e.Width, e.Height)
		}
		text := e.Text
		if text == "" {
			text = e.Latex + e.Code + e.Mermaid
		}
		if text != "" {
			fmt.Fprintf(&out, `<text x="%g" y="%g" fill="%s" stroke="none">%s</text>`, e.X, e.Y+20, escape(stroke), escape(text))
		}
		out.WriteString(`</g>`)
	}
	out.WriteString(`</svg>`)
	return out.String()
}
