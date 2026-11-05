//go:build darwin

package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDarwinCGLRequirementsAndEnvironment(t *testing.T) {
	t.Setenv("MUJOCO_GL", "osmesa")
	for _, key := range []string{"DYLD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "LD_LIBRARY_PATH", "LD_PRELOAD", "PYTHONPATH", "PYTHONHOME"} {
		t.Setenv(key, "/untrusted")
		if platformEnvAllowed(key) {
			t.Fatalf("platform permits %s", key)
		}
	}
	for _, install := range []bool{false, true} {
		env := map[string]string{}
		for _, entry := range childEnvironment(install) {
			name, value, _ := strings.Cut(entry, "=")
			env[name] = value
		}
		if env["MUJOCO_GL"] != "cgl" {
			t.Fatalf("wrong rendering backend: %q", env["MUJOCO_GL"])
		}
		for key := range env {
			if strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "LD_") || key == "PYTHONPATH" || key == "PYTHONHOME" {
				t.Fatalf("inherited %s", key)
			}
		}
	}
	raw, err := json.Marshal(Requirements{Python: ">=3.10", OSMesa: detectOSMesa()})
	var requirements map[string]any
	if err != nil || json.Unmarshal(raw, &requirements) != nil || len(requirements) != 2 || requirements["python"] != ">=3.10" || requirements["osmesa"] != true {
		t.Fatalf("requirements: %s, %v", raw, err)
	}
	if strings.Contains(runtimeUnavailableOutput(), "OSMesa") || !strings.Contains(prerequisiteMessage(), "CGL") {
		t.Fatal("Linux rendering hints on macOS")
	}
}

func TestDarwinPythonSearch(t *testing.T) {
	for _, found := range []string{"python3", "/opt/homebrew/bin/python3", "/usr/local/bin/python3", ""} {
		var calls []string
		got := findDarwinPython(func(name string) (string, error) {
			calls = append(calls, name)
			if name == found {
				return "/resolved/python3", nil
			}
			return "", os.ErrNotExist
		})
		want := "/resolved/python3"
		if found == "" {
			want = "python3"
		}
		if got != want {
			t.Fatalf("got %q for %q", got, found)
		}
		if len(calls) == 0 || calls[0] != "python3" {
			t.Fatal("PATH must take precedence")
		}
	}
}

func TestDarwinRuntimeRequirements(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "requirements.lock")
	original := []byte("--extra-index-url https://download.pytorch.org/whl/cpu\ntorch==2.15.0+cpu\nnumpy==2.5.4\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	macPath, data, err := prepareRuntimeRequirements(path, original)
	if err != nil {
		t.Fatal(err)
	}
	if macPath == path || !strings.Contains(string(data), "torch==2.15.0\n") || strings.Contains(string(data), "+cpu") || strings.Contains(string(data), "download.pytorch.org") {
		t.Fatalf("mac requirements: %q %s", macPath, data)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(unchanged, original) {
		t.Fatal("embedded Linux requirements changed")
	}
	info, err := os.Stat(macPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("requirements must be private")
	}
	if _, _, err := prepareRuntimeRequirements(filepath.Join(dir, "missing", "requirements.lock"), original); err == nil {
		t.Fatal("write failure ignored")
	}
}

func TestDarwinInstallerCommands(t *testing.T) {
	runner := &fakeRunner{version: "3.12"}
	installer, events := installerFixture(t, runner)
	// Exercise dependency installation with a fake runner, without network access.
	if err := installer.install(context.Background()); err != nil {
		t.Fatal(err)
	}
	event := awaitInstall(t, events)
	if event["phase"] != "done" || strings.Contains(event["message"].(string), "OSMesa") {
		t.Fatal(event)
	}
	runner.mu.Lock()
	calls := append([][]string(nil), runner.calls...)
	runner.mu.Unlock()
	if len(calls) != 5 {
		t.Fatalf("commands: %v", calls)
	}
	if !reflect.DeepEqual(calls[2][1:5], []string{"-m", "pip", "install", "-r"}) {
		t.Fatalf("dependency command: %v", calls[2])
	}
	if calls[1][0] != calls[0][0] || !strings.HasSuffix(calls[2][5], "requirements-darwin.lock") {
		t.Fatalf("Python/requirements: %v", calls)
	}
	if _, err := os.Stat(filepath.Join(installer.DataDir, "robotlab", "venv", ".robotlab-installed")); err != nil {
		t.Fatal(err)
	}
}

func TestDarwinRuntimeArchitectureGate(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		supported := darwinRuntimeSupported(arch)
		if supported != (arch == "arm64") {
			t.Fatalf("%s: supported=%v", arch, supported)
		}
		var output struct {
			Status string `json:"status"`
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal([]byte(darwinRuntimeUnavailableOutput(arch)), &output); err != nil {
			t.Fatal(err)
		}
		if output.Status != "unavailable" {
			t.Fatalf("%s: status=%q", arch, output.Status)
		}
		if arch == "amd64" {
			if !strings.Contains(output.Detail, "Robot Lab on macOS requires Apple Silicon") {
				t.Fatalf("Intel prerequisite missing: %q", output.Detail)
			}
		} else if !strings.Contains(output.Detail, "CGL") || !strings.Contains(output.Detail, "explicit user action") {
			t.Fatalf("Apple Silicon installation hint missing: %q", output.Detail)
		}
	}
	if runtimeSupported() != darwinRuntimeSupported(runtime.GOARCH) || runtimeUnavailableOutput() != darwinRuntimeUnavailableOutput(runtime.GOARCH) {
		t.Fatal("runtime helpers do not use the host architecture")
	}
}

func TestDarwinIntelInstallerRejectsBeforeRunning(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("Intel macOS installer gate")
	}
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	runner := &fakeRunner{version: "3.12"}
	installer := &Installer{DataDir: t.TempDir(), Runner: runner}
	defer installer.Close()
	err := installer.Start()
	var unavailable *RuntimeError
	if !errors.As(err, &unavailable) || unavailable.Code != "unavailable" {
		t.Fatalf("installer gate: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("unsupported installer started Python")
	}
}

func TestDarwinPythonChildPreservesCGL(t *testing.T) {
	python, _, _ := bootstrapPython()
	if _, err := exec.LookPath(python); err != nil {
		t.Skip("Python 3 is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import os
assert os.environ["MUJOCO_GL"] == "cgl"
`)
	cmd.Dir = "runtime"
	cmd.Env = append(childEnvironment(false), "MUJOCO_GL=cgl")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Python CGL environment: %v: %s", err, out)
	}
}
