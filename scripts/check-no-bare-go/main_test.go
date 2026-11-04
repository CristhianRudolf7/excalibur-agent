package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, root, path, source string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRejectsLaunchesAcrossBuildTags(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "worker_windows.go", "//go:build windows\n\npackage worker\nfunc f() {\n go work()\n go func() {}()\n}\n")
	var output bytes.Buffer
	if err := check(root, &output); err == nil {
		t.Fatal("bare goroutines accepted")
	}
	for _, line := range []string{"worker_windows.go:5", "worker_windows.go:6"} {
		if !strings.Contains(output.String(), line) {
			t.Fatalf("missing %s in %s", line, output.String())
		}
	}
}

func TestCheckAllowsSanctionedLaunchesAndIgnoresText(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "internal/safego/safego.go", "package safego\nfunc Go() { go work() }\n")
	fixture(t, root, "worker_test.go", "package worker\nfunc test() { go work() }\n")
	fixture(t, root, "worker.go", "package worker\n// go work()\n/* go work() */\nvar s = `go work()`\nfunc f() { safego.Go(work) }\n")
	fixture(t, root, "apps/web/node_modules/invalid.go", "not Go")
	fixture(t, root, ".git/invalid.go", "not Go")
	var output bytes.Buffer
	if err := check(root, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "OK:") {
		t.Fatal(output.String())
	}
}

func TestCheckReportsParseErrors(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "broken.go", "package broken\nfunc")
	if err := check(root, &bytes.Buffer{}); err == nil {
		t.Fatal("parse error ignored")
	}
}
