package wireengine

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"testing"
)

func TestClassroomIntentAndEvalIsolation(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !emb.Catalog.IsDeferred("classroom") {
		t.Fatal("classroom must be deferred")
	}
	for _, text := range []string{"quiero aprender algebra", "teach me algebra"} {
		a := SuggestTools(emb.Catalog, nil, text, "test", "default")
		found := false
		for _, d := range a.Definitions {
			found = found || d.Name == "classroom"
		}
		if !found {
			t.Fatalf("classroom missing for %q", text)
		}
	}
	frozen, err := BuildEmbedded(EmbeddedOptions{Profile: "eval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := frozen.Registry.Get("classroom"); err == nil {
		t.Fatal("classroom in eval")
	}
	if got := eval.DefaultEvalProfile().Hash(); got != "4ecb6a58986660326b7d20d912918d479e7c72140c6e04ab38d83a23315fec28" {
		t.Fatalf("eval changed: %s", got)
	}
}
