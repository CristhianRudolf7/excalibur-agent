//go:build !darwin

package main

import (
	"os"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

func TestNonDarwinInstallReportUnchanged(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stdout := os.Stdout
	os.Stdout = file
	t.Cleanup(func() { os.Stdout = stdout })
	printInstallReport(daemon.InstallReport{DataDir: "/data", ConfigFile: "/config/config.toml", UnitStatus: "active", Browser: "browser ready", Warnings: []string{"example"}})
	os.Stdout = stdout
	raw, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	const want = "data: /data\nconfig: /config/config.toml\nunit: active\nbrowser ready\nwarning: example\n"
	if string(raw) != want {
		t.Fatalf("report changed: %q", raw)
	}
	if installAutostartHelp() != "install the systemd user unit" {
		t.Fatal("autostart help changed")
	}
}
