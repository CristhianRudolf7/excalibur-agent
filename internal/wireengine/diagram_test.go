package wireengine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/codeintel"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type codeDiagramRenderer struct{ Calls int }

func (r *codeDiagramRenderer) Render(_ context.Context, _, _, _ string) (string, error) {
	r.Calls++
	return "<svg/>", nil
}

func TestCodeDiagramSurfaceAndMediaWiring(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	renderer := &codeDiagramRenderer{}
	media := &tools.MediaDeps{Diagram: renderer}
	emb, err := BuildEmbedded(EmbeddedOptions{Workdir: root, RunID: "diagram-run", Media: media})
	if err != nil {
		t.Fatal(err)
	}
	if media.Workdir != "" {
		t.Fatal("mutated shared media dependencies")
	}
	tool, err := emb.Registry.Get(codeintel.CodeDiagramToolName)
	if err != nil {
		t.Fatal(err)
	}
	if !emb.Catalog.IsDeferred(codeintel.CodeDiagramToolName) || emb.Catalog.IsCore(codeintel.CodeDiagramToolName) {
		t.Fatal("diagram must be deferred")
	}
	result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"path":".","level":"overview"}`)})
	if err != nil || result.Status != tools.StatusOK || renderer.Calls != 1 || len(result.Artifacts) != 1 {
		t.Fatalf("wiring: %+v %v", result, err)
	}
	eval, err := BuildEmbedded(EmbeddedOptions{Profile: "eval", Workdir: root, Media: media})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eval.Registry.Get(codeintel.CodeDiagramToolName); err == nil || eval.Catalog.IsDeferred(codeintel.CodeDiagramToolName) {
		t.Fatal("code_diagram leaked into eval registry")
	}
}
