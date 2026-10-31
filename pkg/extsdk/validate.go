package extsdk

import (
	"fmt"
	"strings"
)

// Meta keys carrying the Excalibur execution fields over MCP
// (Section 14.12). Servers write their manifest fields under
// "excalibur/"; the bare "excalibur" key is accepted for robustness.
// Missing fields derive from the MCP annotations with the most
// conservative value (see internal/capabilities.ExcaliburMeta).
const (
	MetaKey     = "excalibur/"
	MetaKeyBare = "excalibur"
)

// ToolMeta holds the Excalibur execution fields of one MCP tool for
// the _meta maps of route-1 extensions.
type ToolMeta struct {
	Effect       string   `json:"effect,omitempty"`
	Idempotency  string   `json:"idempotency,omitempty"`
	Cancel       string   `json:"cancel,omitempty"`
	Fast         bool     `json:"fast,omitempty"`
	Group        string   `json:"group,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
	TimeoutSecs  int64    `json:"timeout_secs,omitempty"`
	OutputBudget int64    `json:"output_budget,omitempty"`
	DryRun       string   `json:"dry_run,omitempty"`
	Inverse      string   `json:"inverse,omitempty"`
	Probe        string   `json:"probe,omitempty"`
}

// MetaMap renders the tool meta as the _meta payload of an MCP tool.
func (m ToolMeta) MetaMap() map[string]any {
	out := map[string]any{}
	if m.Effect != "" {
		out["effect"] = m.Effect
	}
	if m.Idempotency != "" {
		out["idempotency"] = m.Idempotency
	}
	if m.Cancel != "" {
		out["cancel"] = m.Cancel
	}
	if m.Fast {
		out["fast"] = true
	}
	if m.Group != "" {
		out["group"] = m.Group
	}
	if len(m.Tags) > 0 {
		out["tags"] = m.Tags
	}
	if len(m.Keywords) > 0 {
		out["keywords"] = m.Keywords
	}
	if m.TimeoutSecs > 0 {
		out["timeout_secs"] = m.TimeoutSecs
	}
	if m.OutputBudget > 0 {
		out["output_budget"] = m.OutputBudget
	}
	if m.DryRun != "" {
		out["dry_run"] = m.DryRun
	}
	if m.Inverse != "" {
		out["inverse"] = m.Inverse
	}
	if m.Probe != "" {
		out["probe"] = m.Probe
	}
	return out
}

// Validate checks the manifest: identity, supported contract version,
// known kind and lifecycle, and every tool entry with the Section
// 13.1 execution fields. It mirrors internal/tools.Manifest.Validate
// so manifests written against either validate on both sides.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("extsdk: extension manifest needs an id")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("extsdk: extension %q needs a version", m.ID)
	}
	if m.Contract != ExtensionContract {
		return fmt.Errorf("extsdk: extension %q uses unsupported contract %q", m.ID, m.Contract)
	}
	switch m.Kind {
	case ExtensionMCP, ExtensionApp, ExtensionPackage, ExtensionBuiltin:
	default:
		return fmt.Errorf("extsdk: extension %q has unknown kind %q", m.ID, m.Kind)
	}
	switch m.Lifecycle {
	case "", LifecycleLazy, LifecycleResident, LifecyclePerCall:
	default:
		return fmt.Errorf("extsdk: extension %q has unknown lifecycle %q", m.ID, m.Lifecycle)
	}
	// Route-1 (MCP) manifests may declare no static tools: the server
	// tools/list is the source of truth and binding fills the catalog.
	if len(m.Tools) == 0 && m.Kind != ExtensionMCP {
		return fmt.Errorf("extsdk: extension %q declares no tools", m.ID)
	}
	seen := map[string]bool{}
	for _, t := range m.Tools {
		if err := t.Definition.Validate(); err != nil {
			return fmt.Errorf("extsdk: extension %q: %w", m.ID, err)
		}
		if seen[t.Name] {
			return fmt.Errorf("extsdk: extension %q declares duplicate tool %q", m.ID, t.Name)
		}
		seen[t.Name] = true
	}
	return nil
}

// Validate checks one tool definition: name shape, description,
// schemas and the declared execution classes.
func (d Definition) Validate() error {
	if len(d.Name) == 0 || len(d.Name) > 64 {
		return fmt.Errorf("extsdk: bad tool name %q: want 1..64 chars", d.Name)
	}
	for _, r := range d.Name {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'
		if !ok {
			return fmt.Errorf("extsdk: bad tool name %q: want snake_case", d.Name)
		}
	}
	if d.Name[0] < 'a' || d.Name[0] > 'z' {
		return fmt.Errorf("extsdk: bad tool name %q: must start with a letter", d.Name)
	}
	if strings.TrimSpace(d.Description) == "" {
		return fmt.Errorf("extsdk: tool %q needs a description", d.Name)
	}
	if len(d.InputSchema) == 0 {
		return fmt.Errorf("extsdk: tool %q needs an input schema", d.Name)
	}
	switch d.Effect {
	case EffectReadOnly, EffectWriteLocal, EffectExec, EffectWriteExternal, EffectCommunication:
	default:
		return fmt.Errorf("extsdk: tool %q has unknown effect %q", d.Name, d.Effect)
	}
	switch d.Idempotency {
	case IdemPure, IdemIdempotent, IdemKeyed, IdemNonIdempotent:
	default:
		return fmt.Errorf("extsdk: tool %q has unknown idempotency %q", d.Name, d.Idempotency)
	}
	switch d.Cancel {
	case CancelImmediate, CancelGraceful, CancelAfterCompletion, CancelCooperative:
	default:
		return fmt.Errorf("extsdk: tool %q has unknown cancel %q", d.Name, d.Cancel)
	}
	return nil
}
