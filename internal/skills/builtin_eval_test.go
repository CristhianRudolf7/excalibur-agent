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
