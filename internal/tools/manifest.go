package tools

import (
	"encoding/json"
	"fmt"
	"time"
)

// Extension manifest: the single excalibur-extension.yaml used by all
// four extension paths (Section 14.12). Over MCP the Excalibur-specific
// fields travel in _meta under the "excalibur/" key and, when missing,
// are derived from the MCP annotations with the most conservative
// value. YAML parsing lands in H13; the struct carries both tags from
// day zero so manifests written now stay valid.
const (
	// ExtensionContract is the manifest contract version.
	ExtensionContract = "excalibur-ext/1"
)

// Extension kinds (Section 14.12).
const (
	ExtensionMCP     = "mcp"
	ExtensionApp     = "app"
	ExtensionPackage = "package"
	ExtensionBuiltin = "builtin"
)

// Extension lifecycle modes.
const (
	LifecycleLazy     = "lazy"
	LifecycleResident = "resident"
	LifecyclePerCall  = "per_call"
)

// ExtensionTool is one tool entry of a manifest: every field of Section
// 13.1 (see Definition) plus discovery hints for search_tools, which
// live in Definition.Group/Tags/Keywords. The inline tag matters:
// without it yaml.v3 decodes manifest tool entries to zero values.
type ExtensionTool struct {
	Definition `yaml:",inline"`
}

// Manifest is the excalibur-extension.yaml content (Section 14.12).
type Manifest struct {
	ID          string          `json:"id" yaml:"id"`
	Version     string          `json:"version" yaml:"version"`
	Contract    string          `json:"contract" yaml:"contract"`
	Kind        string          `json:"kind" yaml:"kind"`
	Transport   string          `json:"transport,omitempty" yaml:"transport,omitempty"`
	Command     []string        `json:"command,omitempty" yaml:"command,omitempty"`
	URL         string          `json:"url,omitempty" yaml:"url,omitempty"`
	Tools       []ExtensionTool `json:"tools" yaml:"tools"`
	SkillsDir   string          `json:"skills,omitempty" yaml:"skills,omitempty"`
	App         *AppBinding     `json:"app,omitempty" yaml:"app,omitempty"`
	Config      json.RawMessage `json:"config,omitempty" yaml:"config,omitempty"`
	Platforms   []string        `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	Profiles    []string        `json:"profiles,omitempty" yaml:"profiles,omitempty"`
	Resources   *ResourceLimits `json:"resources,omitempty" yaml:"resources,omitempty"`
	Concurrency int             `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Lifecycle   string          `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	HealthCheck string          `json:"health_check,omitempty" yaml:"health_check,omitempty"`
}

// AppBinding describes the app-channel binding (path 2).
type AppBinding struct {
	StateForm string   `json:"state_form" yaml:"state_form"`
	Events    []string `json:"events,omitempty" yaml:"events,omitempty"`
	Scope     string   `json:"scope" yaml:"scope"`
}

// ResourceLimits caps what the extension may consume.
type ResourceLimits struct {
	RAMBytes    int64 `json:"ram_bytes,omitempty" yaml:"ram_bytes,omitempty"`
	Processes   int   `json:"processes,omitempty" yaml:"processes,omitempty"`
	MaxSessions int   `json:"max_sessions,omitempty" yaml:"max_sessions,omitempty"`
	MaxCalls    int   `json:"max_calls,omitempty" yaml:"max_calls,omitempty"`
}

// Validate checks the manifest: schema, supported contract version,
// unique catalog names and declared effects.
func (m Manifest) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("tools: extension manifest needs an id")
	}
	if m.Version == "" {
		return fmt.Errorf("tools: extension %q needs a version", m.ID)
	}
	if m.Contract != ExtensionContract {
		return fmt.Errorf("tools: extension %q uses unsupported contract %q", m.ID, m.Contract)
	}
	switch m.Kind {
	case ExtensionMCP, ExtensionApp, ExtensionPackage, ExtensionBuiltin:
	default:
		return fmt.Errorf("tools: extension %q has unknown kind %q", m.ID, m.Kind)
	}
	switch m.Lifecycle {
	case "", LifecycleLazy, LifecycleResident, LifecyclePerCall:
	default:
		return fmt.Errorf("tools: extension %q has unknown lifecycle %q", m.ID, m.Lifecycle)
	}
	// Route-1 (MCP) manifests may declare no static tools: the server
	// tools/list is the source of truth and binding fills the catalog.
	if len(m.Tools) == 0 && m.Kind != ExtensionMCP {
		return fmt.Errorf("tools: extension %q declares no tools", m.ID)
	}
	seen := map[string]bool{}
	for _, t := range m.Tools {
		if err := t.Definition.Validate(); err != nil {
			return fmt.Errorf("tools: extension %q: %w", m.ID, err)
		}
		if seen[t.Name] {
			return fmt.Errorf("tools: extension %q declares duplicate tool %q", m.ID, t.Name)
		}
		seen[t.Name] = true
	}
	return nil
}

// DefaultTimeout fills zero timeouts with the catalog default.
func DefaultTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return 60 * time.Second
	}
	return d
}
