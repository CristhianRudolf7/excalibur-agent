package migrations

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestRegistriesValidate asserts the embedded registries are
// well-formed: numbered from 1, gapless, and versioned.
func TestRegistriesValidate(t *testing.T) {
	main := Main()
	if err := main.Validate(); err != nil {
		t.Fatal(err)
	}
	if main.Latest() != 4 {
		t.Fatalf("main latest = %d, want 4", main.Latest())
	}
	tele := Telemetry()
	if err := tele.Validate(); err != nil {
		t.Fatal(err)
	}
	if tele.Latest() != 1 {
		t.Fatalf("telemetry latest = %d, want 1", tele.Latest())
	}
	seen := map[string]bool{}
	for _, m := range main.Ordered() {
		if m.Up == nil || m.Name == "" {
			t.Fatalf("migration v%d incomplete", m.Version)
		}
		if seen[m.Name] {
			t.Fatalf("duplicate migration name %q", m.Name)
		}
		seen[m.Name] = true
	}
	// Duplicate and gap detection.
	dup := New()
	first := main.Ordered()[0]
	_ = dup.Register(first)
	if err := dup.Register(first); err == nil {
		t.Fatal("expected duplicate version error")
	}
	gap := New()
	_ = gap.Register(store.Migration{Version: 2, Name: "gap"})
	if err := gap.Validate(); err == nil {
		t.Fatal("expected gap error")
	}
}
