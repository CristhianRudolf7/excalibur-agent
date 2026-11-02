// Command excalibur install: Section 25.2 installer over the daemon
// core. This file is a thin layer: it parses flags, wires the real
// Linux paths, service manager and environment probe, and prints the
// install report. No agent or provider logic lives here.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

// cmdEnvProber detects interpreters and low-priority mechanisms with
// stdlib only. Missing mechanisms degrade to warnings in the report,
// never to a failure (25.2 step 2).
type cmdEnvProber struct{}

func (cmdEnvProber) Probe() daemon.EnvReport {
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
	for _, tool := range []string{"python3", "node", "go", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			rep.Notes = append(rep.Notes, tool+" not on PATH")
		}
	}
	return rep
}

// providersFlag collects repeatable --provider flags.
type providersFlag []string

func (p *providersFlag) String() string     { return strings.Join(*p, ",") }
func (p *providersFlag) Set(v string) error { *p = append(*p, v); return nil }

func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	autostart := fs.Bool("autostart", false, "install the systemd user unit")
	timezone := fs.String("timezone", "", "IANA timezone for the user profile")
	vaultMode := fs.String("vault-mode", "secret_service", "secret_service or passphrase")
	passFile := fs.String("passphrase-file", "", "unattended unlock file (0600, declared explicitly)")
	var providers providersFlag
	fs.Var(&providers, "provider", "configured model provider (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	in := &daemon.Installer{
		BrowserStep: daemon.InstallBrowserDriver,
		Dirs:        installDirs(),
		Service:     installService(),
		Prober:      cmdEnvProber{},
	}
	rep, err := in.Install(daemon.InstallOptions{
		Autostart:      *autostart,
		Timezone:       *timezone,
		Providers:      []string(providers),
		VaultMode:      *vaultMode,
		PassphraseFile: *passFile,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
		return nil
	}
	fmt.Printf("data: %s\nconfig: %s\nunit: %s\n", rep.DataDir, rep.ConfigFile, rep.UnitStatus)
	fmt.Println(rep.Browser)
	for _, w := range rep.Warnings {
		fmt.Printf("warning: %s\n", w)
	}
	return nil
}
