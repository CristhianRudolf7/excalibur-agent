package ctxcompiler

import (
	"os"
	"strconv"
	"strings"
)

// Model limits table for the full-window policy (Section 11.4,
// owner decision 0051): the compaction trigger is derived from the
// model's own max input, not from the old fixed 500 000-token
// ceiling. The ceiling survives only as an optional cap
// (EXCALIBUR_CONTEXT_CAP, default unset = model maximum).
//
// Sources: gpt-6-luna row from models.dev (context 1 050 000,
// max input 922 000, max output 128 000). Unknown models fall back
// to the conservative configured default so the harness never
// overshoots an unseen window.

// ModelLimits carries one model's window: total context, bindable
// input and completion headroom.
type ModelLimits struct {
	// ContextWindow is the advertised total window.
	ContextWindow int `json:"context_window" toml:"context_window"`
	// MaxInput is the binding request cap the threshold derives from.
	MaxInput int `json:"max_input" toml:"max_input"`
	// MaxOutput caps one completion.
	MaxOutput int `json:"max_output" toml:"max_output"`
}

// ReferenceModel is the harness reference model for cheap runs.
const ReferenceModel = "gpt-6-luna"

// ReferenceLimits are the models.dev values for the reference model.
var ReferenceLimits = ModelLimits{
	ContextWindow: 1050000,
	MaxInput:      922000,
	MaxOutput:     128000,
}

// normalizeModel lowercases and trims a model id for table lookup.
func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// KnownLimits holds the pinned per-model rows. Operators override
// through Config.ModelLimits; the table here is the shipped fallback.
func KnownLimits(model string) (ModelLimits, bool) {
	switch normalizeModel(model) {
	case "gpt-6-luna":
		return ReferenceLimits, true
	default:
		return ModelLimits{}, false
	}
}

// ConservativeLimits returns the configured fallback for unknown
// models: never larger than any shipped row.
func ConservativeLimits(cfg Config) ModelLimits {
	return ModelLimits{
		ContextWindow: cfg.DefaultContextWindow,
		MaxInput:      cfg.DefaultMaxInput,
		MaxOutput:     cfg.DefaultMaxOutput,
	}
}

// LimitsForModel resolves the effective limits: explicit per-model
// config first, then the shipped known table, then the conservative
// configured default. An empty model id selects the conservative
// default.
func LimitsForModel(model string, cfg Config) ModelLimits {
	key := normalizeModel(model)
	if key != "" && cfg.ModelLimits != nil {
		if lim, ok := cfg.ModelLimits[key]; ok && lim.MaxInput > 0 {
			return lim
		}
	}
	if lim, ok := KnownLimits(key); ok {
		return lim
	}
	return ConservativeLimits(cfg)
}

// EffectiveMaxInput is the binding input cap for model.
func EffectiveMaxInput(model string, cfg Config) int {
	return LimitsForModel(model, cfg).MaxInput
}

// EffectiveContextWindow is the advertised window for model.
func EffectiveContextWindow(model string, cfg Config) int {
	return LimitsForModel(model, cfg).ContextWindow
}

// SafetyMargin is the headroom subtracted after the tool-result
// reserve: EstimateMargin share of max input (11.7, default 10%).
func SafetyMargin(maxInput int, cfg Config) int {
	if maxInput <= 0 {
		return 0
	}
	return int(cfg.EstimateMargin * float64(maxInput))
}

// DefaultReserve returns the configured tool-result reserve, used
// when the caller passes a non-positive reserve.
func DefaultReserve(cfg Config) int {
	if cfg.CompactionReserveTokens > 0 {
		return cfg.CompactionReserveTokens
	}
	return 16000
}

// EffectiveCap resolves the optional ceiling: the smallest positive
// value among the configured ceiling, the per-model ceiling and the
// EXCALIBUR_CONTEXT_CAP environment override. Zero means uncapped
// (model maximum).
func EffectiveCap(cfg Config, modelCeiling int) int {
	cap := cfg.CompactionCeilingTokens
	if modelCeiling > 0 && (cap <= 0 || modelCeiling < cap) {
		cap = modelCeiling
	}
	if env := ContextCapFromEnv(nil); env > 0 && (cap <= 0 || env < cap) {
		cap = env
	}
	return cap
}

// ThresholdForModel returns the compaction trigger for model:
// maxInput - (reserve + safety), optionally capped. The request
// therefore never exceeds the model's max input.
func ThresholdForModel(model string, reserveTokens int, cfg Config, modelCeiling int) int {
	lim := LimitsForModel(model, cfg)
	if reserveTokens <= 0 {
		reserveTokens = DefaultReserve(cfg)
	}
	return thresholdFromMaxInput(lim.MaxInput, reserveTokens, cfg, EffectiveCap(cfg, modelCeiling))
}

// thresholdFromMaxInput is the shared trigger math, also used by
// CompactionThresholdForModel. Degenerate inputs clamp to 1024.
func thresholdFromMaxInput(maxInput, reserveTokens int, cfg Config, cap int) int {
	safety := SafetyMargin(maxInput, cfg)
	threshold := maxInput - reserveTokens - safety
	if threshold < 1024 {
		threshold = 1024
	}
	if cap > 0 && threshold > cap {
		return cap
	}
	return threshold
}

// LegacyThreshold restores the pre-full-window trigger
// (min(500 000, 0.85*window - reserve)) for the
// EXCALIBUR_CONTEXT_FULL_WINDOW=0 ablation.
func LegacyThreshold(windowTokens, reserveTokens int, cfg Config) int {
	ceiling := cfg.CompactionCeilingTokens
	if ceiling <= 0 {
		ceiling = 500000
	}
	frac := int(0.85*float64(windowTokens)) - reserveTokens
	if frac < 1024 {
		frac = 1024
	}
	if frac > ceiling {
		return ceiling
	}
	return frac
}

// FullWindowEnabled reports whether the full-window trigger applies
// (default ON). EXCALIBUR_CONTEXT_FULL_WINDOW=0 restores the legacy
// 500 000 ceiling behaviour for ablations.
func FullWindowEnabled(getenv func(string) string) bool {
	return envFlagDefaultOn(getenv, "EXCALIBUR_CONTEXT_FULL_WINDOW")
}

// MaskEnabled reports whether embedded observation masking applies
// (default ON). EXCALIBUR_CONTEXT_MASK=0 disables it.
func MaskEnabled(getenv func(string) string) bool {
	return envFlagDefaultOn(getenv, "EXCALIBUR_CONTEXT_MASK")
}

// CompactEnabled reports whether embedded compaction applies
// (default ON). EXCALIBUR_CONTEXT_COMPACT=0 disables it.
func CompactEnabled(getenv func(string) string) bool {
	return envFlagDefaultOn(getenv, "EXCALIBUR_CONTEXT_COMPACT")
}

// ContextCapFromEnv reads the optional EXCALIBUR_CONTEXT_CAP token
// cap (default unset = model maximum). Non-positive or unparsable
// values mean uncapped.
func ContextCapFromEnv(getenv func(string) string) int {
	get := getenv
	if get == nil {
		get = os.Getenv
	}
	raw := strings.TrimSpace(get("EXCALIBUR_CONTEXT_CAP"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// envFlagDefaultOn parses a default-ON switch: unset or
// 1/true/yes/on means on; 0/false/no/off means off.
func envFlagDefaultOn(getenv func(string) string, key string) bool {
	get := getenv
	if get == nil {
		get = os.Getenv
	}
	raw := strings.ToLower(strings.TrimSpace(get(key)))
	if raw == "" {
		return true
	}
	switch raw {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return true
	}
}
