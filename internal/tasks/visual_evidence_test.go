package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func visualHistory(path, text string) HistoryTool {
	args, _ := json.Marshal(map[string]string{"path": path, "compare_to": "prior.png"})
	return HistoryTool{Name: "inspect_image", Args: string(args), Output: "format: png\nsize: 800x600\nblank/uniform: false\nchanged_pixels: 48 / 480000 (0.0100%; per-channel tolerance 8)\n" + strings.Repeat("metadata: detail\n", 70) + "screen_text:\n" + text + "\nASCII (dark to light):\nthumbnail-body"}
}

func TestVerifierVisualEvidenceLatestPerCaptureBeyondLineBudget(t *testing.T) {
	history := []HistoryTool{visualHistory("./view.png", "obsolete observation")}
	for i := 0; i < 30; i++ {
		history = append(history, HistoryTool{Name: "exec", Args: `{"command":"check"}`, Output: "ordinary evidence"})
	}
	history = append(history, visualHistory("prior.png", "prior view"), visualHistory("view.png", "First readable row\nSecond readable row\nThird readable row\nFourth row omitted"))
	wrapped := visualHistory("other.png", "other current view")
	wrapped.Name = "call_tool"
	wrapped.Args = `{"name":"inspect_image","arguments":` + wrapped.Args + `}`
	history = append(history, wrapped)
	ctx := BuildVerifierContext("Show the requested rendered output", nil, history, 1)
	if len(ctx.Evidence) != 1 {
		t.Fatalf("ordinary line limit changed: %+v", ctx.Evidence)
	}
	visual := ctx.VisualEvidence
	for _, want := range []string{`path="view.png"`, `path="prior.png"`, `path="other.png"`, `compare_to="prior.png"`, "First readable row | Second readable row | Third readable row", "blank/uniform: false", "(0.0100%;"} {
		if !strings.Contains(visual, want) {
			t.Fatalf("missing %q: %s", want, visual)
		}
	}
	for _, unwanted := range []string{"obsolete observation", "Fourth row omitted", "thumbnail-body", "metadata: detail"} {
		if strings.Contains(visual, unwanted) {
			t.Fatalf("stale/unbounded visual output %q: %s", unwanted, visual)
		}
	}
	if strings.Index(visual, `path="other.png"`) > strings.Index(visual, `path="view.png"`) {
		t.Fatal("latest captures lost priority")
	}
	prompt := StrongVerifierPromptWithConfig(DefaultSelfVerifyConfig(), ctx, history, "")
	if !strings.Contains(prompt, visualEndStateReview) || !strings.Contains(prompt, visual) {
		t.Fatal("verifier prompt lacks end-state evidence/rule")
	}
}

func TestVerifierVisualEvidenceBoundAndAblation(t *testing.T) {
	var history []HistoryTool
	for i := 0; i < 40; i++ {
		history = append(history, visualHistory(fmt.Sprintf("capture-%d.png", i), strings.Repeat("界", 500)+"\n"+strings.Repeat("é", 500)))
	}
	visual := BuildVerifierContext("Display requested output", nil, history, 1).VisualEvidence
	if len(visual) > 1500 || !utf8.ValidString(visual) || !strings.Contains(visual, "older capture summaries omitted") || !strings.Contains(visual, `path="capture-39.png"`) {
		t.Fatalf("invalid bounded summary (%d bytes): %s", len(visual), visual)
	}
	t.Setenv(VisualVerifierEvidenceSwitch, "0")
	ctx := BuildVerifierContext("Display requested output", nil, history, 1)
	if ctx.VisualEvidence != "" || len(ctx.Evidence) != 1 {
		t.Fatalf("disabled summary changed legacy evidence: %+v", ctx)
	}
	if strings.Contains(StrongVerifierPromptWithConfig(DefaultSelfVerifyConfig(), ctx, history, ""), visualEndStateReview) {
		t.Fatal("disabled visual prompt rule present")
	}
	t.Setenv(VisualVerifierEvidenceSwitch, "1")
	t.Setenv("EXCALIBUR_INSPECT_IMAGE", "0")
	if got := LatestVisualEvidence(history); got != "" {
		t.Fatal("disabled image inspection still produced visual evidence")
	}
}

func TestVerifierVisualEvidenceFailedInspectionDoesNotReuseOldState(t *testing.T) {
	old := visualHistory("view.png", "previous success")
	failed := old
	failed.IsError = true
	failed.Output = "cannot read capture"
	ctx := BuildVerifierContext("Display requested output", nil, []HistoryTool{old, failed}, 20)
	if !strings.Contains(ctx.VisualEvidence, "inspection failed") || strings.Contains(ctx.VisualEvidence, "previous success") {
		t.Fatalf("stale successful evidence: %+v", ctx)
	}
}

func TestVerifierVisualEvidenceMissingTextAndComparisonExplicit(t *testing.T) {
	history := []HistoryTool{{Name: "inspect_image", Args: `{"path":"blank.png"}`, Output: "blank/uniform: true (color #000000)\nASCII (dark to light):\n"}}
	got := LatestVisualEvidence(history)
	for _, want := range []string{"screen_text: unavailable", "blank/uniform: true", "changed_pixels: unavailable (no comparison)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
}
