package ctxcompiler_test

import (
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

// Model limits table (owner decision 0051): the reference row comes
// from models.dev, unknown models fall back to the conservative
// configured default, and per-model config overrides win.
func TestModelLimitsTable(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	got, ok := ctxcompiler.KnownLimits("gpt-6-luna")
	if !ok {
		t.Fatal("reference model must be known")
	}
	if got.ContextWindow != 1050000 || got.MaxInput != 922000 || got.MaxOutput != 128000 {
		t.Fatalf("reference limits = %+v, want 1050000/922000/128000", got)
	}
	// Case-insensitive lookup.
	if _, ok := ctxcompiler.KnownLimits("GPT-6-Luna"); !ok {
		t.Fatal("lookup must be case-insensitive")
	}
	if _, ok := ctxcompiler.KnownLimits("unknown-model-xyz"); ok {
		t.Fatal("unknown model must not be known")
	}
	// Resolved limits: reference row for the reference model.
	lim := ctxcompiler.LimitsForModel("gpt-6-luna", cfg)
	if lim.MaxInput != 922000 {
		t.Fatalf("reference max input = %d, want 922000", lim.MaxInput)
	}
	// Unknown models fall back to the conservative configured default.
	lim = ctxcompiler.LimitsForModel("some-future-model", cfg)
	if lim.MaxInput != cfg.DefaultMaxInput || lim.ContextWindow != cfg.DefaultContextWindow {
		t.Fatalf("unknown fallback = %+v, want defaults %+v", lim, cfg)
	}
	// Empty model id is unknown: conservative default.
	lim = ctxcompiler.LimitsForModel("", cfg)
	if lim.MaxInput != cfg.DefaultMaxInput {
		t.Fatalf("empty model fallback = %+v", lim)
	}
	// Per-model config override wins over the shipped table.
	cfg.ModelLimits = map[string]ctxcompiler.ModelLimits{
		"gpt-6-luna": {ContextWindow: 500000, MaxInput: 400000, MaxOutput: 64000},
	}
	lim = ctxcompiler.LimitsForModel("GPT-6-LUNA", cfg)
	if lim.MaxInput != 400000 || lim.ContextWindow != 500000 {
		t.Fatalf("config override must win: %+v", lim)
	}
}

// Threshold per model: maxInput - (reserve + safety), optionally
// capped. The request therefore never exceeds the model's max input.
func TestThresholdPerModel(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	// Reference: 922000 - 16000 (default reserve) - 92200 (10%) = 813800.
	if got := ctxcompiler.ThresholdForModel("gpt-6-luna", 0, cfg, 0); got != 813800 {
		t.Fatalf("reference threshold = %d, want 813800", got)
	}
	// Explicit reserve overrides the default.
	if got := ctxcompiler.ThresholdForModel("gpt-6-luna", 8192, cfg, 0); got != 821608 {
		t.Fatalf("explicit reserve threshold = %d, want 821608", got)
	}
	// Unknown model uses the conservative default max input:
	// 128000 - 16000 - 12800 = 99200.
	if got := ctxcompiler.ThresholdForModel("future-model", 0, cfg, 0); got != 99200 {
		t.Fatalf("conservative threshold = %d, want 99200", got)
	}
	// Optional cap lowers the trigger; zero means model maximum.
	cfg.CompactionCeilingTokens = 500000
	if got := ctxcompiler.ThresholdForModel("gpt-6-luna", 0, cfg, 0); got != 500000 {
		t.Fatalf("capped threshold = %d, want 500000", got)
	}
	cfg.CompactionCeilingTokens = 0
	// EXCALIBUR_CONTEXT_CAP override lowers the trigger.
	t.Setenv("EXCALIBUR_CONTEXT_CAP", "600000")
	if got := ctxcompiler.ThresholdForModel("gpt-6-luna", 0, cfg, 0); got != 600000 {
		t.Fatalf("env cap threshold = %d, want 600000", got)
	}
	t.Setenv("EXCALIBUR_CONTEXT_CAP", "")
	t.Setenv("EXCALIBUR_CONTEXT_CAP", "not-a-number")
	if got := ctxcompiler.ThresholdForModel("gpt-6-luna", 0, cfg, 0); got != 813800 {
		t.Fatalf("bad env cap must be ignored, got %d", got)
	}
}

// Switches default ON so the new mechanisms can be measured on and
// off (agent-quality ablation).
func TestContextSwitchesDefaultOn(t *testing.T) {
	t.Setenv("EXCALIBUR_CONTEXT_FULL_WINDOW", "")
	if !ctxcompiler.FullWindowEnabled(nil) {
		t.Fatal("full window must default ON")
	}
	t.Setenv("EXCALIBUR_CONTEXT_FULL_WINDOW", "0")
	if ctxcompiler.FullWindowEnabled(nil) {
		t.Fatal("FULL_WINDOW=0 must disable")
	}
	t.Setenv("EXCALIBUR_CONTEXT_MASK", "off")
	if ctxcompiler.MaskEnabled(nil) {
		t.Fatal("MASK=off must disable")
	}
	t.Setenv("EXCALIBUR_CONTEXT_COMPACT", "0")
	if ctxcompiler.CompactEnabled(nil) {
		t.Fatal("COMPACT=0 must disable")
	}
}
