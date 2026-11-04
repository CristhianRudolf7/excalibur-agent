package tools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsPackagePlans(t *testing.T) {
	for _, manager := range []string{"winget", "scoop", "choco"} {
		lookup := func(name string) (string, error) {
			if name == manager {
				return name, nil
			}
			return "", exec.ErrNotFound
		}
		plan, handled := platformCommandPlan(&MissingDep{Name: "git"}, lookup)
		if !handled || plan == nil || plan.Manager != manager {
			t.Fatalf("%s: %+v", manager, plan)
		}
		if strings.Contains(strings.Join(plan.Commands, " "), "sudo") {
			t.Fatal(plan.Commands)
		}
		if manager == "winget" && plan.Commands[0] != "winget install --id Git.Git -e --accept-source-agreements --accept-package-agreements" {
			t.Fatal(plan.Commands)
		}
	}
	if got := sudoPrefix(func(string) (string, error) { return "sudo", nil }); got != "" {
		t.Fatal(got)
	}
	if plan, _ := platformCommandPlan(&MissingDep{Name: "unknown"}, exec.LookPath); plan != nil {
		t.Fatal(plan)
	}
}

func TestWindowsSyntaxInterpreterSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("PATHEXT", ".EXE;.CMD")
	python := filepath.Join(dir, "python.exe")
	if err := os.WriteFile(python, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := pythonSyntaxCommand(); got[0] != "python" {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(dir, "py.exe"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := pythonSyntaxCommand(); len(got) < 2 || got[0] != "py" || got[1] != "-3" {
		t.Fatal(got)
	}
	store := filepath.Join(dir, "Microsoft", "WindowsApps")
	if err := os.MkdirAll(store, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "python.exe"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", store)
	if _, err := syntaxCheckLookPath("python"); err == nil {
		t.Fatal("accepted the Store alias")
	}
	if err := os.WriteFile(filepath.Join(store, "py.exe"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := syntaxCheckLookPath("py"); err != nil {
		t.Fatalf("valid Python manager alias: %v", err)
	}
	if err := checkShell("echo hello"); !errors.Is(err, errCheckUnavailable) {
		t.Fatalf("missing sh: %v", err)
	}
}
