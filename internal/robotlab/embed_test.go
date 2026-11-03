package robotlab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRuntime(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(data, "robotlab", "src", strings.Repeat("a", 64))
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := ExtractRuntime(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("obsolete extraction not pruned")
	}
	b, err := ExtractRuntime(data)
	if err != nil || a != b {
		t.Fatalf("reuse: %s %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(a, "robotlab", "__init__.py")); err != nil {
		t.Fatalf("Python package initializer missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a, "robotlab", "scenario.py")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a, "tests")); !os.IsNotExist(err) {
		t.Fatal("tests extracted")
	}
}
