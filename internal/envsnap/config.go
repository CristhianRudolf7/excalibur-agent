package envsnap

import (
	"os"
	"strings"
	"time"
)

// Switches for the ablation protocol: every new mechanism ships with
// a config switch defaulting to ON so it can be measured on and off.
// See docs/development/agent-quality.md for the full switch table.
const (
	// EnvSnapshotSwitch disables the proactive snapshot when set to
	// 0, off or false (default: enabled).
	EnvSnapshotSwitch = "EXCALIBUR_ENV_SNAPSHOT"
	// EnvSnapshotTimeoutSwitch overrides the total collection budget
	// (Go duration string, default 4s).
	EnvSnapshotTimeoutSwitch = "EXCALIBUR_ENV_SNAPSHOT_TIMEOUT"
)

// Config tunes collection. Zero values are filled by Normalize.
type Config struct {
	// ProcRoot and SysRoot override resource files for local simulation.
	ProcRoot string
	SysRoot  string
	// Enabled gates collection and injection (default true).
	Enabled bool
	// Timeout bounds the whole collection pass (default 4s).
	Timeout time.Duration
	// ProbeTimeout bounds one --version probe (default 800ms).
	ProbeTimeout time.Duration
	// MaxTreeEntries caps the workdir listing (default 60).
	MaxTreeEntries int
	// MaxTreeDepth caps tree recursion: 1 lists the top level,
	// 2 adds one entry-count line per subdirectory (default 2).
	MaxTreeDepth int
	// MaxRenderBytes caps the rendered block (default 1800).
	MaxRenderBytes int
	// NetworkProbe enables the TCP reachability probe (default true).
	NetworkProbe bool
	// ProbeHost is dialed as host:port when set; empty skips the
	// dial and reports reachability unknown.
	ProbeHost string
	// Workdir is the directory summarized as the tree root.
	Workdir string
}

// DefaultConfig returns the calibrated defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:        true,
		Timeout:        4 * time.Second,
		ProbeTimeout:   800 * time.Millisecond,
		MaxTreeEntries: 60,
		MaxTreeDepth:   2,
		MaxRenderBytes: 1800,
		NetworkProbe:   true,
	}
}

// Normalize fills zero values with defaults.
func (c Config) Normalize() Config {
	d := DefaultConfig()
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = d.ProbeTimeout
	}
	if c.MaxTreeEntries <= 0 {
		c.MaxTreeEntries = d.MaxTreeEntries
	}
	if c.MaxTreeDepth <= 0 {
		c.MaxTreeDepth = d.MaxTreeDepth
	}
	if c.MaxRenderBytes <= 0 {
		c.MaxRenderBytes = d.MaxRenderBytes
	}
	return c
}

// ConfigFromEnv overlays the ablation switches on base: the mechanism
// is ON unless EXCALIBUR_ENV_SNAPSHOT is set to 0/off/false, and
// EXCALIBUR_ENV_SNAPSHOT_TIMEOUT overrides the collection budget. The
// explicit Enabled=false of a literal Config only survives when the
// switch is unset and the caller bypasses this function; every
// production path goes through here, so the default is ON.
func ConfigFromEnv(base Config) Config {
	base = base.Normalize()
	if v, ok := os.LookupEnv(EnvSnapshotSwitch); ok {
		base.Enabled = !isOff(v)
	} else {
		base.Enabled = true
	}
	if v, ok := os.LookupEnv(EnvSnapshotTimeoutSwitch); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d > 0 {
			base.Timeout = d
		}
	}
	return base
}

func isOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no":
		return true
	default:
		return false
	}
}

// ParseBool reports whether s enables a default-on switch.
func ParseBool(s string, def bool) bool {
	if strings.TrimSpace(s) == "" {
		return def
	}
	if isOff(s) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "on", "true", "yes":
		return true
	default:
		return def
	}
}
