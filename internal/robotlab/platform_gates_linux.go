package robotlab

import "path/filepath"

// Per-OS gate for the Robot Lab Python runtime. Each OS flips its own
// file.
// runtimeSupported reports whether the Robot Lab sidecar and its
// installer can run on this platform.
func runtimeSupported() bool { return true }

// platformEnvAllowed reports whether an OS-specific variable name may
// reach the runtime in addition to the shared allowlist.
func platformEnvAllowed(string) bool { return false }

// mujocoGL is the MuJoCo offscreen rendering backend (MUJOCO_GL).
func mujocoGL() string { return "osmesa" }

func venvPython(dir string) string               { return filepath.Join(dir, "bin", "python") }
func bootstrapPython() (string, []string, error) { return "python3", nil, nil }
func needsOSMesa() bool                          { return true }

func prerequisiteMessage() string { return "Checking Python and OSMesa" }
