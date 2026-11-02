package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

func TestEvalSkillsHashTracksCatalog(t *testing.T) {
	// Section 23.2: the profile records the frozen skill catalog
	// hash, so any builtin skill change moves profile.json and the
	// ATIF profile hash (Appendix F minimum records).
	p := DefaultEvalProfile()
	if p.SkillsCatalogHash == "" || p.SkillsCatalogHash == "frozen-placeholder" {
		t.Fatalf("eval profile carries no real catalog hash: %q", p.SkillsCatalogHash)
	}
	ix := skills.NewEvalBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	if got := ix.CatalogHash(); got != p.SkillsCatalogHash {
		t.Fatalf("profile hash %q != embedded catalog hash %q", p.SkillsCatalogHash, got)
	}
	if again := DefaultEvalProfile().SkillsCatalogHash; again != p.SkillsCatalogHash {
		t.Fatal("catalog hash not stable across constructions")
	}
	if p.Hash() == "" {
		t.Fatal("profile hash empty")
	}
}

func TestStatsExact(t *testing.T) {
	if v, err := PassAtK(5, 5, 5); err != nil || v != 1 {
		t.Fatalf("pass@k all solve: %v %v", v, err)
	}
	if v, err := PassAtK(5, 0, 3); err != nil || v != 0 {
		t.Fatalf("pass@k none: %v %v", v, err)
	}
	// n=5 c=1 k=1 -> 1/5.
	if v, err := PassAtK(5, 1, 1); err != nil || !closeTo(v, 0.2) {
		t.Fatalf("pass@k single: %v %v", v, err)
	}
	// n=5 c=2 k=2 -> 1 - C(3,2)/C(5,2) = 1-3/10.
	if v, err := PassAtK(5, 2, 2); err != nil || !closeTo(v, 0.7) {
		t.Fatalf("pass@k: %v %v", v, err)
	}
	if v, err := PassPowK(4, 2, 2); err != nil || !closeTo(v, 0.25) {
		t.Fatalf("pass^k: %v %v", v, err)
	}
	xs := []float64{0, 0, 1, 1}
	ci, err := BootstrapCI(xs, 2000, 42)
	if err != nil {
		t.Fatal(err)
	}
	if ci.Mean != 0.5 {
		t.Fatalf("mean %v", ci.Mean)
	}
	if !(ci.Low <= ci.Mean && ci.Mean <= ci.High) {
		t.Fatalf("interval does not contain mean: %+v", ci)
	}
	// Deterministic seed: same inputs give identical intervals.
	ci2, _ := BootstrapCI(xs, 2000, 42)
	if ci != ci2 {
		t.Fatal("bootstrap not deterministic")
	}
	// Paired: clear win excludes 0.
	a := []float64{0, 0, 0, 0, 1, 0, 0, 0}
	b := []float64{1, 1, 1, 1, 1, 1, 1, 1}
	diff, err := PairedDiffCI(a, b, 5000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Low <= 0 {
		t.Fatalf("paired interval should exclude 0: %+v", diff)
	}
	if !AcceptImprovement(diff, 0.03) {
		t.Fatal("clear win should be accepted")
	}
	// Noisy tie is rejected.
	tie, _ := PairedDiffCI([]float64{1, 0, 1, 0}, []float64{0, 1, 0, 1}, 5000, 1)
	if AcceptImprovement(tie, 0.03) {
		t.Fatalf("tie should be rejected: %+v", tie)
	}
	if RequiredTrials(2) != MinTrials {
		t.Fatal("trial floor not applied")
	}
	n, err := PowerTrials(0.3, 0.1)
	if err != nil || n < MinTrials {
		t.Fatalf("power trials: %d %v", n, err)
	}
}

func TestReplayNoChangeAndDivergence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recording.jsonl")
	rec, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	req := ReplayRequest{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}}}
	resp := providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "hello"},
		Usage: providers.Usage{Input: 5, Output: 3}, Model: "m"}
	if err := rec.Record(req, resp); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	// Same code: zero provider calls, same result.
	p, err := LoadPlayer(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Replay(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Message.Content != "hello" || got.Usage.Input != 5 {
		t.Fatalf("replay mismatch: %+v", got)
	}
	if !p.Done() {
		t.Fatal("player should be done")
	}
	// Changed request: stop at first divergence.
	p2, _ := LoadPlayer(path)
	changed := ReplayRequest{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "changed prompt"}}}
	if _, err := p2.Replay(changed); err == nil {
		t.Fatal("expected divergence")
	} else if _, ok := err.(*Divergence); !ok {
		t.Fatalf("wrong error type: %T", err)
	}
}

func TestSuiteRegistry(t *testing.T) {
	reg := Registry()
	if len(reg) != 17 {
		t.Fatalf("registry has %d suites, want 17", len(reg))
	}
	seen := map[SuiteID]bool{}
	for _, s := range reg {
		if seen[s.ID] {
			t.Fatalf("duplicate suite %s", s.ID)
		}
		seen[s.ID] = true
		if len(s.Fixtures) == 0 {
			t.Fatalf("suite %s has no fixtures", s.ID)
		}
	}
	for _, id := range []SuiteID{SuiteAvail, SuiteCrash, SuiteCache, SuiteCompaction} {
		if _, err := ByID(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ByID("nope"); err == nil {
		t.Fatal("expected unknown-suite error")
	}
	// Deterministic setup: same contents hash equally.
	s, _ := ByID(SuiteCache)
	h1 := FixtureHash(s, map[string]string{"cache-workload": "abc"})
	h2 := FixtureHash(s, map[string]string{"cache-workload": "abc"})
	if h1 != h2 || h1 == "" {
		t.Fatal("fixture hash not deterministic")
	}
}

func TestProfileContract(t *testing.T) {
	p := DefaultEvalProfile()
	if p.Version != EvalProfileVersion {
		t.Fatal("profile version drift")
	}
	if !p.SingleModel {
		t.Fatal("eval must be single-model")
	}
	// Tool table of Section 23.2 after the item-41 trim: the core tools
	// that act in a single run stay, assistant tools absent, and the
	// four tools that cannot act here are gone (see evalTrimmedTools).
	for _, want := range []string{"exec", "search_tools", "call_tool", "search_skills", "load_skill"} {
		found := false
		for _, tool := range p.EnabledTools {
			if tool == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("core tool %s missing", want)
		}
	}
	for _, banned := range append([]string{"send_message", "ask_user", "email", "browser_navigate"}, TrimmedFromEvalTools()...) {
		for _, tool := range p.EnabledTools {
			if tool == banned {
				t.Fatalf("assistant tool %s exposed in eval", banned)
			}
		}
	}
	h1 := p.Hash()
	h2 := DefaultEvalProfile().Hash()
	if h1 == "" || h1 != h2 {
		t.Fatal("profile hash not stable")
	}
	// Data dir isolation: non-empty dir aborts without override.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckDataDir(dir, ""); err == nil {
		t.Fatal("expected non-empty dir abort")
	}
	if err := CheckDataDir(dir, "declared-deviation"); err != nil {
		t.Fatal(err)
	}
	if err := CheckDataDir(filepath.Join(dir, "missing"), ""); err != nil {
		t.Fatal(err)
	}
	// Network policy + blocked sources.
	if WebToolsVisible(true) {
		t.Fatal("web tools must hide under restricted network")
	}
	if !BlockedBenchmarkSource("https://github.com/harbor-framework/harbor") {
		t.Fatal("harbor repo should be blocked")
	}
	if !BlockedBenchmarkSource("https://tbench.ai/tasks/1") {
		t.Fatal("tbench should be blocked")
	}
	if BlockedBenchmarkSource("https://docs.python.org/3/library/os.html") {
		t.Fatal("docs should not be blocked")
	}
	// Single-model mode.
	if err := CheckSingleModel([]string{"m", "m"}, "m"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSingleModel([]string{"m", "other"}, "m"); err == nil {
		t.Fatal("expected single-model violation")
	}
}

func TestReportAggregation(t *testing.T) {
	dir := t.TempDir()
	trials := []TrialOutcome{
		{TrialID: "a1", TaskID: "task-a", Reward: 1, InputTokens: 100, OutputTokens: 50, CostUSD: 0.01},
		{TrialID: "a2", TaskID: "task-a", Reward: 0, FailCategory: "timeout"},
		{TrialID: "b1", TaskID: "task-b", Reward: 1, InputTokens: 200, OutputTokens: 100, CostUSD: 0.02},
		{TrialID: "b2", TaskID: "task-b", Reward: 1, InputTokens: 200, OutputTokens: 100, CostUSD: 0.02},
	}
	for i, tr := range trials {
		buf, _ := json.Marshal(tr)
		if err := os.WriteFile(filepath.Join(dir, string(rune('a'+i))+".json"), append(buf, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadTrials(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 4 {
		t.Fatalf("loaded %d", len(loaded))
	}
	rep, err := Aggregate(loaded, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tasks != 2 || rep.Trials != 4 {
		t.Fatalf("report counts: %+v", rep)
	}
	if rep.Success.Mean != 0.75 {
		t.Fatalf("success mean %v", rep.Success.Mean)
	}
	if rep.Failures["timeout"] != 1 {
		t.Fatalf("failures: %v", rep.Failures)
	}
	if rep.CostPerSuccess <= 0 || rep.TokensPerSuccess <= 0 {
		t.Fatal("per-success costs missing")
	}
	if HashTrials(loaded) == "" {
		t.Fatal("trial hash empty")
	}
	if err := WriteReport(filepath.Join(dir, "out"), rep); err != nil {
		t.Fatal(err)
	}
}

func TestSmokeScaffold(t *testing.T) {
	dir := t.TempDir()
	r := SmokeRunner{Dir: dir, Profile: DefaultEvalProfile()}
	results, err := r.Run(DefaultSmokeTasks())
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if !res.Passed {
			t.Fatalf("smoke %s failed: %s", res.Name, res.Detail)
		}
		if err := telemetry.ValidateFile(res.ATIFPath); err != nil {
			t.Fatalf("smoke ATIF invalid: %v", err)
		}
	}
}

func TestHarborAgentContext(t *testing.T) {
	dir := t.TempDir()
	metrics := map[string]any{
		"usage":    map[string]any{"input": 1000, "cache_read": 300, "cache_write": 100, "output": 200, "reasoning": 50},
		"cost_usd": 0.05, "model": "m",
	}
	buf, _ := json.Marshal(metrics)
	path := filepath.Join(dir, "metrics.json")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, err := FromMetricsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.InputTokens != 1000 || ctx.CacheRead != 300 || ctx.CacheWrite != 100 {
		t.Fatalf("context tokens: %+v", ctx)
	}
	if ctx.OutputTokens != 250 {
		t.Fatalf("output should include reasoning: %+v", ctx)
	}
	if MetricsPath("/logs/agent") != "/logs/agent/excalibur/metrics.json" {
		t.Fatal("metrics path")
	}
	if _, err := FromMetricsFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected missing-metrics error")
	}
}

func closeTo(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func TestSmokeTrialsAreReportable(t *testing.T) {
	dir := t.TempDir()
	r := SmokeRunner{Dir: dir, Profile: DefaultEvalProfile()}
	results, err := r.Run(DefaultSmokeTasks())
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if !res.Passed {
			t.Fatalf("smoke %s failed", res.Name)
		}
	}
	// Smoke trials feed `eval report` exactly like Harbor trials.
	trials, err := LoadTrials(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(trials) != len(results) {
		t.Fatalf("loaded %d trials, want %d", len(trials), len(results))
	}
	rep, err := Aggregate(trials, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Trials != len(results) || rep.Success.Mean != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if err := WriteReport(filepath.Join(dir, "report"), rep); err != nil {
		t.Fatal(err)
	}
}
