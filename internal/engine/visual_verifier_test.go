package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestVisualVerifierFullAndRecoveryContextRetainScreenState(t *testing.T) {
	t.Setenv(tasks.VisualVerifierEvidenceSwitch, "1")
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		{Message: providers.Message{Content: ""}},
		{Message: providers.Message{Content: "VERDICT: FAIL\nREASONS: requested rendered output is absent"}},
	}}
	e, _ := testEngine(model, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Request = "Show the requested rendered output"
	cfg := *verifyConfig()
	cfg.EnableGeneralizeChecks = false
	history := []tasks.HistoryTool{}
	for i := 0; i < 25; i++ {
		history = append(history, tasks.HistoryTool{Name: "exec", Args: `{"command":"check"}`, Output: "ok"})
	}
	history = append(history, tasks.HistoryTool{Name: "inspect_image", Args: `{"path":"view.png","compare_to":"prior.png"}`, Output: "blank/uniform: false\nchanged_pixels: 0 / 100 (0.0000%)\n" + strings.Repeat("metadata: detail\n", 70) + "screen_text:\nRendering is paused; input is required\nASCII (dark to light):\n"})
	// No filename-derived draft is required to review an observed visual state.
	if ok, _ := e.runStrongVerifier(context.Background(), r, cfg, nil, history, "done"); ok {
		t.Fatal("visual failure was accepted")
	}
	if len(model.requests) != 2 {
		t.Fatalf("verifier calls=%d", len(model.requests))
	}
	for i, req := range model.requests {
		prompt := req.Messages[0].Content
		for _, want := range []string{"Rendering is paused; input is required", "blank/uniform: false", "0.0000%", "requested visual end state"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("verifier attempt %d missing %q: %s", i, want, prompt)
			}
		}
	}
}
