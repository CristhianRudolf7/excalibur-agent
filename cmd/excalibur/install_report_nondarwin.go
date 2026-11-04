//go:build !darwin

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

func probeInstallPriority() daemon.EnvReport {
	rep := daemon.EnvReport{}
	if _, err := exec.LookPath("nice"); err == nil {
		rep.NiceAvailable = true
	}
	if _, err := exec.LookPath("ionice"); err == nil {
		rep.IoprioAvailable = true
	}
	// Delegated cgroup v2: the user manager owns our subtree when we
	// can create a child group.
	if f, err := os.OpenFile("/sys/fs/cgroup/excalibur-probe", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
		_ = f.Close()
		_ = os.Remove("/sys/fs/cgroup/excalibur-probe")
		rep.CgroupDelegated = true
	} else {
		rep.Notes = append(rep.Notes, "cgroup v2 not delegated: tool processes use nice/ioprio fallback")
	}
	return rep
}

func installAutostartHelp() string { return "install the systemd user unit" }

func printInstallReport(rep daemon.InstallReport) {
	fmt.Printf("data: %s\nconfig: %s\nunit: %s\n", rep.DataDir, rep.ConfigFile, rep.UnitStatus)
	fmt.Println(rep.Browser)
	for _, w := range rep.Warnings {
		fmt.Printf("warning: %s\n", w)
	}
}
