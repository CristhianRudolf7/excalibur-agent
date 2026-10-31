package extsdk

import "encoding/json"

// Extension manifest types mirror internal/tools.Manifest field for
// field so a manifest validates identically on both sides. The contract
// version is shared: extensions target one contract string.
const (
	// ExtensionContract is the manifest contract version.
	ExtensionContract = "excalibur-ext/1"

	ExtensionMCP     = "mcp"
	ExtensionApp     = "app"
	ExtensionPackage = "package"
	ExtensionBuiltin = "builtin"

	LifecycleLazy     = "lazy"
	LifecycleResident = "resident"
	LifecyclePerCall  = "per_call"
)

// ExtensionTool is one tool entry of a manifest. The inline tag
// matters: without it yaml.v3 decodes manifest tool entries to zero
// values.
type ExtensionTool struct {
	Definition `yaml:",inline"`
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

// Manifest is the excalibur-extension.yaml content (Section 14.12).
// Keep in sync with internal/tools.Manifest; the parity test enforces
// JSON convertibility both ways.
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
