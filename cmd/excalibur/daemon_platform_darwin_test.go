//go:build darwin

package main

import (
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestDarwinPlatformWiring(t *testing.T) {
	if _, ok := daemonSampler().(platform.DarwinResourceSampler); !ok {
		t.Fatal("wrong Darwin sampler")
	}
	if ram := physicalRAMBytes(); ram == 0 {
		t.Fatal("no physical RAM")
	}
	if keyring, ok := daemonKeyring().(platform.DarwinKeyring); !ok || keyring.Fallback != nil {
		t.Fatal("wrong Keychain or fallback wiring")
	}
	if _, ok := installDirs().(platform.DarwinPaths); !ok {
		t.Fatal("wrong installer paths")
	}
	if _, ok := installService().(platform.DarwinServiceManager); !ok {
		t.Fatal("wrong installer service")
	}
}

func TestDarwinInstallerPaths(t *testing.T) {
	base := t.TempDir()
	for key, dir := range map[string]string{"XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_CACHE_HOME": "cache", "XDG_RUNTIME_DIR": "runtime"} {
		t.Setenv(key, filepath.Join(base, dir))
	}
	installer := daemon.Installer{Dirs: installDirs()}
	report, err := installer.Install(daemon.InstallOptions{Timezone: "UTC", VaultMode: "passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if report.DataDir != filepath.Join(base, "data", "excalibur") || report.RuntimeDir != filepath.Join(base, "runtime") {
		t.Fatal("installer paths differ from Darwin platform paths")
	}
}

func TestDarwinSupervisorWithoutNotifySocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	watchdog := daemon.NewWatchdog(daemon.WatchdogConfig{}, func() bool { return true })
	if err := watchdog.Ready(); err != nil {
		t.Fatal(err)
	}
	if petted, err := watchdog.Tick(); err != nil || petted {
		t.Fatalf("watchdog tick = %v, %v", petted, err)
	}
	if err := watchdog.Stopping(); err != nil {
		t.Fatal(err)
	}
}
