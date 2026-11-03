package wireengine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestAvatarIntentExecutionAndEvalIsolation(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = emb.Close() })
	if !emb.Catalog.IsDeferred("design_avatar") {
		t.Fatal("avatar tool must be deferred")
	}
	for _, query := range []string{"avatar", "icon", "character", "appearance", "icono", "muñeco", "personaje", "apariencia"} {
		a := SuggestTools(emb.Catalog, nil, query, "test", "default")
		found := false
		for _, d := range a.Definitions {
			found = found || d.Name == "design_avatar"
		}
		if !found {
			t.Errorf("avatar not activated for %q", query)
		}
	}
	result, err := emb.Exec.Execute(context.Background(), "call_tool", json.RawMessage(`{"name":"design_avatar","arguments":{"description":"a friendly helper"}}`), tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatalf("call_tool: %+v, %v", result, err)
	}
	frozen, err := BuildEmbedded(EmbeddedOptions{Profile: "eval"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = frozen.Close() })
	if _, err := frozen.Registry.Get("design_avatar"); err == nil {
		t.Fatal("avatar in eval")
	}
}
