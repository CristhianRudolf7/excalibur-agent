package codeintel

import (
	"encoding/json"
	"testing"
)

func TestCodeDiagramArguments(t *testing.T) {
	a, err := parseDiagramArgs(json.RawMessage(`{"path":".","level":"overview"}`))
	if err != nil || a.MaxNodes != 40 || a.Format != "mermaid" || a.Output != "svg" || a.Depth != 3 {
		t.Fatalf("defaults: %+v %v", a, err)
	}
	for _, raw := range []string{`{}`, `{"path":".","level":"bad"}`, `{"path":".","level":"flow"}`, `{"path":".","level":"overview","format":"svg"}`, `{"path":".","level":"overview","max_nodes":1}`, `{"path":".","level":"overview","max_nodes":201}`, `{"path":".","level":"overview","direction":"bad"}`, `{"path":".","level":"overview","depth":0}`, `{"path":".","level":"overview","output":"jpg"}`} {
		if _, err := parseDiagramArgs(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	d := (&CodeDiagramTool{}).Definition()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, p := range d.Profiles {
		if p == "eval" {
			t.Fatal("diagram tool must stay out of eval")
		}
	}
}
