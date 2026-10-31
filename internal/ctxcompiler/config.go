package ctxcompiler

import "fmt"

// Config carries the numeric policy of Section 11 and Appendix B.
// It mirrors internal/config ContextConfig without importing it, so
// the compiler only depends on internal/types, provider request
// types and the small source interfaces of this package.
type Config struct {
	// CompactionCeilingTokens is the configured ceiling (Appendix B:
	// 500 000). The effective threshold is
	// min(ceiling, 0.85*window-reserve); see compaction.go.
	CompactionCeilingTokens int
	// PostCompactionLiteralTokens keeps this literal tail after a
	// compaction (Appendix B: 20K).
	PostCompactionLiteralTokens int
	// LiteralObservations pins the newest observations (11.3: 10).
	LiteralObservations int
	// MaskTriggerTokens starts a masking batch above this unmasked
	// total (11.3: 24K).
	MaskTriggerTokens int
	// MaskTargetTokens compacts masking toward this total (11.3:
	// 12K).
	MaskTargetTokens int
	// MasterHistoryTokens caps literal master history (11.4: 12K,
	// summarized past 24K).
	MasterHistoryTokens int
	// MasterHistoryRecalcTokens recalculates the conversation summary
	// past this literal total (11.4: 24K).
	MasterHistoryRecalcTokens int
	// MasterTargetMin and MasterTargetMax bound the master working
	// range (11.2: 8-24K).
	MasterTargetMin int
	MasterTargetMax int
	// ChildTargetMax bounds a child run in steady state (11.2: 32K).
	ChildTargetMax int
	// ParentTargetMax bounds a coordinator parent (11.2: 48K).
	ParentTargetMax int
	// SuggestedSkillsMax caps suggested skills per task (11.1: 3).
	SuggestedSkillsMax int
	// SkillCatalogFraction caps the L1 catalog share of the window;
	// SkillCatalogMax caps it absolutely (12.3: 2% or 4K tokens).
	SkillCatalogFraction float64
	SkillCatalogMax      int
	// EstimateMargin is the safety margin applied to token estimates
	// before comparing them with the 11.3/11.4 thresholds (11.7:
	// 10%).
	EstimateMargin float64
}

// DefaultConfig returns the Appendix B initial values.
func DefaultConfig() Config {
	return Config{
		CompactionCeilingTokens:     500000,
		PostCompactionLiteralTokens: 20000,
		LiteralObservations:         10,
		MaskTriggerTokens:           24000,
		MaskTargetTokens:            12000,
		MasterHistoryTokens:         12000,
		MasterHistoryRecalcTokens:   24000,
		MasterTargetMin:             8000,
		MasterTargetMax:             24000,
		ChildTargetMax:              32000,
		ParentTargetMax:             48000,
		SuggestedSkillsMax:          3,
		SkillCatalogFraction:        0.02,
		SkillCatalogMax:             4000,
		EstimateMargin:              0.10,
	}
}

// Validate checks the configuration ranges.
func (c Config) Validate() error {
	checks := []struct {
		ok   bool
		what string
	}{
		{c.CompactionCeilingTokens > 0, "compaction_ceiling_tokens"},
		{c.PostCompactionLiteralTokens > 0, "post_compaction_literal_tokens"},
		{c.LiteralObservations > 0, "literal_observations"},
		{c.MaskTriggerTokens > c.MaskTargetTokens, "mask_trigger_gt_target"},
		{c.MasterHistoryTokens > 0, "master_history_tokens"},
		{c.MasterHistoryRecalcTokens > c.MasterHistoryTokens, "master_history_recalc_gt_literal"},
		{c.MasterTargetMin > 0 && c.MasterTargetMin <= c.MasterTargetMax, "master_target_range"},
		{c.ChildTargetMax > 0, "child_target_max"},
		{c.ParentTargetMax > 0, "parent_target_max"},
		{c.SuggestedSkillsMax > 0, "suggested_skills_max"},
		{c.SkillCatalogFraction > 0 && c.SkillCatalogFraction < 1, "skill_catalog_fraction"},
		{c.SkillCatalogMax > 0, "skill_catalog_max"},
		{c.EstimateMargin >= 0 && c.EstimateMargin < 1, "estimate_margin"},
	}
	for _, c := range checks {
		if !c.ok {
			return fmt.Errorf("ctxcompiler: invalid value for %s", c.what)
		}
	}
	return nil
}
