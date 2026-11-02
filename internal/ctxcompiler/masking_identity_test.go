package ctxcompiler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestMaskIdentityAcrossLongReplay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaskTriggerTokens, cfg.MaskTargetTokens, cfg.LiteralObservations = 450, 200, 2
	ctrl := NewController(cfg, NewEstimator(4), "")
	var activity []ActivityEvent
	previous := map[string]string{}
	transitions := map[string]int{}
	originals := map[string]string{}
	for turn := 0; turn < 90; turn++ {
		id := fmt.Sprintf("compile-%d", turn)
		body := strings.Repeat("compiling source unit\n", 80)
		originals[id] = body
		activity = append(activity, ActivityEvent{Seq: int64(turn + 1), Role: providers.RoleTool, Content: body, ToolResult: &providers.ToolResult{CallID: id, Name: "exec", Output: body}})
		// Each replay restores controller state and history as a real request does.
		ctrl.ApplyMask(activity, nil, false)
		for i, ev := range activity {
			id := ev.ToolResult.CallID
			raw, _ := json.Marshal(ev.ToolResult)
			if old, ok := previous[id]; ok && old != string(raw) {
				transitions[id]++
			}
			if transitions[id] > 1 {
				t.Fatalf("%s changed more than once", id)
			}
			previous[id] = string(raw)
			if i >= len(activity)-2 && ev.ToolResult.Output != originals[id] {
				t.Fatal("protected recent output changed")
			}
			if ev.Mask != nil && (ev.Mask.Digest != shortDigest(originals[id]) || ev.Mask.Lines != 80 || ev.Mask.CallID != id || ev.Content != originals[id]) {
				t.Fatalf("lost original identity: %+v", ev.Mask)
			}
		}
		raw, _ := json.Marshal(activity)
		if err := json.Unmarshal(raw, &activity); err != nil {
			t.Fatal(err)
		}
		ctrl = NewController(cfg, NewEstimator(4), "")
	}
	if activity[0].Mask == nil {
		t.Fatal("no masking occurred")
	}
	// Original literal blobs are independent of the rendered history.
	if !strings.Contains(RenderMarker(Observation{CallID: "compile-0", Mask: activity[0].Mask}), shortDigest(originals["compile-0"])) {
		t.Fatal("marker hashed itself")
	}
}

func TestMaskIdentitySwitch(t *testing.T) {
	o := Observation{CallID: "c", Output: "full"}
	o.Output = RenderMarker(o)
	if !alreadyMasked(o) {
		t.Fatal("legacy marker not recognized")
	}
	t.Setenv("EXCALIBUR_MASK_IDEMPOTENT", "0")
	if alreadyMasked(o) {
		t.Fatal("switch ignored")
	}
}
