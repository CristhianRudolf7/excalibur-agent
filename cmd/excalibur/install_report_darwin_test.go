//go:build darwin

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestDarwinInstallProbeUsesKernelPriority(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	rep := (cmdEnvProber{}).Probe()
	if !rep.NiceAvailable || !rep.IoprioAvailable || rep.CgroupDelegated {
		t.Fatalf("priority report: %+v", rep)
	}
	want := []string{
		"priority: setpriority + PRIO_DARWIN_BG",
		"autostart: launchd LaunchAgent (~/Library/LaunchAgents/com.excalibur.daemon.plist)",
		"python3 not on PATH", "node not on PATH", "go not on PATH", "git not on PATH",
	}
	if !reflect.DeepEqual(rep.Notes, want) {
		t.Fatalf("notes: %v", rep.Notes)
	}
	if strings.Contains(strings.Join(rep.Notes, " "), "cgroup") || strings.Contains(strings.Join(rep.Notes, " "), "ionice") {
		t.Fatal("Linux mechanism reported on Darwin")
	}
}

func TestDarwinInstallLaunchAgentLabel(t *testing.T) {
	for _, label := range []string{"", "com.example.excalibur"} {
		manager := platform.DarwinServiceManager{Label: label}
		want := "launchd LaunchAgent (~/Library/LaunchAgents/" + manager.LaunchAgentLabel() + ".plist)"
		if got := launchAgentDescription(manager); got != want {
			t.Fatalf("description = %q, want %q", got, want)
		}
	}
}
