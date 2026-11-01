package config

import "testing"

// EXCALIBUR_CONTEXT_CAP is the documented single-segment alias for
// the optional compaction cap (owner decision 0051). Zero/unset
// means the model maximum.
func TestContextCapAlias(t *testing.T) {
	cfg, err := Load(LoadOptions{SkipFiles: true, NoEnv: false, Env: []string{"EXCALIBUR_CONTEXT_CAP=600000"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context.CompactionCeilingTokens != 600000 {
		t.Fatalf("cap alias = %d, want 600000", cfg.Context.CompactionCeilingTokens)
	}
	// Unset means uncapped.
	cfg, err = Load(LoadOptions{SkipFiles: true, NoEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context.CompactionCeilingTokens != 0 {
		t.Fatalf("default cap = %d, want 0 (model maximum)", cfg.Context.CompactionCeilingTokens)
	}
	// Bad values are ignored, not fatal.
	cfg, err = Load(LoadOptions{SkipFiles: true, NoEnv: false, Env: []string{"EXCALIBUR_CONTEXT_CAP=nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context.CompactionCeilingTokens != 0 {
		t.Fatalf("bad cap must stay uncapped, got %d", cfg.Context.CompactionCeilingTokens)
	}
}
