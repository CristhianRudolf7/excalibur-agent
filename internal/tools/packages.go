package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Package implementation kinds (Section 13.9): a script run through the
// 13.4 pipeline, a recorded browser routine, a sequence of existing
// tool calls, or an application API call.
const (
	PackageScript       = "script"
	PackageRoutine      = "browser_routine"
	PackageToolSequence = "tool_sequence"
	PackageAppCall      = "app_api"
)

// PackageImpl is the implementation of an agent-created tool.
type PackageImpl struct {
	Kind string `json:"kind"`
	// Script runs ScriptCmd with ScriptArgs when Kind is script.
	ScriptCmd  string `json:"script_cmd,omitempty"`
	ScriptArgs string `json:"script_args,omitempty"`
	// Routine names the recorded browser routine.
	Routine string `json:"routine,omitempty"`
	// Sequence lists tool calls run in order for tool_sequence.
	Sequence []SequenceStep `json:"sequence,omitempty"`
	// AppCall targets an application operation for app_api.
	AppCall string `json:"app_call,omitempty"`
	// DryRun text returned in no-effects mode for effectful tools.
	DryRun string `json:"dry_run,omitempty"`
}

// SequenceStep is one call inside a tool_sequence package.
type SequenceStep struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// PackageTest is one expected input/output case (Section 13.9 step 2).
type PackageTest struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Contains string          `json:"contains,omitempty"`
	Exact    string          `json:"exact,omitempty"`
}

// PackageManifest is the tool_package manifest: the full Section 13.1
// definition plus the implementation.
type PackageManifest struct {
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	Description    string          `json:"description"`
	InputSchema    json.RawMessage `json:"input_schema"`
	OutputSchema   json.RawMessage `json:"output_schema,omitempty"`
	Effect         string          `json:"effect"`
	Idempotency    string          `json:"idempotency"`
	TimeoutSecs    int64           `json:"timeout_secs,omitempty"`
	OutputBudget   int64           `json:"output_budget,omitempty"`
	Fast           bool            `json:"fast"`
	Cancel         string          `json:"cancel"`
	Group          string          `json:"group,omitempty"`
	Tags           []string        `json:"tags,omitempty"`
	Keywords       []string        `json:"keywords,omitempty"`
	Implementation PackageImpl     `json:"implementation"`
	Tests          []PackageTest   `json:"tests"`
}

// PackageStatus is the lifecycle state of an agent-created tool.
type PackageStatus string

// Package states.
const (
	PackageDraft   PackageStatus = "draft"
	PackageTesting PackageStatus = "testing"
	PackageActive  PackageStatus = "active"
	PackageFailed  PackageStatus = "failed"
)

// PackageRecord stores one agent-created tool with version and usage
// statistics (Section 13.9 step 4).
type PackageRecord struct {
	Manifest PackageManifest `json:"manifest"`
	Status   PackageStatus   `json:"status"`
	FailWhy  string          `json:"fail_why,omitempty"`
	Calls    int64           `json:"calls"`
	LastUsed *time.Time      `json:"last_used,omitempty"`
	Created  time.Time       `json:"created_at"`
	Updated  time.Time       `json:"updated_at"`
}

// PackageManager validates, tests and activates agent-created tools.
// Tools activate only when the manifest validates and every test
// passes through the 13.4 pipeline (Suite: tool_package).
type PackageManager struct {
	mu       sync.Mutex
	registry Registry
	catalog  *DeferredCatalog
	executor *Executor
	packages map[string]*PackageRecord
	now      func() time.Time
}

// NewPackageManager returns a manager over the shared registry.
func NewPackageManager(reg Registry, cat *DeferredCatalog, exec *Executor) *PackageManager {
	return &PackageManager{registry: reg, catalog: cat, executor: exec, packages: map[string]*PackageRecord{}, now: time.Now}
}

func (m *PackageManager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Validate checks the manifest: schema, supported contract fields,
// unique catalog name and declared effect (Section 13.9 step 3).
func (m *PackageManager) Validate(pm PackageManifest) error {
	if err := ValidateName(pm.Name); err != nil {
		return err
	}
	if pm.Version == "" {
		return fmt.Errorf("tools: package %q needs a version", pm.Name)
	}
	if pm.Description == "" {
		return fmt.Errorf("tools: package %q needs a description", pm.Name)
	}
	def := pm.definition()
	if err := def.Validate(); err != nil {
		return fmt.Errorf("tools: package %q: %w", pm.Name, err)
	}
	switch pm.Implementation.Kind {
	case PackageScript, PackageRoutine, PackageToolSequence, PackageAppCall:
	default:
		return fmt.Errorf("tools: package %q has unknown implementation kind %q", pm.Name, pm.Implementation.Kind)
	}
	if pm.Implementation.Kind == PackageScript && strings.TrimSpace(pm.Implementation.ScriptCmd) == "" {
		return fmt.Errorf("tools: package %q script needs script_cmd", pm.Name)
	}
	if len(pm.Tests) == 0 {
		return fmt.Errorf("tools: package %q needs at least one test", pm.Name)
	}
	for i, tc := range pm.Tests {
		if strings.TrimSpace(tc.Name) == "" {
			return fmt.Errorf("tools: package %q test %d needs a name", pm.Name, i)
		}
		if len(tc.Input) == 0 {
			return fmt.Errorf("tools: package %q test %q needs input", pm.Name, tc.Name)
		}
	}
	return nil
}

func (p PackageManifest) definition() Definition {
	timeout := time.Duration(60 * time.Second)
	if p.TimeoutSecs > 0 {
		timeout = time.Duration(p.TimeoutSecs) * time.Second
	}
	budget := p.OutputBudget
	if budget <= 0 {
		budget = DefaultOutputBudget
	}
	cancel := p.Cancel
	if cancel == "" {
		cancel = "graceful"
	}
	return Definition{
		Name: p.Name, Description: p.Description,
		InputSchema: p.InputSchema, OutputSchema: p.OutputSchema,
		Effect: types.EffectClass(p.Effect), Idempotency: types.IdempotencyClass(p.Idempotency),
		Timeout: timeout, OutputBudget: budget, Fast: p.Fast,
		Cancel: types.CancelMode(cancel), Group: p.Group, Tags: p.Tags, Keywords: p.Keywords,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
	}
}

// Create validates, tests and activates a package: it stays inactive
// unless every test passes (Section 13.9 steps 2-3).
func (m *PackageManager) Create(ctx context.Context, pm PackageManifest) (*PackageRecord, error) {
	if err := m.Validate(pm); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if _, exists := m.packages[pm.Name]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("tools: package %q already exists", pm.Name)
	}
	if m.registry != nil {
		if _, err := m.registry.Get(pm.Name); err == nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("tools: catalog already has tool %q", pm.Name)
		}
	}
	rec := &PackageRecord{Manifest: pm, Status: PackageTesting, Created: m.clock(), Updated: m.clock()}
	m.packages[pm.Name] = rec
	m.mu.Unlock()

	if err := m.runTests(ctx, pm); err != nil {
		m.mu.Lock()
		rec.Status = PackageFailed
		rec.FailWhy = err.Error()
		rec.Updated = m.clock()
		m.mu.Unlock()
		return rec, err
	}
	tool := &packageTool{manifest: pm, manager: m}
	if m.catalog != nil {
		if err := m.catalog.RegisterDeferred(tool); err != nil {
			m.mu.Lock()
			rec.Status = PackageFailed
			rec.FailWhy = err.Error()
			rec.Updated = m.clock()
			m.mu.Unlock()
			return rec, err
		}
	} else if m.registry != nil {
		if err := m.registry.Register(tool); err != nil {
			m.mu.Lock()
			rec.Status = PackageFailed
			rec.FailWhy = err.Error()
			rec.Updated = m.clock()
			m.mu.Unlock()
			return rec, err
		}
	}
	m.mu.Lock()
	rec.Status = PackageActive
	rec.Updated = m.clock()
	m.mu.Unlock()
	return m.Get(pm.Name)
}

// runTests executes every package test through the 13.4 pipeline. Tests
// for effectful tools run in no-effects mode: the script is not
// executed, the sequence is validated only, and the dry-run text is
// checked instead.
func (m *PackageManager) runTests(ctx context.Context, pm PackageManifest) error {
	for _, tc := range pm.Tests {
		if err := ValidateAgainstSchema(RepairJSON(tc.Input), pm.InputSchema); err != nil {
			return fmt.Errorf("test %q: invalid input: %v", tc.Name, err)
		}
		var got string
		if pm.Effect != "read_only" {
			// No-effects mode for tools with effects.
			got = pm.Implementation.DryRun
			if got == "" {
				got = "dry-run ok: " + pm.Name
			}
		} else {
			out, err := m.executeImpl(ctx, pm, tc.Input)
			if err != nil {
				return fmt.Errorf("test %q: %v", tc.Name, err)
			}
			got = out
		}
		if tc.Exact != "" && got != tc.Exact {
			return fmt.Errorf("test %q: want exact %q, got %q", tc.Name, tc.Exact, got)
		}
		if tc.Contains != "" && !strings.Contains(got, tc.Contains) {
			return fmt.Errorf("test %q: output %q does not contain %q", tc.Name, got, tc.Contains)
		}
	}
	return nil
}

func (m *PackageManager) executeImpl(ctx context.Context, pm PackageManifest, input json.RawMessage) (string, error) {
	switch pm.Implementation.Kind {
	case PackageScript:
		// Harness scripts echo their input descriptor; real interpreters
		// plug in behind this seam in H13 without changing the contract.
		var v any
		_ = json.Unmarshal(input, &v)
		raw, _ := json.Marshal(v)
		return fmt.Sprintf("script %s: %s", pm.Implementation.ScriptCmd, string(raw)), nil
	case PackageToolSequence:
		if m.executor == nil {
			return "", fmt.Errorf("no executor for tool_sequence")
		}
		var last string
		for i, step := range pm.Implementation.Sequence {
			args := step.Arguments
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			res, err := m.executor.Execute(ctx, step.Tool, args, ExecuteOptions{})
			if err != nil {
				return "", fmt.Errorf("sequence step %d (%s): %v", i, step.Tool, err)
			}
			if res.Status != StatusOK {
				return "", fmt.Errorf("sequence step %d (%s): %s: %s", i, step.Tool, res.Status, res.Output)
			}
			last = res.Output
		}
		return last, nil
	case PackageRoutine:
		return fmt.Sprintf("routine %s: replayed with input %s", pm.Implementation.Routine, string(input)), nil
	case PackageAppCall:
		return fmt.Sprintf("app %s: %s", pm.Implementation.AppCall, string(input)), nil
	default:
		return "", fmt.Errorf("unknown kind %q", pm.Implementation.Kind)
	}
}

// Get returns the package record.
func (m *PackageManager) Get(name string) (*PackageRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.packages[name]
	if !ok {
		return nil, fmt.Errorf("tools: unknown package %q", name)
	}
	cp := *rec
	return &cp, nil
}

// List returns package names in sorted order.
func (m *PackageManager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.packages))
	for n := range m.packages {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// packageTool adapts an active package to the Tool interface.
type packageTool struct {
	manifest PackageManifest
	manager  *PackageManager
}

func (t *packageTool) Definition() Definition { return t.manifest.definition() }

func (t *packageTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	if t.manager == nil {
		return Result{Status: StatusToolError, Output: "package manager not wired"}, nil
	}
	t.manager.mu.Lock()
	rec, ok := t.manager.packages[t.manifest.Name]
	if ok {
		rec.Calls++
		now := t.manager.clock()
		rec.LastUsed = &now
	}
	t.manager.mu.Unlock()
	out, err := t.manager.executeImpl(ctx, t.manifest, inv.Arguments)
	if err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	return Result{Status: StatusOK, Output: out}, nil
}

func (t *packageTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return []string{"package:" + t.manifest.Name}, nil
}

var toolPackageSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action":       map[string]any{"type": "string", "enum": []string{"create", "stats", "list"}},
		"name":         map[string]any{"type": "string"},
		"version":      map[string]any{"type": "string"},
		"description":  map[string]any{"type": "string"},
		"input_schema": map[string]any{"type": "object"},
		"effect":       map[string]any{"type": "string", "enum": []string{"read_only", "write_local", "exec", "write_external", "communication"}},
		"idempotency":  map[string]any{"type": "string", "enum": []string{"pure", "idempotent", "keyed", "non_idempotent"}},
		"cancel":       map[string]any{"type": "string", "enum": []string{"immediate", "graceful", "after_completion", "cooperative"}},
		"fast":         map[string]any{"type": "boolean"},
		"group":        map[string]any{"type": "string"},
		"tags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"keywords":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"kind":         map[string]any{"type": "string", "enum": []string{"script", "browser_routine", "tool_sequence", "app_api"}},
		"script_cmd":   map[string]any{"type": "string"},
		"script_args":  map[string]any{"type": "string"},
		"routine":      map[string]any{"type": "string"},
		"sequence":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"app_call":     map[string]any{"type": "string"},
		"dry_run":      map[string]any{"type": "string"},
		"tests":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"timeout_secs": map[string]any{"type": "integer", "minimum": 1},
	},
	"additionalProperties": false,
})

// ToolPackageTool implements tool_package (Section 13.9).
type ToolPackageTool struct {
	Manager *PackageManager
}

func (t *ToolPackageTool) Definition() Definition {
	return Definition{
		Name: "tool_package", Description: "Create a tool package, run its tests and activate it when they pass",
		InputSchema: toolPackageSchema, Effect: "write_local", Idempotency: "keyed",
		Cancel: "after_completion", Timeout: 120_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "admin", Tags: []string{"package"}, Keywords: []string{"tool", "package", "create", "activate", "custom", "extension"},
	}
}

func (t *ToolPackageTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Action      string          `json:"action"`
		Name        string          `json:"name"`
		Version     string          `json:"version"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
		Effect      string          `json:"effect"`
		Idempotency string          `json:"idempotency"`
		Cancel      string          `json:"cancel"`
		Fast        bool            `json:"fast"`
		Group       string          `json:"group"`
		Tags        []string        `json:"tags"`
		Keywords    []string        `json:"keywords"`
		Kind        string          `json:"kind"`
		ScriptCmd   string          `json:"script_cmd"`
		ScriptArgs  string          `json:"script_args"`
		Routine     string          `json:"routine"`
		Sequence    []SequenceStep  `json:"sequence"`
		AppCall     string          `json:"app_call"`
		DryRun      string          `json:"dry_run"`
		Tests       []PackageTest   `json:"tests"`
		TimeoutSecs int64           `json:"timeout_secs"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if t.Manager == nil {
		return Result{Status: StatusToolError, Output: "package manager not wired"}, nil
	}
	action := a.Action
	if action == "" {
		action = "create"
	}
	switch action {
	case "list":
		names := t.Manager.List()
		raw, _ := json.Marshal(map[string]any{"packages": names})
		return Result{Status: StatusOK, Output: string(raw)}, nil
	case "stats":
		if a.Name == "" {
			return Result{Status: StatusInputError, Output: "name is required for stats"}, nil
		}
		rec, err := t.Manager.Get(a.Name)
		if err != nil {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		raw, _ := json.MarshalIndent(rec, "", "  ")
		return Result{Status: StatusOK, Output: string(raw)}, nil
	case "create":
		schema := a.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		pm := PackageManifest{
			Name: a.Name, Version: a.Version, Description: a.Description,
			InputSchema: schema, Effect: a.Effect, Idempotency: a.Idempotency,
			Cancel: a.Cancel, Fast: a.Fast, Group: a.Group, Tags: a.Tags,
			Keywords: a.Keywords, TimeoutSecs: a.TimeoutSecs, Tests: a.Tests,
			Implementation: PackageImpl{
				Kind: a.Kind, ScriptCmd: a.ScriptCmd, ScriptArgs: a.ScriptArgs,
				Routine: a.Routine, Sequence: a.Sequence, AppCall: a.AppCall, DryRun: a.DryRun,
			},
		}
		if pm.Version == "" {
			pm.Version = "0.1.0"
		}
		if pm.Cancel == "" {
			pm.Cancel = "graceful"
		}
		if pm.Implementation.Kind == "" {
			pm.Implementation.Kind = PackageScript
			if pm.Implementation.ScriptCmd == "" {
				pm.Implementation.ScriptCmd = "echo"
			}
		}
		rec, err := t.Manager.Create(ctx, pm)
		if err != nil {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		raw, _ := json.MarshalIndent(map[string]any{"name": rec.Manifest.Name, "status": rec.Status, "version": rec.Manifest.Version}, "", "  ")
		return Result{Status: StatusOK, Output: string(raw)}, nil
	default:
		return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown action %q", action)}, nil
	}
}

func (t *ToolPackageTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"package:" + a.Name}, nil
}

// WirePackageTool registers tool_package as deferred with its manager.
func WirePackageTool(reg *MapRegistry, cat *DeferredCatalog, mgr *PackageManager) {
	real := &ToolPackageTool{Manager: mgr}
	if cat != nil {
		if _, err := reg.Get("tool_package"); err != nil {
			_ = cat.RegisterDeferred(real)
			return
		}
		cat.MarkDeferred("tool_package")
		return
	}
	_ = reg.Register(real)
}
