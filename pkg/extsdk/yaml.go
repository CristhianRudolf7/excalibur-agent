package extsdk

import (
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// YAML support for excalibur-extension.yaml (Section 14.12). Two
// shapes need normalization beyond plain struct tags: timeouts read as
// duration strings ("90s") or raw nanoseconds, and JSON schemas read
// as YAML maps. The JSON view is unchanged: everything converts
// through it, so manifests validate identically on both sides.

// yamlTool is the on-disk shape of one tool entry.
type yamlTool struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	InputSchema  any      `yaml:"input_schema"`
	OutputSchema any      `yaml:"output_schema,omitempty"`
	Effect       string   `yaml:"effect"`
	Idempotency  string   `yaml:"idempotency"`
	Timeout      any      `yaml:"timeout"`
	OutputBudget int64    `yaml:"output_budget"`
	Platforms    []string `yaml:"platforms,omitempty"`
	Profiles     []string `yaml:"profiles,omitempty"`
	Inverse      string   `yaml:"inverse,omitempty"`
	Probe        string   `yaml:"probe,omitempty"`
	DryRun       string   `yaml:"dry_run,omitempty"`
	Fast         bool     `yaml:"fast,omitempty"`
	Cancel       string   `yaml:"cancel"`
	Group        string   `yaml:"group,omitempty"`
	Tags         []string `yaml:"tags,omitempty"`
	Keywords     []string `yaml:"keywords,omitempty"`
}

// yamlManifest is the on-disk shape of the extension manifest.
type yamlManifest struct {
	ID          string          `yaml:"id"`
	Version     string          `yaml:"version"`
	Contract    string          `yaml:"contract"`
	Kind        string          `yaml:"kind"`
	Transport   string          `yaml:"transport,omitempty"`
	Command     []string        `yaml:"command,omitempty"`
	URL         string          `yaml:"url,omitempty"`
	Tools       []yamlTool      `yaml:"tools"`
	SkillsDir   string          `yaml:"skills,omitempty"`
	App         *AppBinding     `yaml:"app,omitempty"`
	Config      any             `yaml:"config,omitempty"`
	Platforms   []string        `yaml:"platforms,omitempty"`
	Profiles    []string        `yaml:"profiles,omitempty"`
	Resources   *ResourceLimits `yaml:"resources,omitempty"`
	Concurrency int             `yaml:"concurrency,omitempty"`
	Lifecycle   string          `yaml:"lifecycle,omitempty"`
	HealthCheck string          `yaml:"health_check,omitempty"`
}

// UnmarshalManifestYAML parses excalibur-extension.yaml.
func UnmarshalManifestYAML(raw []byte) (Manifest, error) {
	var ym yamlManifest
	if err := yaml.Unmarshal(raw, &ym); err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		ID: ym.ID, Version: ym.Version, Contract: ym.Contract, Kind: ym.Kind,
		Transport: ym.Transport, Command: ym.Command, URL: ym.URL,
		SkillsDir: ym.SkillsDir, App: ym.App,
		Platforms: ym.Platforms, Profiles: ym.Profiles,
		Resources: ym.Resources, Concurrency: ym.Concurrency,
		Lifecycle: ym.Lifecycle, HealthCheck: ym.HealthCheck,
	}
	if ym.Config != nil {
		cfg, err := json.Marshal(ym.Config)
		if err != nil {
			return Manifest{}, fmt.Errorf("extsdk: bad config: %w", err)
		}
		m.Config = cfg
	}
	for _, yt := range ym.Tools {
		dt, err := yamlToolToDefinition(yt)
		if err != nil {
			return Manifest{}, err
		}
		m.Tools = append(m.Tools, ExtensionTool{Definition: dt})
	}
	return m, nil
}

func yamlToolToDefinition(yt yamlTool) (Definition, error) {
	in, err := yamlSchemaToRaw(yt.InputSchema)
	if err != nil {
		return Definition{}, fmt.Errorf("extsdk: tool %q: bad input_schema: %w", yt.Name, err)
	}
	var out json.RawMessage
	if yt.OutputSchema != nil {
		out, err = yamlSchemaToRaw(yt.OutputSchema)
		if err != nil {
			return Definition{}, fmt.Errorf("extsdk: tool %q: bad output_schema: %w", yt.Name, err)
		}
	}
	timeout, err := yamlTimeout(yt.Timeout)
	if err != nil {
		return Definition{}, fmt.Errorf("extsdk: tool %q: bad timeout: %w", yt.Name, err)
	}
	return Definition{
		Name: yt.Name, Description: yt.Description,
		InputSchema: in, OutputSchema: out,
		Effect: yt.Effect, Idempotency: yt.Idempotency,
		Timeout: timeout, OutputBudget: yt.OutputBudget,
		Platforms: yt.Platforms, Profiles: yt.Profiles,
		Inverse: yt.Inverse, Probe: yt.Probe, DryRun: yt.DryRun,
		Fast: yt.Fast, Cancel: yt.Cancel,
		Group: yt.Group, Tags: yt.Tags, Keywords: yt.Keywords,
	}, nil
}

// yamlSchemaToRaw accepts a YAML map (or an inline JSON string) and
// returns the JSON schema bytes.
func yamlSchemaToRaw(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	if s, ok := v.(string); ok {
		s = trimSpace(s)
		if s == "" {
			return json.RawMessage(`{"type":"object"}`), nil
		}
		var probe any
		if err := json.Unmarshal([]byte(s), &probe); err != nil {
			return nil, fmt.Errorf("not inline JSON: %q", s)
		}
		return json.RawMessage(s), nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// yamlTimeout accepts duration strings ("90s") or raw nanoseconds.
func yamlTimeout(v any) (time.Duration, error) {
	switch t := v.(type) {
	case nil:
		return 0, nil
	case string:
		if trimSpace(t) == "" {
			return 0, nil
		}
		return time.ParseDuration(trimSpace(t))
	case int:
		return time.Duration(t), nil
	case int64:
		return time.Duration(t), nil
	case uint64:
		return time.Duration(t), nil
	case float64:
		return time.Duration(t), nil
	default:
		return 0, fmt.Errorf("want duration string or nanoseconds, got %T", v)
	}
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

// MarshalManifestYAML renders a manifest as excalibur-extension.yaml:
// timeouts as duration strings, schemas as YAML maps.
func MarshalManifestYAML(m Manifest) ([]byte, error) {
	ym := yamlManifest{
		ID: m.ID, Version: m.Version, Contract: m.Contract, Kind: m.Kind,
		Transport: m.Transport, Command: m.Command, URL: m.URL,
		SkillsDir: m.SkillsDir, App: m.App,
		Platforms: m.Platforms, Profiles: m.Profiles,
		Resources: m.Resources, Concurrency: m.Concurrency,
		Lifecycle: m.Lifecycle, HealthCheck: m.HealthCheck,
	}
	if len(m.Config) > 0 {
		var cfg any
		if err := json.Unmarshal(m.Config, &cfg); err != nil {
			return nil, fmt.Errorf("extsdk: bad config: %w", err)
		}
		ym.Config = cfg
	}
	for _, t := range m.Tools {
		yt, err := definitionToYAMLTool(t.Definition)
		if err != nil {
			return nil, err
		}
		ym.Tools = append(ym.Tools, yt)
	}
	return yaml.Marshal(ym)
}

func definitionToYAMLTool(d Definition) (yamlTool, error) {
	in, err := rawToYAMLValue(d.InputSchema)
	if err != nil {
		return yamlTool{}, err
	}
	var out any
	if len(d.OutputSchema) > 0 {
		out, err = rawToYAMLValue(d.OutputSchema)
		if err != nil {
			return yamlTool{}, err
		}
	}
	timeout := ""
	if d.Timeout > 0 {
		timeout = d.Timeout.String()
	}
	return yamlTool{
		Name: d.Name, Description: d.Description,
		InputSchema: in, OutputSchema: out,
		Effect: d.Effect, Idempotency: d.Idempotency,
		Timeout: timeout, OutputBudget: d.OutputBudget,
		Platforms: d.Platforms, Profiles: d.Profiles,
		Inverse: d.Inverse, Probe: d.Probe, DryRun: d.DryRun,
		Fast: d.Fast, Cancel: d.Cancel,
		Group: d.Group, Tags: d.Tags, Keywords: d.Keywords,
	}, nil
}

func rawToYAMLValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return map[string]any{"type": "object"}, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}
