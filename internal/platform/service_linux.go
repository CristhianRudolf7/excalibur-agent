//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// systemd unit managed by the installer (Section 25.2).
const defaultUnitName = "excalibur.service"

// LinuxServiceManager installs, removes and queries the per-user
// systemd unit. The unit file is always written; enable/start are
// attempted when systemctl exists and skipped otherwise (containers,
// minimal servers), in which case Status reports "installed".
type LinuxServiceManager struct {
	// UnitName overrides the unit file name. Empty means
	// excalibur.service.
	UnitName string
	// ExecPath overrides the daemon binary. Empty means the current
	// executable.
	ExecPath string
	// UnitDir overrides ~/.config/systemd/user. Tests set it to a
	// temp dir directly or via EXCALIBUR_SYSTEMD_DIR.
	UnitDir string
}

// Compile-time interface check.
var _ ServiceManager = LinuxServiceManager{}

func (m LinuxServiceManager) unitName() string {
	if m.UnitName != "" {
		return m.UnitName
	}
	return defaultUnitName
}

// managedDir resolves the unit directory and reports whether it was
// overridden (struct field or EXCALIBUR_SYSTEMD_DIR). Overridden
// directories are test/dev sandboxes outside systemd's unit search
// path, so Install/Uninstall skip the systemctl calls there and only
// manage the unit file.
func (m LinuxServiceManager) managedDir() (string, bool) {
	if m.UnitDir != "" {
		return m.UnitDir, true
	}
	if v := os.Getenv("EXCALIBUR_SYSTEMD_DIR"); v != "" {
		return v, true
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user"), false
}

func (m LinuxServiceManager) execPath() (string, error) {
	if m.ExecPath != "" {
		return m.ExecPath, nil
	}
	if v := os.Getenv("EXCALIBUR_EXEC_PATH"); v != "" {
		return v, nil
	}
	return os.Executable()
}

// unitBody renders the user unit: restart with backoff plus a
// watchdog. The daemon's interactive-path goroutine pets WATCHDOG=1
// via sd_notify; a hung daemon is detected and restarted (Section
// 25.2). Type=notify requires that sd_notify path, wired in H10.
func (m LinuxServiceManager) unitBody(execPath string) string {
	return `[Unit]
Description=Excalibur personal AI harness
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + execPath + ` daemon start
Restart=on-failure
RestartSec=5s
WatchdogSec=30s

[Install]
WantedBy=default.target
`
}

// Install implements platform.ServiceManager.
func (m LinuxServiceManager) Install() error {
	dir, custom := m.managedDir()
	if dir == "" {
		return errors.New("platform: cannot resolve systemd unit directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	bin, err := m.execPath()
	if err != nil {
		return err
	}
	unitPath := filepath.Join(dir, m.unitName())
	if err := os.WriteFile(unitPath, []byte(m.unitBody(bin)), 0o644); err != nil {
		return err
	}
	if custom {
		return nil
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("platform: systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", m.unitName()).CombinedOutput(); err != nil {
		return fmt.Errorf("platform: systemctl enable: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Uninstall implements platform.ServiceManager.
func (m LinuxServiceManager) Uninstall() error {
	dir, custom := m.managedDir()
	if dir == "" {
		return errors.New("platform: cannot resolve systemd unit directory")
	}
	unitPath := filepath.Join(dir, m.unitName())
	if !custom {
		if _, err := exec.LookPath("systemctl"); err == nil {
			_ = exec.Command("systemctl", "--user", "disable", "--now", m.unitName()).Run()
		}
	}
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !custom {
		if _, err := exec.LookPath("systemctl"); err == nil {
			_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		}
	}
	return nil
}

// Status implements platform.ServiceManager: "absent" without a unit
// file, the systemctl is-active state when available, else
// "installed".
func (m LinuxServiceManager) Status() (string, error) {
	dir, custom := m.managedDir()
	if dir == "" {
		return "", errors.New("platform: cannot resolve systemd unit directory")
	}
	if _, err := os.Stat(filepath.Join(dir, m.unitName())); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "absent", nil
		}
		return "", err
	}
	if custom {
		return "installed", nil
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "installed", nil
	}
	out, err := exec.Command("systemctl", "--user", "is-active", m.unitName()).CombinedOutput()
	state := strings.TrimSpace(string(out))
	if state == "" && err != nil {
		return "installed", nil
	}
	return state, nil
}
