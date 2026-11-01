package ctxcompiler_test

import (
	"math"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// 11.8 row 8: estimate vs usage stays within 10% after calibration.
func TestEstimateCalibratesWithinMargin(t *testing.T) {
	est := ctxcompiler.NewEstimator(4.0)
	const model = "test-model"
	// Simulate a model whose true factor is 6 chars/token: every
	// response reports usage, the factor converges.
	for i := 0; i < 30; i++ {
		text := strings.Repeat("abcdef", 1000) // 6000 chars
		actual := int64(1000)                  // true factor 6.0
		est.Observe(model, len(text), actual)
	}
	factor, samples := est.Factor(model)
	if samples != 30 {
		t.Fatalf("samples = %d, want 30", samples)
	}
	if math.Abs(factor-6.0) > 0.3 {
		t.Fatalf("calibrated factor = %v, want ~6.0", factor)
	}
	text := strings.Repeat("abcdef", 500)
	estimated := est.EstimateForModel(model, text)
	actual := float64(len(text)) / 6.0
	if math.Abs(float64(estimated)-actual)/actual > 0.10 {
		t.Fatalf("estimate %d vs usage %.0f exceeds the 10%% band (factor %v)",
			estimated, actual, factor)
	}
	// Unknown models keep the seed factor.
	if f, n := est.Factor("never-seen"); f != 4.0 || n != 0 {
		t.Fatalf("unseen model factor = %v (%d samples), want 4.0 (0)", f, n)
	}
	// Non-positive samples never corrupt the factor.
	est.Observe(model, 0, 0)
	est.Observe(model, 100, 0)
	est.Observe("", 100, 10)
	if f, _ := est.Factor(model); math.Abs(f-factor) > 1e-9 {
		t.Fatal("degenerate samples must not move the factor")
	}
}

// Threshold comparisons apply the 10% safety margin.
func TestMarginMath(t *testing.T) {
	if got := ctxcompiler.WithMargin(1000, 0.10); got != 1100 {
		t.Fatalf("WithMargin = %d, want 1100", got)
	}
	if got := ctxcompiler.WithMargin(0, 0.10); got != 0 {
		t.Fatalf("WithMargin(0) = %d, want 0", got)
	}
}

// The local tokenizer seam: an injected exact counter wins, else the
// estimator applies.
func TestCounterForModel(t *testing.T) {
	est := ctxcompiler.NewEstimator(4.0)
	if got := ctxcompiler.CounterForModel("m", nil, est); got != ctxcompiler.Counter(est) {
		t.Fatal("nil tokenizer must fall back to the estimator")
	}
	if got := ctxcompiler.CounterForModel("m", nil, nil); got == nil || got.Count("abcd") != 1 {
		t.Fatal("nil everything must still count with defaults")
	}
	exact := &stubTokenizer{model: "m", per: 2}
	if got := ctxcompiler.CounterForModel("m", exact, est); got != ctxcompiler.Counter(exact) {
		t.Fatal("matching tokenizer must win over the estimator")
	}
	if got := ctxcompiler.CounterForModel("other", exact, est); got != ctxcompiler.Counter(est) {
		t.Fatal("tokenizer for another model must not apply")
	}
}

type stubTokenizer struct {
	model string
	per   int
}

func (s *stubTokenizer) Count(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + s.per - 1) / s.per
}

func (s *stubTokenizer) Model() string { return s.model }

// Message costing covers content, tool arguments and results.
func TestCountMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: providers.RoleUser, Content: "abcdefgh"},
		{Role: providers.RoleAssistant, Content: "ijkl",
			ToolCalls: []providers.ToolCall{{ID: "t", Name: "mnop", Arguments: []byte(`{}`)}}},
		{Role: providers.RoleTool, Content: "qrst",
			ToolResult: &providers.ToolResult{CallID: "t", Name: "mnop", Output: "uvwx"}},
	}
	got := ctxcompiler.CountMessages(msgs, fixedCounter{per: 4})
	// 8/4 + (4/4 + 4/4 + 2/4->1) + (4/4 + 4/4) = 2 + 3 + 2 = 7.
	if got != 7 {
		t.Fatalf("CountMessages = %d, want 7", got)
	}
}

// The compiler defaults mirror the shared config: one Appendix B,
// two spellings of the same numbers.
func TestDefaultsMirrorSharedConfig(t *testing.T) {
	mine := ctxcompiler.DefaultConfig()
	theirs := config.Default().Context
	checks := map[string][2]int{
		"ceiling":        {mine.CompactionCeilingTokens, theirs.CompactionCeilingTokens},
		"default window": {mine.DefaultContextWindow, theirs.DefaultContextWindow},
		"default maxin":  {mine.DefaultMaxInput, theirs.DefaultMaxInput},
		"default maxout": {mine.DefaultMaxOutput, theirs.DefaultMaxOutput},
		"reserve":        {mine.CompactionReserveTokens, theirs.CompactionReserveTokens},
		"post literal":   {mine.PostCompactionLiteralTokens, theirs.PostCompactionLiteralTokens},
		"literal obs":    {mine.LiteralObservations, theirs.LiteralObservations},
		"mask trigger":   {mine.MaskTriggerTokens, theirs.MaskTriggerTokens},
		"mask target":    {mine.MaskTargetTokens, theirs.MaskTargetTokens},
		"master history": {mine.MasterHistoryTokens, theirs.MasterHistoryTokens},
		"master min":     {mine.MasterTargetMin, theirs.MasterTargetMin},
		"master max":     {mine.MasterTargetMax, theirs.MasterTargetMax},
		"child max":      {mine.ChildTargetMax, theirs.ChildTargetMax},
		"parent max":     {mine.ParentTargetMax, theirs.ParentTargetMax},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] {
			t.Fatalf("%s: ctxcompiler %d != config %d", name, pair[0], pair[1])
		}
	}
	if err := mine.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	bad := mine
	bad.MaskTargetTokens = bad.MaskTriggerTokens
	if err := bad.Validate(); err == nil {
		t.Fatal("target >= trigger must not validate")
	}
}
