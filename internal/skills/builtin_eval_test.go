package skills

import (
	"strings"
	"testing"
)

func TestEvalPDFSkillRetainsBaseline(t *testing.T) {
	frozen := NewEvalBuiltinIndex()
	current := NewBuiltinIndex()
	if err := frozen.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := current.Scan(); err != nil {
		t.Fatal(err)
	}
	before, ok := frozen.Get("pdf-documents")
	if !ok {
		t.Fatal("missing frozen PDF skill")
	}
	if before.Hash != "5d2d1f2abf0290ad6c1943178fa47c9771d82bda095a21ab80b5cccf698b6d48" {
		t.Fatalf("baseline moved: %s", before.Hash)
	}
	after, ok := current.Get("pdf-documents")
	if !ok {
		t.Fatal("missing current PDF skill")
	}
	if before.Hash == after.Hash || !strings.Contains(after.Body, "form_lock") || strings.Contains(before.Body, "form_lock") {
		t.Fatal("eval and assistant PDF guidance are not separated")
	}
	loaded, err := frozen.Load("pdf-documents")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(loaded.Body, "form_lock") {
		t.Fatal("eval loaded assistant guidance")
	}
}

func TestEvalDiagramSkillsRetainBaseline(t *testing.T) {
	frozen, current := NewEvalBuiltinIndex(), NewBuiltinIndex()
	if err := frozen.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := current.Scan(); err != nil {
		t.Fatal(err)
	}
	for name, hash := range map[string]string{
		"diagram-rendering": "62d99255acb21ae540f911d6559cd6a8769e16fcc0c3229f2e30d293497eb036",
		"data-charts":       "2be05a769f15f7d598a285d6c9c83e9f0d28008bb560854a5289d1e83dacb249",
	} {
		before, ok := frozen.Get(name)
		if !ok || before.Hash != hash {
			t.Fatalf("%s eval baseline moved: %+v", name, before)
		}
		after, ok := current.Get(name)
		if !ok || before.Hash == after.Hash {
			t.Fatalf("%s current and eval guidance were not separated", name)
		}
		loaded, err := frozen.Load(name)
		if err != nil || strings.Contains(loaded.Body, "Result.Artifacts") {
			t.Fatalf("%s eval loaded assistant guidance: %v", name, err)
		}
	}
}
