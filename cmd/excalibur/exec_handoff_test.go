package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// TestExecReadsRequestFromInstructionFile covers the entry point of the argv
// hand-off: the request comes from the --instruction-file path, the file is
// consumed, and the exec scope set for the run is restored afterwards.
func TestExecReadsRequestFromInstructionFile(t *testing.T) {
	logs := t.TempDir()
	path := filepath.Join(t.TempDir(), "instruction")
	if err := os.WriteFile(path, []byte("say hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"exec", "--profile", "eval", "--logs-dir", logs, "--instruction-file", path}); code != ExitOK {
		t.Fatalf("exec from instruction file exit = %d, want %d", code, ExitOK)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("instruction file not consumed")
	}
	if tools.CurrentExecScope().Active {
		t.Fatal("exec scope leaked past the run")
	}
}

func TestExecMissingInstructionFileIsUsageError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if code := run([]string{"exec", "--profile", "eval", "--logs-dir", t.TempDir(), "--instruction-file", missing}); code != ExitError {
		t.Fatalf("missing instruction file exit = %d, want %d", code, ExitError)
	}
}
