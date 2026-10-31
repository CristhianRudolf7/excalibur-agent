package codeintel

import (
	"context"
	"testing"
)

// TestImpactedTestsKnownChanges is the Section 10.11 verification row
// (code-graph part): after editing files, the impacted tests must be
// exactly the tests covering the change, and the graph must have
// refreshed incrementally.
func TestImpactedTestsKnownChanges(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"lib.py":        "def base():\n    return 1\n",
		"mid.py":        "from lib import base\n\ndef mid():\n    return base() + 1\n",
		"test_mid.py":   "from mid import mid\n\ndef test_mid():\n    assert mid() == 2\n",
		"other.py":      "def other():\n    return 0\n",
		"test_other.py": "from other import other\n\ndef test_other():\n    assert other() == 0\n",
	})
	g := testGraph(t)
	x := NewIndexer(g, root)
	ctx := context.Background()
	if _, err := x.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	// Baseline: no test is impacted by nothing.
	if res := g.ImpactedTests(nil); len(res.Files()) != 0 || res.Conservative {
		t.Fatalf("empty change: %+v", res)
	}
	// Edit the leaf: only its test chain is impacted.
	writeTree(t, root, map[string]string{
		"lib.py": "def base():\n    return 2\n",
	})
	rep, err := x.NotifyChanged(ctx, []string{"lib.py"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Updated != 1 {
		t.Fatalf("expected incremental update of 1 file: %+v", rep)
	}
	res := g.ImpactedTests([]string{"lib.py"})
	assertFiles(t, res.Files(), []string{"test_mid.py"})
	if res.Conservative {
		t.Error("known path must not be conservative")
	}
	// Edit the middle: its direct test is impacted.
	writeTree(t, root, map[string]string{
		"mid.py": "from lib import base\n\ndef mid():\n    return base() + 2\n",
	})
	if _, err := x.NotifyChanged(ctx, []string{"mid.py"}); err != nil {
		t.Fatal(err)
	}
	assertFiles(t, g.ImpactedTests([]string{"mid.py"}).Files(), []string{"test_mid.py"})
	// Edit the test itself: it selects itself.
	assertFiles(t, g.ImpactedTests([]string{"test_other.py"}).Files(), []string{"test_other.py"})
	// Unrelated change selects only its own test.
	assertFiles(t, g.ImpactedTests([]string{"other.py"}).Files(), []string{"test_other.py"})
	// Multiple changes union.
	assertFiles(t, g.ImpactedTests([]string{"lib.py", "other.py"}).Files(),
		[]string{"test_mid.py", "test_other.py"})
	// Unknown path is conservative: every test file.
	res = g.ImpactedTests([]string{"nope/missing.go"})
	if !res.Conservative {
		t.Error("unknown path must be conservative")
	}
	assertFiles(t, res.Files(), []string{"test_mid.py", "test_other.py"})
}

func TestImpactedTestsViaModuleImport(t *testing.T) {
	// A test importing the changed module without naming a symbol
	// (table-driven) is still selected through its import edge.
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pkg/core.py":      "VALUE = 1\n",
		"pkg/test_core.py": "import pkg.core\n\ndef test_core():\n    assert pkg.core.VALUE > 0\n",
	})
	g := testGraph(t)
	x := NewIndexer(g, root)
	if _, err := x.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := g.ImpactedTests([]string{"pkg/core.py"})
	assertFiles(t, res.Files(), []string{"pkg/test_core.py"})
}

func TestTestFilesAndAll(t *testing.T) {
	g := testGraph(t)
	for rel, src := range map[string]string{
		"a.go":      "package p\n\nfunc A() {}\n",
		"b_test.go": "package p\n\nfunc TestB() {}\n",
		"c.py":      "def test_inline():\n    pass\n",
	} {
		if err := g.UpsertFile(mustParse(t, rel, src)); err != nil {
			t.Fatal(err)
		}
	}
	assertFiles(t, g.TestFiles(), []string{"b_test.go", "c.py"})
}

func assertFiles(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("files=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files=%v want %v", got, want)
		}
	}
}
