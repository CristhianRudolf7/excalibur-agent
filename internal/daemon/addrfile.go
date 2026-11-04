package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AddrEnv overrides the daemon address for every client surface
// (CLI, MCP, ACP): EXCALIBUR_DAEMON_ADDR=127.0.0.1:port.
const AddrEnv = "EXCALIBUR_DAEMON_ADDR"

// AddrFileName is the rendezvous file the daemon writes on start and
// every client reads when the env override is absent. It lives in the
// per-user runtime dir (XDG_RUNTIME_DIR) so only the user can read
// the loopback address of their daemon.
const AddrFileName = "daemon.addr"

// WriteAddrFile publishes the bound gate address for clients. dir is
// the runtime dir; it is created with user-only permissions.
func WriteAddrFile(dir, addr string) error {
	if dir == "" {
		return fmt.Errorf("daemon: no runtime dir for addr file")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("daemon: mkdir runtime: %w", err)
	}
	tmp := filepath.Join(dir, AddrFileName+".tmp")
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(addr)+"\n"), 0o600); err != nil {
		return fmt.Errorf("daemon: write addr file: %w", err)
	}
	return os.Rename(tmp, filepath.Join(dir, AddrFileName))
}

// ReadAddrFile returns the published daemon address, or "" when the
// daemon never started here.
func ReadAddrFile(dir string) string {
	if dir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(dir, AddrFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// RemoveAddrFile drops the rendezvous on clean shutdown.
func RemoveAddrFile(dir string) {
	if dir == "" {
		return
	}
	_ = os.Remove(filepath.Join(dir, AddrFileName))
}

// ResolveAddr finds the daemon: explicit env first, then the runtime
// rendezvous file. The second return is false when no address is
// configured at all (the caller runs embedded).
func ResolveAddr(runtimeDir string) (string, bool) {
	if addr := strings.TrimSpace(os.Getenv(AddrEnv)); addr != "" {
		return addr, true
	}
	if addr := ReadAddrFile(runtimeDir); addr != "" {
		return addr, true
	}
	if dir := rendezvousRuntimeRoot(); dir != "" && dir != runtimeDir {
		if addr := ReadAddrFile(filepath.Join(dir, "excalibur")); addr != "" {
			return addr, true
		}
	}
	return "", false
}

// RuntimeAddrDir is the default rendezvous directory under a runtime
// root ($XDG_RUNTIME_DIR or the platform runtime dir).
func RuntimeAddrDir(runtimeRoot string) string {
	return filepath.Join(runtimeRoot, "excalibur")
}
