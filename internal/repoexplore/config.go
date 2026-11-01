package repoexplore

import (
	"os"
	"strings"
	"time"
)

// Switches for the ablation protocol: the exploration phase ships
// with a config switch defaulting to OFF until measured, so it can
// be compared on and off. See docs/development/agent-quality.md.
const (
	// ExploreSwitch enables the exploration phase when set to
	// 1/on/true/yes (default: off).
	ExploreSwitch = "EXCALIBUR_EXPLORE"
	// ExploreTimeoutSwitch overrides the collection budget
	// (Go duration string, default 3s).
	ExploreTimeoutSwitch = "EXCALIBUR_EXPLORE_TIMEOUT"
)

// Config tunes collection. Zero values are filled by Normalize.
type Config struct {
	// Enabled gates collection and injection (default false).
	Enabled bool
	// Timeout bounds the whole collection pass (default 3s).
	Timeout time.Duration
	// MaxFiles caps filesystem entries visited (default 120).
	MaxFiles int
	// MaxDepth caps walk depth below the workdir (default 3).
	MaxDepth int
	// MaxBriefLines caps rendered brief lines (default 60).
	MaxBriefLines int
	// MaxFileReadBytes caps one file read (default 8192).
	MaxFileReadBytes int
	// Workdir is the directory summarized as the tree root.
	Workdir string
}

// DefaultConfig returns the calibrated defaults: disabled until
// measured, 3s budget, 120 files, depth 3, 60-line brief.
func DefaultConfig() Config {
	return Config{
		Enabled:          false,
		Timeout:          3 * time.Second,
		MaxFiles:         120,
		MaxDepth:         3,
		MaxBriefLines:    60,
		MaxFileReadBytes: 8192,
	}
}

// Normalize fills zero values with defaults. Enabled is left as-is:
// the default-off policy lives in ConfigFromEnv.
func (c Config) Normalize() Config {
	d := DefaultConfig()
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	if c.MaxFiles <= 0 {
		c.MaxFiles = d.MaxFiles
	}
	if c.MaxDepth <= 0 {
		c.MaxDepth = d.MaxDepth
	}
	if c.MaxBriefLines <= 0 {
		c.MaxBriefLines = d.MaxBriefLines
	}
	if c.MaxFileReadBytes <= 0 {
		c.MaxFileReadBytes = d.MaxFileReadBytes
	}
	return c
}

// ConfigFromEnv overlays the ablation switch on base: the mechanism
// is OFF unless EXCALIBUR_EXPLORE is set to 1/on/true/yes, and
// EXCALIBUR_EXPLORE_TIMEOUT overrides the collection budget. An
// explicit Enabled=true in a literal Config only survives when the
// switch is unset; every production path goes through here, so the
// default is off until measured.
func ConfigFromEnv(base Config) Config {
	base = base.Normalize()
	if v, ok := os.LookupEnv(ExploreSwitch); ok && strings.TrimSpace(v) != "" {
		base.Enabled = isOn(v)
	} else if !ok && !base.Enabled {
		base.Enabled = false
	}
	if v, ok := os.LookupEnv(ExploreTimeoutSwitch); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d > 0 {
			base.Timeout = d
		}
	}
	return base
}

func isOn(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "on", "true", "yes":
		return true
	default:
		return false
	}
}

// ParseBool reports whether s enables a default-off switch.
func ParseBool(s string, def bool) bool {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return isOn(s)
}
