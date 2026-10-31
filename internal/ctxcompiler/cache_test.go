package ctxcompiler_test

import (
	"strings"
	"testing"
	"time"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// 11.6 table: each provider family gets its own wiring.
func TestCachePlansPerProvider(t *testing.T) {
	// Anthropic: four explicit marks, sliding fourth at end of L4,
	// TTL by pace, prewarm supported.
	human := ctxcompiler.Plan(ctxcompiler.FamilyAnthropic, "child", "scope/run_1", ctxcompiler.HumanPace)
	if human.MaxBreakpoints != 4 || len(human.Breakpoints) != 4 {
		t.Fatalf("anthropic needs 4 marks: %+v", human.Breakpoints)
	}
	if human.Breakpoints[3].After != ctxcompiler.LayerActivity {
		t.Fatal("fourth mark slides at the end of L4")
	}
	if human.TTL != time.Hour {
		t.Fatalf("human pace TTL = %v, want 1h", human.TTL)
	}
	if !human.Prewarm.Supported || !human.Prewarm.LeaderFirst {
		t.Fatal("anthropic needs leader-first prewarm")
	}
	machine := ctxcompiler.Plan(ctxcompiler.FamilyAnthropic, "child", "scope/run_1", ctxcompiler.MachinePace)
	if machine.TTL != 5*time.Minute {
		t.Fatalf("machine pace TTL = %v, want 5min", machine.TTL)
	}

	// OpenAI: automatic cache from 1024 tokens, stable key per
	// role/scope, fixed reasoning, allowlist restriction.
	openai := ctxcompiler.Plan(ctxcompiler.FamilyOpenAI, "child", "scope/run_1", ctxcompiler.MachinePace)
	if openai.MinTokens != 1024 || !openai.ReasoningFixed || !openai.RestrictViaAllowlist {
		t.Fatalf("bad openai plan: %+v", openai)
	}
	if openai.PromptCacheKey == "" || openai.PromptCacheKey != ctxcompiler.Plan(ctxcompiler.FamilyOpenAI, "child", "scope/run_1", ctxcompiler.MachinePace).PromptCacheKey {
		t.Fatal("prompt cache key must be stable per role/scope")
	}
	if other := ctxcompiler.Plan(ctxcompiler.FamilyOpenAI, "master", "scope/run_1", ctxcompiler.MachinePace); other.PromptCacheKey == openai.PromptCacheKey {
		t.Fatal("different roles must not share a cache key")
	}

	// Gemini: implicit cache with the 2050-token gotcha.
	gemini := ctxcompiler.Plan(ctxcompiler.FamilyGemini, "child", "", ctxcompiler.MachinePace)
	found := false
	for _, n := range gemini.Notes {
		if strings.Contains(n, "2050") {
			found = true
		}
	}
	if !found {
		t.Fatalf("gemini plan must warn about the 2050-token rule: %v", gemini.Notes)
	}

	// Qwen supports explicit marks; economic and local families use
	// the stable prefix only.
	qwen := ctxcompiler.Plan(ctxcompiler.FamilyQwen, "child", "", ctxcompiler.MachinePace)
	if len(qwen.Breakpoints) == 0 {
		t.Fatal("qwen supports explicit marks")
	}
	for _, f := range []ctxcompiler.ProviderFamily{
		ctxcompiler.FamilyDeepSeek, ctxcompiler.FamilyGLM,
		ctxcompiler.FamilyKimi, ctxcompiler.FamilyLocal, ctxcompiler.FamilyGeneric,
	} {
		p := ctxcompiler.Plan(f, "child", "", ctxcompiler.MachinePace)
		if len(p.Breakpoints) != 0 {
			t.Fatalf("%s must rely on the stable prefix, got %+v", f, p.Breakpoints)
		}
	}
}

func TestClassifyProvider(t *testing.T) {
	cases := map[string]ctxcompiler.ProviderFamily{
		"anthropic":  ctxcompiler.FamilyAnthropic,
		"openai":     ctxcompiler.FamilyOpenAI,
		"gemini-pro": ctxcompiler.FamilyGemini,
		"deepseek":   ctxcompiler.FamilyDeepSeek,
		"qwen-max":   ctxcompiler.FamilyQwen,
		"glm-4":      ctxcompiler.FamilyGLM,
		"kimi-k2":    ctxcompiler.FamilyKimi,
		"openrouter": ctxcompiler.FamilyOpenRouter,
		"ollama":     ctxcompiler.FamilyLocal,
		"vllm":       ctxcompiler.FamilyLocal,
		"something":  ctxcompiler.FamilyGeneric,
	}
	for name, want := range cases {
		if got := ctxcompiler.ClassifyProvider(name); got != want {
			t.Fatalf("ClassifyProvider(%q) = %s, want %s", name, got, want)
		}
	}
}

// 11.8 row 7: siblings of the same role prewarm leader-first on
// Anthropic only.
func TestPrewarm(t *testing.T) {
	if !ctxcompiler.ShouldPrewarm(ctxcompiler.FamilyAnthropic, 3, true) {
		t.Fatal("anthropic siblings with one prefix must prewarm")
	}
	if ctxcompiler.ShouldPrewarm(ctxcompiler.FamilyAnthropic, 1, true) {
		t.Fatal("a lone request needs no prewarm")
	}
	if ctxcompiler.ShouldPrewarm(ctxcompiler.FamilyAnthropic, 3, false) {
		t.Fatal("divergent prefixes cannot share a cache entry")
	}
	if ctxcompiler.ShouldPrewarm(ctxcompiler.FamilyOpenAI, 3, true) {
		t.Fatal("automatic caches need no prewarm")
	}
}

// 11.8 row 2 (watchdog half): L0/L1 stable across a long session,
// drift fires only on real prefix changes.
func TestDriftDetector(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	d := ctxcompiler.NewDriftDetector()
	snap := testSnapshot()
	prev, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if drifts := d.Observe("run_9", prev.Hashes); len(drifts) != 0 {
		t.Fatalf("first call never drifts: %v", drifts)
	}
	// Twenty calls with task deltas: zero drift.
	for i := 0; i < 20; i++ {
		ctxcompiler.AppendDelta(&snap, int64(10+i), i, "R9 progress")
		c, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
		if err != nil {
			t.Fatal(err)
		}
		if drifts := d.Observe("run_9", c.Hashes); len(drifts) != 0 {
			t.Fatalf("call %d drifted on deltas: %v", i, drifts)
		}
		if got := ctxcompiler.TheoreticalStableLayers(prev.Hashes, c.Hashes); got < 3 {
			t.Fatalf("call %d: theoretical prefix = %d layers, want >= 3", i, got)
		}
		prev = c
	}
	// A harness defect changing L0 fires with the layer attached.
	broken := testSnapshot()
	broken.CoreTools = append(broken.CoreTools, ctxcompiler.CoreTool{Name: "zzz_new", Description: "late tool"})
	c, err := ctxcompiler.Compile(cfg, broken, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	drifts := d.Observe("run_9", c.Hashes)
	if len(drifts) != 1 || drifts[0].Layer != ctxcompiler.LayerCore {
		t.Fatalf("L0 change must fire one L0 drift: %v", drifts)
	}
	// Theoretical prefix drops to zero on an L0 break.
	if got := ctxcompiler.TheoreticalStableLayers(prev.Hashes, c.Hashes); got != 0 {
		t.Fatalf("broken prefix must match 0 layers, got %d", got)
	}
	d.Forget("run_9")
	if drifts := d.Observe("run_9", c.Hashes); len(drifts) != 0 {
		t.Fatalf("forgotten run restarts clean: %v", drifts)
	}
}

// The cache suite math: hit rate from provider usage.
func TestCacheStats(t *testing.T) {
	var s ctxcompiler.CacheStats
	s.Add(providers.Usage{Input: 10000, CacheRead: 8000, CacheWrite: 2000, Output: 500})
	s.Add(providers.Usage{Input: 10000, CacheRead: 9000, CacheWrite: 1000, Output: 500})
	if s.Requests != 2 || s.InputTokens != 20000 || s.CachedTokens != 17000 {
		t.Fatalf("bad accumulation: %+v", s)
	}
	if r := s.HitRate(); r != 0.85 {
		t.Fatalf("hit rate = %v, want 0.85", r)
	}
	if r := (ctxcompiler.CacheStats{}).HitRate(); r != 0 {
		t.Fatal("empty stats must report 0")
	}
}
