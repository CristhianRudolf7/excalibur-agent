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
		{"muse-spark-1.3", ctxcompiler.PromptMuse},
		{"MUSE-SPARK-1.3", ctxcompiler.PromptMuse},
		{"meta-models/Muse-Spark-Open-816B-A42B", ctxcompiler.PromptMuse},
		{"meta-models/muse-glimmer-30b", ctxcompiler.PromptMuse},
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
		ctxcompiler.PromptMuse,
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

// The Muse variant teaches the Meta Model API discipline: exact
// JSON arguments, every parallel call answered in order, no invented
// results, and finish only through the finish tool.
func TestMuseNoteContent(t *testing.T) {
	note := ctxcompiler.FamilyNote(ctxcompiler.PromptMuse)
	for _, want := range []string{
		"exact JSON", "call order", "never invent results",
		"Finish only via the finish tool",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("muse note missing %q: %q", want, note)
		}
	}
	if strings.Contains(note, "Reasoning strength:") {
		t.Error("muse note must not set a reasoning strength line (it would override the request effort)")
	}
}

// The GPT reference bytes never move: the default note is the exact
// tuned line and the adapted GPT block is byte-stable.
func TestGPTBytesStable(t *testing.T) {
	const wantNote = "Style: batch independent read-only calls in one turn with exact JSON arguments. After acting, verify with an executable check before finishing."
	if got := ctxcompiler.FamilyNote(ctxcompiler.PromptGPT); got != wantNote {
		t.Errorf("gpt note changed:\n got %q\nwant %q", got, wantNote)
	}
	gptOn := ctxcompiler.MethodBlock(ctxcompiler.PromptGPT, true)
	if !strings.Contains(gptOn, wantNote) {
		t.Error("adapted GPT block must carry the reference note")
	}
	if strings.Contains(gptOn, "finish tool") {
		t.Error("adapted GPT block must not carry Muse wording")
	}
}

func TestAdaptToolDescription(t *testing.T) {
	exec, finish, other := "run a command", "end the run", "read a file"
	// Every non-Muse family is the identity, including unknown (GPT
	// default): GPT bytes never move.
	for _, f := range []ctxcompiler.PromptFamily{
		ctxcompiler.PromptGPT, ctxcompiler.PromptClaude,
		ctxcompiler.PromptGemini, ctxcompiler.PromptDeepSeek,
		ctxcompiler.PromptGrok, ctxcompiler.PromptQwen,
		ctxcompiler.PromptUnknown,
	} {
		for _, tc := range [][2]string{{"exec", exec}, {"finish", finish}, {"read_file", other}} {
			if got := ctxcompiler.AdaptToolDescription(f, tc[0], tc[1]); got != tc[1] {
				t.Errorf("AdaptToolDescription(%s, %s) changed %q to %q", f, tc[0], tc[1], got)
			}
		}
	}
	// Muse adapts only exec and finish; every other tool is identical.
	if got := ctxcompiler.AdaptToolDescription(ctxcompiler.PromptMuse, "read_file", other); got != other {
		t.Errorf("muse must leave other tools unchanged, got %q", got)
	}
	mexec := ctxcompiler.AdaptToolDescription(ctxcompiler.PromptMuse, "exec", exec)
	if mexec == exec || !strings.Contains(mexec, "exact JSON") || !strings.Contains(mexec, "call order") {
		t.Errorf("muse exec description missing API discipline: %q", mexec)
	}
	mfinish := ctxcompiler.AdaptToolDescription(ctxcompiler.PromptMuse, "finish", finish)
	if mfinish == finish || !strings.Contains(mfinish, "ONLY through this tool") {
		t.Errorf("muse finish description missing finish-only rule: %q", mfinish)
	}
}

// Adapted L0 tool lines follow the same adapt-on/adapt-off contract
// as the method block: Muse differs only when adaptation is on.
func TestRenderCoreToolAdapt(t *testing.T) {
	mkSnap := func(model string) ctxcompiler.Snapshot {
		snap := testSnapshot()
		snap.ModelID = model
		return snap
	}
	on := ctxcompiler.DefaultConfig()
	gpt, err := ctxcompiler.Compile(on, mkSnap("gpt-6-luna"), fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	muse, err := ctxcompiler.Compile(on, mkSnap("muse-spark-1.3"), fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(muse.LayerText(ctxcompiler.LayerCore), "call order") {
		t.Error("adapted Muse L0 must carry the exec discipline on the tool line")
	}
	if strings.Contains(gpt.LayerText(ctxcompiler.LayerCore), "call order") {
		t.Error("GPT L0 must not carry Muse tool wording")
	}
	off := ctxcompiler.DefaultConfig()
	off.FamilyAdapt = false
	gptOff, err := ctxcompiler.Compile(off, mkSnap("gpt-6-luna"), fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	museOff, err := ctxcompiler.Compile(off, mkSnap("muse-spark-1.3"), fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if gptOff.LayerHash(ctxcompiler.LayerCore) != museOff.LayerHash(ctxcompiler.LayerCore) {
		t.Error("with adaptation off, L0 must be identical across GPT and Muse")
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
