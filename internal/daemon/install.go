// Installation (Section 25.2).
//
// `excalibur install` creates the user data directory (XDG via
// internal/platform Paths), the layered TOML configuration (defaults
// < system < user < project < scope), probes the environment (13.5)
// and the available low-priority mechanisms (cgroup v2 delegation or
// nice+ioprio), asks the timezone and the model providers, creates the
// vault (Secret Service or passphrase, 20.1) holding the API keys or
// keeps them in environment variables, and optionally wires autostart
// with supervision: a systemd user unit with Restart=on-failure,
// growing RestartSec and WatchdogSec (25.2). The supervisor only
// restarts; crash-loop protection is safe mode (17.3).
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Dirs resolves the user directories for one install. It mirrors the
// internal/platform Paths interface without importing it, so install
// logic stays testable with temp dirs.
type Dirs interface {
	DataDir() (string, error)
	ConfigDir() (string, error)
	CacheDir() (string, error)
	RuntimeDir() (string, error)
}

// ServiceUnit manages the autostart unit. It mirrors the
// internal/platform ServiceManager interface.
type ServiceUnit interface {
	Install() error
	Status() (string, error)
}

// EnvReport records what the environment probe found (13.5 and the
// priority mechanisms of 3.2): which interpreters exist and whether
// low-priority execution can use delegated cgroup v2 or falls back to
// nice+ioprio. Missing mechanisms are reported, never fatal.
type EnvReport struct {
	CgroupDelegated bool
	NiceAvailable   bool
	IoprioAvailable bool
	Notes           []string
}

// EnvProber detects interpreters and priority mechanisms.
type EnvProber interface {
	Probe() EnvReport
}

// InstallOptions selects what install does.
type InstallOptions struct {
	// Autostart writes and enables the systemd user unit.
	Autostart bool
	// Timezone is the IANA zone stored in the user profile (21.1).
	Timezone string
	// Providers lists configured model providers (19.2); keys go to
	// the vault or stay in the environment, never in the config.
	Providers []string
	// VaultMode is "secret_service" or "passphrase" (20.1).
	VaultMode string
	// PassphraseFile, when set, enables unattended unlock (0600);
	// declared explicitly in the configuration (20.1).
	PassphraseFile string
}

// InstallReport is the evidence kept for 25.5: directories,
// configuration layers, mechanism report, unit state.
type InstallReport struct {
	Browser    string
	DataDir    string
	ConfigDir  string
	CacheDir   string
	RuntimeDir string
	ConfigFile string
	Env        EnvReport
	UnitStatus string
	Warnings   []string
}

// Installer wires an install from injectable parts.
type Installer struct {
	// BrowserStep installs the driver in production; injectable to keep tests offline.
	BrowserStep func() string
	Dirs        Dirs
	Service     ServiceUnit
	Prober      EnvProber
	Now         func() time.Time
}

// Install performs the five steps of 25.2 and returns the report.
func (in *Installer) Install(opts InstallOptions) (InstallReport, error) {
	var rep InstallReport
	if in.Dirs == nil {
		return rep, fmt.Errorf("daemon: install needs directories")
	}
	data, err := in.Dirs.DataDir()
	if err != nil {
		return rep, fmt.Errorf("daemon: install data dir: %w", err)
	}
	cfg, err := in.Dirs.ConfigDir()
	if err != nil {
		return rep, fmt.Errorf("daemon: install config dir: %w", err)
	}
	cache, err := in.Dirs.CacheDir()
	if err != nil {
		return rep, fmt.Errorf("daemon: install cache dir: %w", err)
	}
	runtime, err := in.Dirs.RuntimeDir()
	if err != nil {
		return rep, fmt.Errorf("daemon: install runtime dir: %w", err)
	}
	for _, d := range []string{data, cfg, cache, runtime, filepath.Join(data, "backups")} {
		if merr := os.MkdirAll(d, 0o700); merr != nil {
			return rep, fmt.Errorf("daemon: install mkdir %q: %w", d, merr)
		}
	}
	rep.DataDir, rep.ConfigDir, rep.CacheDir, rep.RuntimeDir = data, cfg, cache, runtime
	cfgFile := filepath.Join(cfg, "config.toml")
	if werr := writeInitialConfig(cfgFile, opts); werr != nil {
		return rep, werr
	}
	rep.ConfigFile = cfgFile
	if in.Prober != nil {
		rep.Env = in.Prober.Probe()
	}
	if rep.Env.Notes == nil {
		rep.Env.Notes = []string{}
	}
	if !rep.Env.CgroupDelegated && !rep.Env.NiceAvailable {
		rep.Warnings = append(rep.Warnings, "no low-priority mechanism available: tool processes run at normal priority")
	}
	if opts.VaultMode == "passphrase" && opts.PassphraseFile == "" {
		rep.Warnings = append(rep.Warnings, "vault unlocks with a passphrase: without it and without a backup the secrets cannot be recovered (20.1)")
	}
	if opts.Autostart {
		if in.Service == nil {
			return rep, fmt.Errorf("daemon: autostart needs a service manager")
		}
		if serr := in.Service.Install(); serr != nil {
			return rep, fmt.Errorf("daemon: install service unit: %w", serr)
		}
		st, serr := in.Service.Status()
		if serr != nil {
			return rep, fmt.Errorf("daemon: install unit status: %w", serr)
		}
		rep.UnitStatus = st
	} else {
		rep.UnitStatus = "not installed"
	}
	if in.BrowserStep != nil {
		rep.Browser = in.BrowserStep()
	}
	return rep, nil
}

// initialConfig renders the layered user configuration. Provider keys
// never land here: they live in the vault or the environment (20.1).
func initialConfig(opts InstallOptions) string {
	tz := opts.Timezone
	if tz == "" {
		tz = "local"
	}
	out := "# Excalibur user configuration (layers: defaults < system < user < project < scope).\n"
	out += "# Provider API keys are NOT stored here: they live in the vault or the environment.\n"
	out += "[user]\n"
	out += "timezone = \"" + tz + "\"\n"
	out += "vault_mode = \"" + opts.VaultMode + "\"\n"
	if opts.PassphraseFile != "" {
		out += "passphrase_file = \"" + opts.PassphraseFile + "\"\n"
	}
	if len(opts.Providers) > 0 {
		out += "\n[providers]\n"
		for _, p := range opts.Providers {
			out += p + " = { key = \"vault\" }\n"
		}
	}
	return out
}

// writeInitialConfig writes the user config without overwriting an
// existing one: reinstalls keep user edits.
func writeInitialConfig(path string, opts InstallOptions) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("daemon: install config dir: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(initialConfig(opts)), 0o600); err != nil {
		return fmt.Errorf("daemon: install write config: %w", err)
	}
	return nil
}
