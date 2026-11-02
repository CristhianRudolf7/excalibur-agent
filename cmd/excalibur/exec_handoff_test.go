package main

import (
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"os"
	"path/filepath"
	"strings"
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

func TestExecLeadingDashInstruction(t *testing.T) {
	for _, sep := range []bool{false, true} {
		t.Run(fmt.Sprint(sep), func(t *testing.T) {
			logs := t.TempDir()
			args := []string{"exec", "--profile", "eval", "--logs-dir", logs}
			if sep {
				args = append(args, "--")
			}
			args = append(args, "ignored earlier positional", "- Follow the user's instructions")
			if code := run(args); code != ExitOK {
				t.Fatalf("exit = %d", code)
			}
			trajectory, err := os.ReadFile(filepath.Join(logs, "trajectory.json"))
			if err != nil || !strings.Contains(string(trajectory), "- Follow the user") || strings.Contains(string(trajectory), "ignored earlier positional") {
				t.Fatalf("last positional instruction not preserved: %v", err)
			}
		})
	}
}

func TestExecLeadingDashInstructionHandoff(t *testing.T) {
	request := "- Follow the user's instructions"
	argv, env, path, err := cli.PrepareInstructionArgv([]string{"excalibur", "exec", "--profile", "eval", "--logs-dir", t.TempDir(), "--", request}, os.Environ(), request)
	if err != nil || path == "" {
		t.Fatalf("handoff: %q %v", path, err)
	}
	defer os.Remove(path)
	for _, entry := range env {
		if strings.HasPrefix(entry, cli.InstructionFileEnv+"=") {
			t.Setenv(cli.InstructionFileEnv, strings.TrimPrefix(entry, cli.InstructionFileEnv+"="))
		}
	}
	if code := run(argv[1:]); code != ExitOK {
		t.Fatalf("re-exec exit = %d", code)
	}
}

func TestExecLeadingDashSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_EXEC_LEADING_DASH", "0")
	if code := run([]string{"exec", "- Follow instructions"}); code != ExitError {
		t.Fatalf("exit = %d", code)
	}
}
