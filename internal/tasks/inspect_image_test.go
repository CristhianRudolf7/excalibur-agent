package tasks

import (
	"context"
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisualInspectionVerifierGuidanceAndEvidence(t *testing.T) {
	history := []HistoryTool{{Name: "inspect_image", Args: `{"path":"after.png","compare_to":"before.png"}`, Output: "format: png\nsize: 80x60\nblank/uniform: false\nchanged_pixels: 1200 / 4800 (25.0000%)\nchanged_bbox: x=20 y=10 width=40 height=30"}}
	for _, generalize := range []bool{false, true} {
		cfg := DefaultSelfVerifyConfig()
		cfg.EnableGeneralizeChecks = generalize
		t.Setenv("EXCALIBUR_INSPECT_IMAGE", "0")
		off := StrongVerifierPromptWithConfig(cfg, VerifierSmallContext{}, nil, "")
		if strings.Contains(off, "captured-image inspect_image") {
			t.Fatal("disabled guidance present")
		}
		t.Setenv("EXCALIBUR_INSPECT_IMAGE", "1")
		on := StrongVerifierPromptWithConfig(cfg, VerifierSmallContext{}, nil, "")
		if delta := len(on) - len(off); delta != len(visualInspectionReview) || delta > 250 {
			t.Fatalf("verifier prompt delta %d bytes", delta)
		}
		t.Logf("verifier generalize=%t: off=%d on=%d delta=%d bytes", generalize, len(off), len(on), len(on)-len(off))
		ctx := BuildVerifierContext("Show a window that changes after input", nil, history, 20)
		if len(ctx.Evidence) != 1 || !strings.Contains(ctx.Evidence[0], "inspect_image") {
			t.Fatalf("inspection evidence discarded: %+v", ctx)
		}
		prompt := StrongVerifierPromptWithConfig(cfg, ctx, history, "")
		for _, want := range []string{"require captured-image inspect_image evidence", "blank/unchanged screens and process/port checks do not prove it", "before/after compare_to evidence", `"compare_to":"before.png"`, "changed_pixels: 1200", "changed_bbox: x=20 y=10"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("verifier missing %q: %s", want, prompt)
			}
		}
		bad := append([]HistoryTool(nil), history...)
		bad[0].IsError = true
		if got := BuildVerifierContext("visual result", nil, bad, 20); len(got.Evidence) != 0 {
			t.Fatal("failed inspection counted as evidence")
		}
	}
}

func TestVisualInspectionVerifierRetainsDefaultThumbnails(t *testing.T) {
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "1")
	root := t.TempDir()
	for _, name := range []string{"before.png", "after.png"} {
		img := image.NewNRGBA(image.Rect(0, 0, 80, 60))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
		if name == "after.png" {
			draw.Draw(img, image.Rect(20, 10, 60, 40), image.NewUniform(color.White), image.Point{}, draw.Src)
		}
		f, err := os.Create(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	args := `{"path":"after.png","compare_to":"before.png"}`
	tool := &tools.InspectImageTool{Deps: &tools.FileDeps{Workdir: root}}
	result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(args)})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatalf("image inspection: %+v %v", result, err)
	}
	history := []HistoryTool{{Name: "inspect_image", Args: args, Output: result.Output}}
	cfg := DefaultSelfVerifyConfig()
	cfg.EnableGeneralizeChecks = false
	prompt := StrongVerifierPromptWithConfig(cfg, BuildVerifierContext("inspect rendered output", nil, history, 20), history, "")
	for _, want := range []string{"changed_pixels: 1200 / 4800", "changed_bbox: x=20 y=10 width=40 height=30", "difference_mask:", oneLine(result.Output)} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("verifier lost image evidence %q", want)
		}
	}
}
