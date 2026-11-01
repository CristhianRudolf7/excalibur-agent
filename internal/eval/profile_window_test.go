package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

// Eval profile hash and records report the effective window (owner
// decision 0051): the frozen default carries the reference row, the
// hash moves when the window moves, and profile.json carries the row.
func TestProfileReportsEffectiveWindow(t *testing.T) {
	def := DefaultEvalProfile()
	if def.ContextWindow != ReferenceContextWindow || def.MaxInput != ReferenceMaxInput || def.MaxOutput != ReferenceMaxOutput {
		t.Fatalf("default profile window = %d/%d/%d, want %d/%d/%d",
			def.ContextWindow, def.MaxInput, def.MaxOutput,
			ReferenceContextWindow, ReferenceMaxInput, ReferenceMaxOutput)
	}
	// Reference model keeps the frozen hash.
	ref := ProfileForModel("gpt-6-luna")
	if ref.Hash() != def.Hash() {
		t.Fatal("reference model must keep the frozen profile hash")
	}
	// Unknown models report the conservative fallback with a moved hash.
	unk := ProfileForModel("some-future-model")
	if unk.ContextWindow != ConservativeWindow || unk.MaxInput != ConservativeMaxIn {
		t.Fatalf("unknown window = %+v, want conservative", unk)
	}
	if unk.Hash() == def.Hash() {
		t.Fatal("unknown window must move the profile hash")
	}
	// Case-insensitive reference lookup.
	if got := ProfileForModel("GPT-6-LUNA").Hash(); got != def.Hash() {
		t.Fatal("reference lookup must be case-insensitive")
	}
	// Records: WriteProfile persists the window row.
	dir := t.TempDir()
	if err := WriteProfile(dir, unk); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"context_window", "max_input", "max_output"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("profile.json must report %s", k)
		}
	}
	if int(doc["context_window"].(float64)) != ConservativeWindow {
		t.Fatalf("profile.json window = %v, want %d", doc["context_window"], ConservativeWindow)
	}
}

// The eval table mirrors the compiler table: a drift in either
// breaks the cross-check.
func TestProfileWindowMatchesCompiler(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	lim := ctxcompiler.LimitsForModel("gpt-6-luna", cfg)
	if lim.ContextWindow != ReferenceContextWindow || lim.MaxInput != ReferenceMaxInput || lim.MaxOutput != ReferenceMaxOutput {
		t.Fatalf("compiler reference = %+v, eval reference = %d/%d/%d",
			lim, ReferenceContextWindow, ReferenceMaxInput, ReferenceMaxOutput)
	}
	cons := ctxcompiler.LimitsForModel("unknown-xyz", cfg)
	if cons.ContextWindow != ConservativeWindow || cons.MaxInput != ConservativeMaxIn || cons.MaxOutput != ConservativeMaxOut {
		t.Fatalf("compiler conservative = %+v, eval conservative = %d/%d/%d",
			cons, ConservativeWindow, ConservativeMaxIn, ConservativeMaxOut)
	}
	if cfg.DefaultContextWindow != ConservativeWindow || cfg.DefaultMaxInput != ConservativeMaxIn {
		t.Fatal("compiler defaults must equal the eval conservative row")
	}
}
