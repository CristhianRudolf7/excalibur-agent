package whiteboard

import (
	"encoding/json"
	"fmt"
)

// applyOps emits only native operations, including explicit group removals, so
// persistence, event replay and clients all see the same expanded scene.
func applyOps(b *Board, input []Op, author string) ([]Op, error) {
	if len(input) == 0 || len(input) > 500 {
		return nil, fmt.Errorf("ops must contain 1 to 500 operations")
	}
	var output []Op
	count := 0
	apply := func(op Op) error {
		count += len(op.Elements) + len(op.IDs)
		if count > MaxElements {
			return fmt.Errorf("max 500 expanded elements per call")
		}
		if err := applyNativeOps(b, []Op{op}, author); err != nil {
			return err
		}
		output = append(output, op)
		return nil
	}
	for _, op := range input {
		switch op.Op {
		case "add", "update", "set_scene":
			if len(op.Elements) == 0 && op.Op != "set_scene" {
				return nil, fmt.Errorf("elements required")
			}
			// Keep set_scene atomic, including when it expands into several additions.
			if op.Op == "set_scene" {
				if err := apply(Op{Op: "set_scene"}); err != nil {
					return nil, err
				}
			}
			seen := map[string]bool{}
			for _, raw := range op.Elements {
				var err error
				raw, err = normalizeElement(raw)
				if err != nil {
					return nil, err
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(raw, &fields); err != nil {
					return nil, err
				}
				var id, kind string
				_ = json.Unmarshal(fields["id"], &id)
				_ = json.Unmarshal(fields["type"], &kind)
				if seen[id] {
					return nil, fmt.Errorf("duplicate element id %q", id)
				}
				seen[id] = true
				var anchor *Element
				for i := range b.Elements {
					if b.Elements[i].ID == id && b.Elements[i].GroupID == id {
						anchor = &b.Elements[i]
						break
					}
				}
				_, hasChart := fields["chart"]
				if kind == "chart" || hasChart {
					if op.Op == "update" && anchor == nil {
						return nil, fmt.Errorf("unknown chart %q", id)
					}
					if op.Op == "update" {
						old, _ := json.Marshal(anchor)
						var merged map[string]json.RawMessage
						_ = json.Unmarshal(old, &merged)
						for k, v := range fields {
							if k != "author" {
								merged[k] = v
							}
						}
						merged["type"] = json.RawMessage(`"chart"`)
						raw, _ = json.Marshal(merged)
					}
					e, err := decodeChart(raw)
					if err != nil {
						return nil, err
					}
					if e.Type != "chart" {
						return nil, fmt.Errorf("chart payload requires type chart")
					}
					e.Author = author
					if anchor != nil && op.Op == "update" {
						e.Author = anchor.Author
					}
					expanded, err := expandChart(e.Element, e.Chart)
					if err != nil {
						return nil, err
					}
					if op.Op == "update" {
						ids := groupIDs(b, id)
						if err := apply(Op{Op: "remove", IDs: ids}); err != nil {
							return nil, err
						}
					}
					raws := make([]json.RawMessage, len(expanded))
					for i, member := range expanded {
						raws[i], _ = json.Marshal(member)
					}
					// Native add assigns authorship at the boundary. An update retains it.
					if op.Op == "update" {
						if err := applyNativeOps(b, []Op{{Op: "add", Elements: raws}}, e.Author); err != nil {
							return nil, err
						}
						count += len(raws)
						if count > MaxElements {
							return nil, fmt.Errorf("max 500 expanded elements per call")
						}
						output = append(output, Op{Op: "add", Elements: raws})
					} else if err := apply(Op{Op: "add", Elements: raws}); err != nil {
						return nil, err
					}
				} else if op.Op == "update" && anchor != nil {
					// Geometry updates move/resize every member relative to the chart bounds.
					merged, _ := json.Marshal(anchor)
					var patch map[string]json.RawMessage
					_ = json.Unmarshal(merged, &patch)
					for k, v := range fields {
						if k != "author" {
							patch[k] = v
						}
					}
					merged, _ = json.Marshal(patch)
					var updated Element
					if err := decodeNative(merged, &updated); err != nil {
						return nil, err
					}
					if err := validElement(updated); err != nil {
						return nil, err
					}
					sx, sy := 1.0, 1.0
					if anchor.Width > 0 {
						sx = updated.Width / anchor.Width
					}
					if anchor.Height > 0 {
						sy = updated.Height / anchor.Height
					}
					var patches []json.RawMessage
					for _, member := range b.Elements {
						if member.GroupID != id {
							continue
						}
						member.X = updated.X + (member.X-anchor.X)*sx
						member.Y = updated.Y + (member.Y-anchor.Y)*sy
						member.Width *= sx
						member.Height *= sy
						for i := range member.Points {
							member.Points[i][0] *= sx
							member.Points[i][1] *= sy
						}
						if supplied, ok := fields["style"]; ok {
							oldStyle, _ := json.Marshal(member.Style)
							styleFields := map[string]json.RawMessage{"style": oldStyle}
							stylePatch := map[string]json.RawMessage{"style": supplied}
							mergeStyle(styleFields, stylePatch)
							if err := json.Unmarshal(stylePatch["style"], &member.Style); err != nil {
								return nil, err
							}
						}
						if _, ok := fields["locked"]; ok {
							member.Locked = updated.Locked
						}
						member.Z = updated.Z
						r, _ := json.Marshal(member)
						patches = append(patches, r)
					}
					if err := apply(Op{Op: "update", Elements: patches}); err != nil {
						return nil, err
					}
				} else {
					action := op.Op
					if action == "set_scene" {
						action = "add"
					}
					if err := apply(Op{Op: action, Elements: []json.RawMessage{raw}}); err != nil {
						return nil, err
					}
				}
			}
		case "remove":
			if len(op.IDs) == 0 {
				return nil, fmt.Errorf("ids required")
			}
			ids := map[string]bool{}
			var expanded []string
			for _, id := range op.IDs {
				for _, member := range append([]string{id}, groupIDs(b, id)...) {
					if !ids[member] {
						ids[member] = true
						expanded = append(expanded, member)
					}
				}
			}
			if err := apply(Op{Op: "remove", IDs: expanded}); err != nil {
				return nil, err
			}
		default:
			if err := apply(op); err != nil {
				return nil, err
			}
		}
	}
	return output, nil
}
func groupIDs(b *Board, id string) []string {
	var ids []string
	for _, e := range b.Elements {
		if e.GroupID == id {
			ids = append(ids, e.ID)
		}
	}
	return ids
}
