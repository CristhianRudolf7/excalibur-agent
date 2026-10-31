//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxServiceManagerLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := LinuxServiceManager{
		UnitName: "excalibur-test.service",
		ExecPath: "/usr/bin/excalibur",
		UnitDir:  dir,
	}
	if st, err := m.Status(); err != nil || st != "absent" {
		t.Fatalf("Status before install = %q, %v; want absent", st, err)
	}
	if err := m.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "excalibur-test.service"))
	if err != nil {
		t.Fatalf("unit file missing: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"Restart=on-failure",
		"RestartSec=",
		"WatchdogSec=",
		"ExecStart=/usr/bin/excalibur daemon start",
		"WantedBy=default.target",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("unit file lacks %q:\n%s", want, body)
		}
	}
	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// Without systemctl the status is file-based; with systemctl it is
	// the is-active state. Both prove the unit is registered.
	if st != "installed" && st != "active" && st != "inactive" && st != "failed" && st != "activating" {
		t.Fatalf("Status after install = %q", st)
	}
	if err := m.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if st, err := m.Status(); err != nil || st != "absent" {
		t.Fatalf("Status after uninstall = %q, %v; want absent", st, err)
	}
	// Uninstall is idempotent.
	if err := m.Uninstall(); err != nil {
		t.Fatalf("second Uninstall: %v", err)
	}
}

func TestLinuxServiceManagerEnvDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EXCALIBUR_SYSTEMD_DIR", dir)
	m := LinuxServiceManager{UnitName: "excalibur-env.service", ExecPath: "/bin/true"}
	if err := m.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "excalibur-env.service")); err != nil {
		t.Fatalf("unit not in EXCALIBUR_SYSTEMD_DIR: %v", err)
	}
}
