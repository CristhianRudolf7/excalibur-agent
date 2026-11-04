//go:build darwin

package main

import (
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

const darwinInstallPriority = "setpriority + PRIO_DARWIN_BG"

func probeInstallPriority() daemon.EnvReport {
	// Both mechanisms are kernel facilities; PATH tools and Linux cgroups
	// do not determine whether macOS can lower CPU and I/O priority.
	return daemon.EnvReport{
		NiceAvailable: true, IoprioAvailable: true,
		Notes: []string{"priority: " + darwinInstallPriority, "autostart: " + installAutostartHelp()},
	}
}

func launchAgentDescription(manager platform.DarwinServiceManager) string {
	return "launchd LaunchAgent (~/Library/LaunchAgents/" + manager.LaunchAgentLabel() + ".plist)"
}

func installAutostartHelp() string {
	return launchAgentDescription(installService().(platform.DarwinServiceManager))
}

func printInstallReport(rep daemon.InstallReport) {
	fmt.Printf("data: %s\nconfig: %s\nunit: %s\n", rep.DataDir, rep.ConfigFile, rep.UnitStatus)
	fmt.Printf("priority: %s\nautostart: %s\n", darwinInstallPriority, installAutostartHelp())
	fmt.Println(rep.Browser)
	for _, w := range rep.Warnings {
		fmt.Printf("warning: %s\n", w)
	}
}
