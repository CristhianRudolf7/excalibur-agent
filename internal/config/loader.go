package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Layered configuration of Section 25.2 point 1: values come from
// defaults < system < user < project < scope, all in TOML, with
// environment variables and explicit flag overrides on top. Later
// layers replace whole values (scalars) or merge key by key (tables).

// LoadOptions tunes Load. Zero LoadOptions reads the default
// locations with environment overlay enabled.
type LoadOptions struct {
	// SystemPath overrides /etc/excalibur/config.toml. Empty means
	// the default; set SkipFiles to ignore files entirely.
	SystemPath string
	// UserPath overrides the XDG user file.
	UserPath string
	// StartDir anchors project discovery (default: cwd). The
	// project file is <root>/.excalibur/config.toml where root is
	// the nearest ancestor containing .excalibur.
	StartDir string
	// ProjectFile pins the project layer explicitly, skipping
	// discovery.
	ProjectFile string
	// ScopeFile is the per-scope overlay (conversation or task).
	ScopeFile string
	// SkipFiles ignores every file layer (tests, eval profile).
	SkipFiles bool
	// NoEnv disables the environment layer.
	NoEnv bool
	// EnvPrefix selects environment variables (default EXCALIBUR_).
	// Empty disables the environment layer.
	EnvPrefix string
	// Env overrides os.Environ (tests). Nil reads the process env.
	Env []string
	// Overrides are flag values as dotted paths ("porter.read_pool_size")
	// with the highest precedence.
	Overrides map[string]any
}

// SystemDefaultPath is /etc/excalibur/config.toml.
const SystemDefaultPath = "/etc/excalibur/config.toml"

// UserDefaultPath resolves the XDG user file.
func UserDefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "excalibur", "config.toml")
}

// FindProjectFile walks up from startDir for .excalibur/config.toml.
// It returns "" when no project root contains the start.
func FindProjectFile(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, ".excalibur", "config.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// DefaultPaths reports the system and user layer paths.
func DefaultPaths() (system, user string) {
	return SystemDefaultPath, UserDefaultPath()
}

// Load merges every layer and validates the result.
func Load(opts LoadOptions) (Config, error) {
	merged := map[string]any{}
	if !opts.SkipFiles {
		layers := []string{opts.SystemPath, opts.UserPath}
		if opts.SystemPath == "" {
			layers[0] = SystemDefaultPath
		}
		if opts.UserPath == "" {
			layers[1] = UserDefaultPath()
		}
		project := opts.ProjectFile
		if project == "" {
			start := opts.StartDir
			if start == "" {
				var err error
				start, err = os.Getwd()
				if err != nil {
					return Config{}, err
				}
			}
			found, err := FindProjectFile(start)
			if err != nil {
				return Config{}, err
			}
			project = found
		}
		layers = append(layers, project, opts.ScopeFile)
		for _, path := range layers {
			if path == "" {
				continue
			}
			if err := mergeFile(merged, path); err != nil {
				return Config{}, err
			}
		}
	}
	applyEnv(merged, opts.envPrefix(), opts.envList(), opts.NoEnv)
	applyOverrides(merged, opts.Overrides)
	applyContextCapAlias(merged, opts.envList(), opts.NoEnv)
	cfg, err := decodeMerged(merged)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyContextCapAlias folds the single-segment EXCALIBUR_CONTEXT_CAP
// override into the merged map. The generic applyEnv skips
// single-segment names (they collide with section tables), so the
// documented cap alias needs explicit handling with the highest
// environment precedence.
func applyContextCapAlias(merged map[string]any, env []string, disabled bool) {
	if disabled {
		return
	}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if name != "EXCALIBUR_CONTEXT_CAP" {
			continue
		}
		value = strings.TrimSpace(value)
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			continue
		}
		setPath(merged, []string{"context", "compaction_ceiling_tokens"}, int64(n))
	}
}

func (o LoadOptions) envPrefix() string {
	if o.EnvPrefix == "" {
		return "EXCALIBUR_"
	}
	return o.EnvPrefix
}

func (o LoadOptions) envList() []string {
	if o.Env != nil {
		return o.Env
	}
	return os.Environ()
}

// mergeFile decodes one TOML layer into merged, skipping missing
// files (a fresh install has no layers yet).
func mergeFile(merged map[string]any, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	var layer map[string]any
	if err := toml.Unmarshal(data, &layer); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	deepMerge(merged, layer)
	return nil
}

// deepMerge overlays src onto dst key by key; tables merge, scalars
// and arrays replace.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if existing, ok := dst[k].(map[string]any); ok {
				deepMerge(existing, sub)
				continue
			}
		}
		dst[k] = v
	}
}

// applyEnv folds EXCALIBUR_SECTION__FIELD variables into merged.
// Segments match the snake_case TOML keys case-insensitively.
// Single-segment names (EXCALIBUR_PROVIDER, EXCALIBUR_MODEL,
// EXCALIBUR_API_KEY, ...) are not config keys: every config key
// lives under a section table, and a top-level string would collide
// with the same-named tables. Those aliases resolve explicitly in
// the model wiring (internal/models), so the loader skips them.
func applyEnv(merged map[string]any, prefix string, env []string, disabled bool) {
	if disabled {
		return
	}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" {
			continue
		}
		parts := strings.Split(strings.ToLower(rest), "__")
		if len(parts) < 2 {
			continue
		}
		setPath(merged, parts, coerceEnv(parts, value, lookupPath(merged, parts)))
	}
}

// applyOverrides folds dotted flag paths into merged.
func applyOverrides(merged map[string]any, overrides map[string]any) {
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts := strings.Split(strings.ToLower(k), ".")
		setPath(merged, parts, overrides[k])
	}
}

// setPath creates nested tables along parts and sets the leaf.
func setPath(merged map[string]any, parts []string, value any) {
	node := merged
	for _, p := range parts[:len(parts)-1] {
		sub, ok := node[p].(map[string]any)
		if !ok {
			sub = map[string]any{}
			node[p] = sub
		}
		node = sub
	}
	node[parts[len(parts)-1]] = value
}

// lookupPath reads the current value at parts, if any.
func lookupPath(merged map[string]any, parts []string) any {
	var node any = merged
	for _, p := range parts {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node, ok = m[p]
		if !ok {
			return nil
		}
	}
	return node
}

// coerceEnv converts an environment string to the shape the target
// expects: durations ("5s") on duration paths, numbers and bools
// where they parse, strings otherwise.
func coerceEnv(parts []string, value string, current any) any {
	dotted := strings.Join(parts, ".")
	if isDurationPath(dotted) {
		if d, err := time.ParseDuration(value); err == nil {
			return int64(d)
		}
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
		return value
	}
	switch current.(type) {
	case bool:
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	case int64:
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
		if d, err := time.ParseDuration(value); err == nil {
			return int64(d)
		}
	case float64:
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	default:
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return value
}

// durationPaths are the dotted TOML paths holding time.Duration.
var durationPaths = map[string]bool{
	"master.regen_cancel_window":       true,
	"master.notice_group_window":       true,
	"engine.heartbeat_timeout":         true,
	"porter.group_commit_wait":         true,
	"porter.busy_timeout":              true,
	"provider.breaker_open_for":        true,
	"tools.default_timeout":            true,
	"notifications.user_active_window": true,
	"vault.passphrase_lock_after":      true,
	"resources.resume_stable_for":      true,
	"resources.safe_mode_window":       true,
	"resources.safe_mode_cooldown":     true,
}

func isDurationPath(dotted string) bool { return durationPaths[dotted] }

// decodeMerged renders the merged map back to TOML (durations as
// nanosecond integers) and decodes it over the defaults, so absent
// keys keep Appendix B values.
func decodeMerged(merged map[string]any) (Config, error) {
	cfg := Default()
	normalizeDurations(merged, "")
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(merged); err != nil {
		return Config{}, fmt.Errorf("config: encode merged: %w", err)
	}
	if _, err := toml.Decode(buf.String(), &cfg); err != nil {
		return Config{}, fmt.Errorf("config: decode merged: %w", err)
	}
	return cfg, nil
}

// normalizeDurations rewrites duration strings ("5s", "2m") to
// integer nanoseconds in place, following durationPaths.
func normalizeDurations(node map[string]any, prefix string) {
	for k, v := range node {
		dotted := k
		if prefix != "" {
			dotted = prefix + "." + k
		}
		switch sub := v.(type) {
		case map[string]any:
			normalizeDurations(sub, dotted)
		case string:
			if isDurationPath(dotted) {
				if d, err := time.ParseDuration(sub); err == nil {
					node[k] = int64(d)
				}
			}
		}
	}
}

// WriteFile writes cfg as TOML for install (Section 25.2).
func WriteFile(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}
