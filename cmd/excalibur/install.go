// Command excalibur install: Section 25.2 installer over the daemon
// core. This file is a thin layer: it parses flags, wires the real
// platform paths, service manager and environment probe, and prints the
// install report. No agent or provider logic lives here.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

// cmdEnvProber detects interpreters and low-priority mechanisms with
// platform helpers. Missing mechanisms degrade to warnings in the report,
// never to a failure (25.2 step 2).
type cmdEnvProber struct{}

func (cmdEnvProber) Probe() daemon.EnvReport {
	rep := probeInstallPriority()
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
	autostart := fs.Bool("autostart", false, installAutostartHelp())
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
		Service:     installServiceForRelease(),
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
	if *autostart && os.Getenv("EXCALIBUR_SYSTEMD_DIR") == "" {
		if err := startInstalledDaemon(); err != nil {
			return err
		}
	}
	if *asJSON {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
		return nil
	}
	printInstallReport(rep)
	fmt.Println("Open " + installedURL())
	return nil
}

// releaseService preserves the configured unit when Linux has no user bus.
type releaseService struct{ daemon.ServiceUnit }

func (s releaseService) Install() error {
	if err := s.ServiceUnit.Install(); err != nil {
		if runtime.GOOS != "linux" || !strings.Contains(err.Error(), "platform: systemctl") {
			return err
		}
		fmt.Fprintln(os.Stderr, "warning: user service could not start; starting the daemon for this session:", err)
	}
	return nil
}
func installServiceForRelease() daemon.ServiceUnit { return releaseService{installService()} }
