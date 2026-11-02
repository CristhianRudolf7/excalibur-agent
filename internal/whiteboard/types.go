// Package whiteboard implements persistent interactive conversation boards.
package whiteboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

const MaxElements = 500
const MaxBytes = 2 << 20

type Viewport = api.Viewport
type Style = api.Style
type Element = api.Element
type Board = api.Board
type BoardSummary = api.BoardSummary
type Op = api.Op

func validElement(e Element) error {
	if strings.TrimSpace(e.ID) == "" || len(e.ID) > 200 {
		return fmt.Errorf("element id is required (max 200 bytes)")
	}
	switch e.Type {
	case "rect", "ellipse", "diamond", "line", "arrow", "text", "freedraw", "image", "frame", "math", "mermaid", "code", "sticky":
	default:
		return fmt.Errorf("invalid element type %q", e.Type)
	}
	if e.Author != "agent" && e.Author != "user" {
		return fmt.Errorf("invalid author")
	}
	for _, v := range []float64{e.X, e.Y, e.Width, e.Height, e.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
			return fmt.Errorf("invalid geometry")
		}
	}
	if e.Width < 0 || e.Height < 0 || len(e.Points) > 10000 {
		return fmt.Errorf("invalid dimensions or points")
	}
	for _, p := range e.Points {
		for _, v := range p {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
				return fmt.Errorf("invalid point")
			}
		}
	}
	if e.Style != nil && ((e.Style.StrokeWidth != nil && *e.Style.StrokeWidth < 0) || (e.Style.FontSize != nil && *e.Style.FontSize < 0) || (e.Style.Opacity != nil && (*e.Style.Opacity < 0 || *e.Style.Opacity > 1))) {
		return fmt.Errorf("invalid style")
	}
	return nil
}
func applyOps(b *Board, ops []Op, author string) error {
	if len(ops) == 0 || len(ops) > 500 {
		return fmt.Errorf("ops must contain 1 to 500 operations")
	}
	count := 0
	for _, op := range ops {
		count += len(op.Elements) + len(op.IDs)
		if count > MaxElements || len(op.IDs) > MaxElements {
			return fmt.Errorf("max 500 elements per call")
		}
		switch op.Op {
		case "clear":
			kept := []Element{}
			if op.KeepUser {
				for _, e := range b.Elements {
					if e.Author == "user" {
						kept = append(kept, e)
					}
				}
			}
			b.Elements = kept
		case "set_scene", "add", "update":
			if op.Op != "set_scene" && len(op.Elements) == 0 {
				return fmt.Errorf("elements required")
			}
			if op.Op == "set_scene" {
				b.Elements = []Element{}
			}
			seen := map[string]bool{}
			for _, raw := range op.Elements {
				var patch map[string]json.RawMessage
				if err := json.Unmarshal(raw, &patch); err != nil {
					return err
				}
				if points, ok := patch["points"]; ok {
					var ps [][]float64
					if err := json.Unmarshal(points, &ps); err != nil {
						return err
					}
					for _, p := range ps {
						if len(p) != 2 {
							return fmt.Errorf("each point must contain x and y")
						}
					}
				}
				var id string
				if err := json.Unmarshal(patch["id"], &id); err != nil {
					return fmt.Errorf("element id required")
				}
				if seen[id] {
					return fmt.Errorf("duplicate element id")
				}
				seen[id] = true
				idx := -1
				for i, e := range b.Elements {
					if e.ID == id {
						idx = i
						break
					}
				}
				if op.Op == "update" && idx < 0 {
					return fmt.Errorf("unknown element %q", id)
				}
				if op.Op != "update" && idx >= 0 {
					return fmt.Errorf("element %q already exists", id)
				}
				var e Element
				if op.Op == "update" {
					old, _ := json.Marshal(b.Elements[idx])
					var fields map[string]json.RawMessage
					_ = json.Unmarshal(old, &fields)
					// Merge only supplied properties: rebasing leaves unrelated elements and fields intact.
					for k, v := range patch {
						if k != "author" {
							fields[k] = v
						}
					}
					raw, _ = json.Marshal(fields)
				}
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&e); err != nil {
					return err
				}
				if op.Op != "update" {
					e.Author = author
				}
				if err := validElement(e); err != nil {
					return err
				}
				if op.Op != "update" {
					normalized, _ := json.Marshal(e)
					op.Elements[len(seen)-1] = normalized
				} else {
					delete(patch, "author")
					normalized, _ := json.Marshal(patch)
					op.Elements[len(seen)-1] = normalized
				}
				if idx < 0 {
					b.Elements = append(b.Elements, e)
				} else {
					b.Elements[idx] = e
				}
			}
		case "remove":
			if len(op.IDs) == 0 {
				return fmt.Errorf("ids required")
			}
			ids := map[string]bool{}
			for _, id := range op.IDs {
				ids[id] = true
			}
			kept := []Element{}
			for _, e := range b.Elements {
				if !ids[e.ID] {
					kept = append(kept, e)
				}
			}
			b.Elements = kept
		default:
			return fmt.Errorf("invalid op %q", op.Op)
		}
	}
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if len(data) > MaxBytes {
		return fmt.Errorf("board exceeds 2 MiB")
	}
	return nil
}
