package wireengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/codeintel"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestImpactedChecks covers Sections 10.10 and 18.2: after editing
// files, the impacted tests from the code graph join the
// during-work selection, so the check covering the impacted test
// runs even when the change itself names no check.
func TestImpactedChecks(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"lib.py":      "def base():\n    return 1\n",
		"mid.py":      "from lib import base\n\ndef mid():\n    return base() + 1\n",
		"test_mid.py": "from mid import mid\n\ndef test_mid():\n    assert mid() == 2\n",
	})
	g, err := codeintel.OpenGraph(filepath.Join(t.TempDir(), "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	x := codeintel.NewIndexer(g, root)
	if _, err := x.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	all := []tasks.CriterionChecks{
		{
			Criterion: types.Criterion{ID: "crt_mid", ImpactArea: []string{"test_mid.py"}},
			Checks:    []types.Check{{ID: "chk_mid", CriterionID: "crt_mid", Definition: "pytest test_mid.py"}},
		},
		{
			Criterion: types.Criterion{ID: "crt_other", ImpactArea: []string{"other.py"}},
			Checks:    []types.Check{{ID: "chk_other", CriterionID: "crt_other", Definition: "pytest other"}},
		},
	}
	// Editing lib.py impacts test_mid.py through the graph, so the
	// mid check runs although lib.py names no criterion directly.
	got := ImpactedChecks(all, []string{"lib.py"}, map[types.CriterionID]bool{}, g)
	ids := map[types.CheckID]bool{}
	for _, c := range got {
		ids[c.ID] = true
	}
	if !ids["chk_mid"] || ids["chk_other"] {
		t.Fatalf("impacted selection = %v, want only chk_mid", got)
	}
	// Without the graph the same change selects nothing.
	plain := tasks.SelectDuringWork(all, []string{"lib.py"}, map[types.CriterionID]bool{})
	if len(plain) != 0 {
		t.Fatalf("plain selection without graph = %v, want none", plain)
	}
	if files := ImpactedTestFiles(g, []string{"lib.py"}); len(files) != 1 || files[0] != "test_mid.py" {
		t.Fatalf("impacted files = %v", files)
	}
}
