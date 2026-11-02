package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// namePattern enforces snake_case tool names of at most 64 chars: the
// shape provider tool-call APIs accept (Section 13.2).
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidateName checks a tool name: snake_case, at most 64 chars.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("tools: bad tool name %q: want snake_case, max 64 chars", name)
	}
	return nil
}

// Definition is the catalog entry of one tool (Section 13.1). All fields
// are English; descriptions are written to be found by search_tools
// (verb, object, domain).
type Definition struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	OutputSchema json.RawMessage        `json:"output_schema,omitempty"`
	Effect       types.EffectClass      `json:"effect"`
	Idempotency  types.IdempotencyClass `json:"idempotency"`
	Timeout      time.Duration          `json:"timeout"`
	OutputBudget int64                  `json:"output_budget"`
	Platforms    []string               `json:"platforms,omitempty"`
	Profiles     []string               `json:"profiles,omitempty"`
	Inverse      string                 `json:"inverse,omitempty"`
	Probe        string                 `json:"probe,omitempty"`
	DryRun       string                 `json:"dry_run,omitempty"`
	Fast         bool                   `json:"fast"`
	Cancel       types.CancelMode       `json:"cancel"`
	Group        string                 `json:"group,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
	Keywords     []string               `json:"keywords,omitempty"`
}

// Validate checks the definition fields.
func (d Definition) Validate() error {
	if err := ValidateName(d.Name); err != nil {
		return err
	}
	if d.Description == "" {
		return fmt.Errorf("tools: tool %q needs a description", d.Name)
	}
	if len(d.InputSchema) == 0 {
		return fmt.Errorf("tools: tool %q needs an input schema", d.Name)
	}
	switch d.Effect {
	case types.EffectReadOnly, types.EffectWriteLocal, types.EffectExec,
		types.EffectWriteExternal, types.EffectCommunication:
	default:
		return fmt.Errorf("tools: tool %q has unknown effect %q", d.Name, d.Effect)
	}
	switch d.Idempotency {
	case types.IdemPure, types.IdemIdempotent, types.IdemKeyed, types.IdemNonIdempotent:
	default:
		return fmt.Errorf("tools: tool %q has unknown idempotency %q", d.Name, d.Idempotency)
	}
	switch d.Cancel {
	case types.CancelImmediate, types.CancelGraceful,
		types.CancelAfterCompletion, types.CancelCooperative:
	default:
		return fmt.Errorf("tools: tool %q has unknown cancel mode %q", d.Name, d.Cancel)
	}
	return nil
}

// Invocation is one validated tool call handed to Tool.Invoke.
type Invocation struct {
	Tool           string          `json:"tool"`
	Arguments      json.RawMessage `json:"arguments"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Timeout        time.Duration   `json:"timeout,omitempty"`
}

// ResultStatus classifies a tool outcome for the engine.
type ResultStatus string

// Result statuses.
const (
	StatusOK         ResultStatus = "ok"
	StatusInputError ResultStatus = "input_error"
	StatusToolError  ResultStatus = "tool_error"
	StatusTransient  ResultStatus = "transient"
	StatusTimeout    ResultStatus = "timeout"
	StatusCanceled   ResultStatus = "canceled"
	StatusDenied     ResultStatus = "denied"
)

// SessionReceipt identifies only processes owned or observed by this tool call.
// It is durable evidence, separate from model-visible output text.
type SessionReceipt struct {
	ID       string `json:"id"`
	Alive    bool   `json:"alive"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// Result is the normalized tool outcome. Large outputs are truncated to
// the output budget with the remainder kept in BlobRef (Section 13.4).
type Result struct {
	Sessions  []SessionReceipt `json:"sessions,omitempty"`
	Status    ResultStatus     `json:"status"`
	Output    string           `json:"output"`
	Truncated bool             `json:"truncated"`
	BlobRef   string           `json:"blob_ref,omitempty"`
	Transient bool             `json:"transient"`
	// ExitCode is the process exit status of an exec result (128+n when
	// a signal ended the process). Nil for tools without an exit status
	// and for sessions still running.
	ExitCode *int `json:"exit_code,omitempty"`
	// Artifacts lists visual files produced for the user (see ArtifactRef).
	Artifacts []ArtifactRef `json:"artifacts,omitempty"`
}

// Tool is the interface every tool implements: built-ins, MCP servers,
// apps, packages and Go extensions alike (Section 14.12).
type Tool interface {
	// Definition returns the catalog entry.
	Definition() Definition
	// Invoke executes one validated call. It must honor ctx
	// cancellation per its cancel mode and never repeat a
	// non-idempotent effect on retry: repeats carry the same
	// idempotency key.
	Invoke(ctx context.Context, inv Invocation) (Result, error)
	// ResourceKeys extracts serialization and lease keys (canonical
	// paths, session ids, external accounts) for Section 7.4.
	ResourceKeys(args json.RawMessage) ([]string, error)
}

// Prober is optionally implemented by tools that can check whether
// their effect already happened, for crash recovery (Section 17.2).
type Prober interface {
	Probe(ctx context.Context, args json.RawMessage) (bool, error)
}

// Compensator is optionally implemented by tools that can undo their
// effect (Section 15.8).
type Compensator interface {
	Compensate(ctx context.Context, args json.RawMessage, result Result) error
}
