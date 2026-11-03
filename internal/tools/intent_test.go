package tools_test

import (
	"reflect"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	toolfake "github.com/CristhianRudolf7/excalibur-agent/internal/tools/fake"
)

func TestIntentSuggestionsUseMetadataAndBoundSelection(t *testing.T) {
	reg := tools.NewMapRegistry()
	cat := tools.NewDeferredCatalog(reg)
	for _, name := range []string{"z", "d", "c", "b", "a"} {
		tool := toolfake.New(name)
		tool.Def.Description = "a completely unrelated phrase"
		tool.Def.Tags = []string{"widget"}
		tool.Def.Keywords = []string{"gráfico"}
		tool.Def.Group = "generic"
		if err := cat.RegisterDeferred(tool); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"completely unrelated phrase", "generic", ""} {
		if got := cat.Suggest(query); len(got) != 0 {
			t.Fatalf("prose or group activated tools for %q: %+v", query, got)
		}
	}
	for _, query := range []string{"widgets", "GRÁFICOS"} {
		got := cat.Suggest(query)
		var names []string
		for _, hit := range got {
			names = append(names, hit.Definition.Name)
		}
		if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
			t.Fatalf("bounded deterministic selection for %q: %v", query, names)
		}
	}
	if len(cat.Search("completely unrelated phrase", 5)) != 5 {
		t.Fatal("ordinary search must still include descriptions")
	}
}
