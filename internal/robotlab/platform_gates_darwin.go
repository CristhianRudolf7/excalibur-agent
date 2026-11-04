package robotlab

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The pinned torch release has macOS wheels only for Apple Silicon.
func runtimeSupported() bool { return darwinRuntimeSupported(runtime.GOARCH) }

func darwinRuntimeSupported(arch string) bool { return arch == "arm64" }

// platformEnvAllowed reports whether an OS-specific variable name may
// reach the runtime in addition to the shared allowlist.
func platformEnvAllowed(string) bool { return false }

// mujocoGL is the MuJoCo offscreen rendering backend (MUJOCO_GL).
func mujocoGL() string { return "cgl" }

// CGL is built into macOS: no OSMesa library is needed, so the legacy
// osmesa status field reports the requirement as satisfied.
func needsOSMesa() bool { return false }

func venvPython(dir string) string { return filepath.Join(dir, "bin", "python") }

func bootstrapPython() (string, []string, error) { return findDarwinPython(exec.LookPath), nil, nil }

func findDarwinPython(lookup func(string) (string, error)) string {
	for _, name := range []string{"python3", "/opt/homebrew/bin/python3", "/usr/local/bin/python3"} {
		if path, err := lookup(name); err == nil {
			return path
		}
	}
	// Preserve the normal executable-not-found error from the command runner.
	return "python3"
}

func prerequisiteMessage() string { return "Checking Python and built-in CGL rendering" }

// macOS wheels use the public torch version, without Linux's +cpu local
// version identifier. Keep the embedded Linux lock unchanged and derive a
// private macOS copy beside it for both pip steps.
func prepareRuntimeRequirements(path string, data []byte) (string, []byte, error) {
	lines := strings.Split(string(data), "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) == "--extra-index-url https://download.pytorch.org/whl/cpu" {
			lines[index] = ""
		}
		if strings.HasPrefix(strings.TrimSpace(line), "torch==") {
			lines[index] = strings.Replace(line, "+cpu", "", 1)
		}
	}
	data = []byte(strings.Join(lines, "\n"))
	path = filepath.Join(filepath.Dir(path), "requirements-darwin.lock")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", nil, err
	}
	return path, data, nil
}

func torchInstallArgs(pin string) []string { return []string{"-m", "pip", "install", pin} }

func runtimeUnavailableOutput() string {
	return darwinRuntimeUnavailableOutput(runtime.GOARCH)
}

func darwinRuntimeUnavailableOutput(arch string) string {
	if !darwinRuntimeSupported(arch) {
		return `{"status":"unavailable","detail":"Robot Lab on macOS requires Apple Silicon. The pinned torch release has no macOS x86_64 wheel."}`
	}
	return `{"status":"unavailable","detail":"Robot Lab is unavailable. Enable EXCALIBUR_ROBOTLAB and use the Robot Lab Install card to install Python dependencies; macOS provides CGL rendering. Installation requires an explicit user action."}`
}
