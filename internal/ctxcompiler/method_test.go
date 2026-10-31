package ctxcompiler_test

import (
	"strings"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

func TestDetectFamily(t *testing.T) {
	cases := []struct {
		model string
		want  ctxcompiler.PromptFamily
	}{
		{"gpt-6-luna", ctxcompiler.PromptGPT},
		{"GPT-4O", ctxcompiler.PromptGPT},
		{"openai-o3-mini", ctxcompiler.PromptGPT},
		{"claude-opus-4-6", ctxcompiler.PromptClaude},
		{"gemini-2.5-pro", ctxcompiler.PromptGemini},
		{"deepseek-chat", ctxcompiler.PromptDeepSeek},
		{"Grok-4", ctxcompiler.PromptGrok},
		{"qwen3-max", ctxcompiler.PromptQwen},
		{"", ctxcompiler.PromptUnknown},
		{"some-future-model-9", ctxcompiler.PromptUnknown},
		{"llama-4-scout", ctxcompiler.PromptUnknown},
	}
	for _, c := range cases {
		if got := ctxcompiler.DetectFamily(c.model); got != c.want {
			t.Errorf("DetectFamily(%q) = %s, want %s", c.model, got, c.want)
		}
	}
	if got := ctxcompiler.PromptUnknown.Default(); got != ctxcompiler.PromptGPT {
		t.Fatalf("unknown family must default to GPT, got %s", got)
	}
}

// The method must teach the full loop and name the failure modes
// seen in evaluation: stopping early and blind retries.
func TestWorkingMethodContent(t *testing.T) {
	m := ctxcompiler.WorkingMethod()
	for _, want := range []string{
		"Understand:", "Explore first:", "Plan briefly:",
		"Implement in small steps:", "Test what you built:",
		"On any error:", "tool-parameter error",
		"Finish only when", "Never declare success without evidence",
		"do not stop early", "working notes",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("working method missing %q", want)
		}
	}
}

func TestFamilyNoteVariants(t *testing.T) {
	fams := []ctxcompiler.PromptFamily{
		ctxcompiler.PromptGPT, ctxcompiler.PromptClaude,
		ctxcompiler.PromptGemini, ctxcompiler.PromptDeepSeek,
		ctxcompiler.PromptGrok, ctxcompiler.PromptQwen,
	}
	seen := map[string]ctxcompiler.PromptFamily{}
	for _, f := range fams {
		note := ctxcompiler.FamilyNote(f)
		if note == "" {
			t.Fatalf("empty family note for %s", f)
		}
		if prev, dup := seen[note]; dup {
			t.Fatalf("family %s shares its note with %s", f, prev)
		}
		seen[note] = f
	}
	if got := ctxcompiler.FamilyNote(ctxcompiler.PromptUnknown); got != ctxcompiler.FamilyNote(ctxcompiler.PromptGPT) {
		t.Fatal("unknown family must render the GPT default note")
	}
}

func TestMethodBlockAdapt(t *testing.T) {
	gptOn := ctxcompiler.MethodBlock(ctxcompiler.PromptGPT, true)
	qwenOn := ctxcompiler.MethodBlock(ctxcompiler.PromptQwen, true)
	if gptOn == qwenOn {
		t.Fatal("adapted blocks must differ per family")
	}
	gptOff := ctxcompiler.MethodBlock(ctxcompiler.PromptGPT, false)
	qwenOff := ctxcompiler.MethodBlock(ctxcompiler.PromptQwen, false)
	if gptOff != qwenOff {
		t.Fatal("unadapted blocks must be family-independent")
	}
	unknownOn := ctxcompiler.MethodBlock(ctxcompiler.PromptUnknown, true)
	if unknownOn != gptOn {
		t.Fatal("unknown model id must render the GPT default block")
	}
	if !strings.Contains(gptOff, "working method:") {
		t.Fatal("method block must carry the shared method")
	}
}

func TestWorkingNotesRendering(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	if cfg.WorkingNotesMax <= 0 {
		t.Fatal("default config must render working notes")
	}
	snap := testSnapshot()
	snap.Task.WorkingNotes = []string{"did read_file parser.go", "FAIL exec build: E0432"}
	c, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	l3 := c.LayerText(ctxcompiler.LayerTask)
	if !strings.Contains(l3, "working notes:") {
		t.Fatal("L3 must carry the pinned working notes")
	}
	for _, n := range snap.Task.WorkingNotes {
		if !strings.Contains(l3, n) {
			t.Errorf("L3 missing note %q", n)
		}
	}
	// Empty notes render nothing, so legacy bytes are unchanged.
	snap.Task.WorkingNotes = nil
	plain, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.LayerText(ctxcompiler.LayerTask), "working notes:") {
		t.Fatal("empty notes must not render a section")
	}
	// The cap keeps the most recent notes in order.
	many := make([]string, 0, cfg.WorkingNotesMax+5)
	for i := 0; i < cfg.WorkingNotesMax+5; i++ {
		many = append(many, "note")
	}
	many[cfg.WorkingNotesMax+4] = "newest"
	snap.Task.WorkingNotes = many
	capped, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	l3 = capped.LayerText(ctxcompiler.LayerTask)
	if !strings.Contains(l3, "newest") {
		t.Fatal("capped notes must keep the most recent entries")
	}
	if got := strings.Count(l3, "- note\n") + boolToInt(strings.Contains(l3, "- newest\n")); got != cfg.WorkingNotesMax {
		t.Fatalf("rendered %d notes, want cap %d", got, cfg.WorkingNotesMax)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestMethodSwitchOff(t *testing.T) {
	snap := testSnapshot()
	snap.ModelID = "qwen3-max"
	off := ctxcompiler.DefaultConfig()
	off.MethodPrompt = false
	off.FamilyAdapt = false
	off.WorkingNotesMax = 0
	c, err := ctxcompiler.Compile(off, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.LayerText(ctxcompiler.LayerCore), "working method:") {
		t.Fatal("MethodPrompt=false must drop the method block")
	}
	// Family adaptation off: the model id must not move L0.
	snap.ModelID = "gpt-6-luna"
	gpt, err := ctxcompiler.Compile(off, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if gpt.LayerHash(ctxcompiler.LayerCore) != c.LayerHash(ctxcompiler.LayerCore) {
		t.Fatal("with adaptation off, L0 must be identical across families")
	}
	// Adaptation on: families differ, same family is stable.
	on := ctxcompiler.DefaultConfig()
	snap.ModelID = "qwen3-max"
	qwen, err := ctxcompiler.Compile(on, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	snap.ModelID = "gpt-6-luna"
	gpt2, err := ctxcompiler.Compile(on, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if qwen.LayerHash(ctxcompiler.LayerCore) == gpt2.LayerHash(ctxcompiler.LayerCore) {
		t.Fatal("with adaptation on, L0 must differ across families")
	}
	snap.ModelID = ""
	unknown, err := ctxcompiler.Compile(on, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.LayerHash(ctxcompiler.LayerCore) != gpt2.LayerHash(ctxcompiler.LayerCore) {
		t.Fatal("empty model id must render the GPT default")
	}
}
