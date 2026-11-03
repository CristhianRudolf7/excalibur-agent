package wireengine

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
)

func TestIntentToolSelection(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	prefix := emb.Catalog.PrefixHash()
	core := emb.Catalog.CoreNames()
	ix := DefaultSkillsIndex()
	for _, tc := range []struct {
		request string
		want    []string
	}{
		{"draw an architecture diagram of this repo", []string{"code_diagram", "diagram_render"}},
		{"haz un diagrama de flujo", []string{"code_diagram", "diagram_render"}},
		{"Make two diagrams of this Go repository and a sequence diagram from text. Render them as SVG.", []string{"code_diagram", "diagram_render"}},
		{"visualize the architecture", []string{"code_diagram", "diagram_render"}},
		{"make a graph", []string{"code_diagram", "chart_render"}},
		{"plot a gráfico", []string{"chart_render"}},
		{"flowchart", []string{"diagram_render"}},
		{"esquema", []string{"diagram_render"}},
		{"explain this failing test", nil},
		{"fix this repository build and code", nil},
	} {
		t.Run(tc.request, func(t *testing.T) {
			got := SuggestTools(emb.Catalog, ix, tc.request, "test", "default")
			if len(got.Names) > 3 {
				t.Fatalf("unbounded activation: %v", got.Names)
			}
			for _, name := range tc.want {
				if !slices.Contains(got.Names, name) {
					t.Errorf("missing %s in %v", name, got.Names)
				}
			}
			if len(tc.want) == 0 {
				for _, name := range []string{"code_diagram", "diagram_render", "chart_render"} {
					if slices.Contains(got.Names, name) {
						t.Errorf("unrelated activation: %v", got.Names)
					}
				}
			} else {
				if len(got.Skills) != 1 || !strings.Contains(got.Skills[0].Why, "load_skill") {
					t.Fatalf("missing single skill hint: %+v", got.Skills)
				}
				loaded, err := ix.Load(got.Skills[0].Name)
				if err != nil {
					t.Fatal(err)
				}
				matched := false
				for _, name := range loaded.Tools {
					matched = matched || slices.Contains(got.Names, name)
				}
				if !matched {
					t.Fatalf("hint does not use activated tools: %+v", got)
				}
			}
			if !reflect.DeepEqual(got, SuggestTools(emb.Catalog, ix, tc.request, "test", "default")) {
				t.Fatal("nondeterministic selection")
			}
			if eval := SuggestTools(emb.Catalog, ix, tc.request, "test", "eval"); len(eval.Names) != 0 || len(eval.Skills) != 0 {
				t.Fatal("eval activation", eval)
			}
		})
	}
	if prefix != emb.Catalog.PrefixHash() || !reflect.DeepEqual(core, emb.Catalog.CoreNames()) {
		t.Fatal("selection changed the core prefix")
	}
}

func TestIntentActivationJournal(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	activation := SuggestTools(emb.Catalog, DefaultSkillsIndex(), "architecture diagram", "test", "default")
	ctx := context.Background()
	store := journal.NewMemStore()
	if err := activation.Record(ctx, store, "run"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(ctx, "run")
	if err != nil || len(entries) != 1 || entries[0].Type != "tools_activated" {
		t.Fatalf("entries: %+v %v", entries, err)
	}
	var recorded ToolActivation
	if err := json.Unmarshal(entries[0].Payload, &recorded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(activation.Names, recorded.Names) || !reflect.DeepEqual(activation.Skills, recorded.Skills) {
		t.Fatal("incomplete activation record")
	}
	if err := (ToolActivation{}).Record(ctx, store, "run"); err != nil {
		t.Fatal(err)
	}
	entries, _ = store.List(ctx, "run")
	if len(entries) != 1 {
		t.Fatal("empty selection must not write a record")
	}
}
