package extsdk

import (
	"context"
	"encoding/json"
	"time"
)

// Effect, idempotency and cancel classes mirror internal/tools. Values
// are identical so manifests validate on both sides.
const (
	EffectReadOnly      = "read_only"
	EffectWriteLocal    = "write_local"
	EffectExec          = "exec"
	EffectWriteExternal = "write_external"
	EffectCommunication = "communication"

	IdemPure          = "pure"
	IdemIdempotent    = "idempotent"
	IdemKeyed         = "keyed"
	IdemNonIdempotent = "non_idempotent"

	CancelImmediate       = "immediate"
	CancelGraceful        = "graceful"
	CancelAfterCompletion = "after_completion"
	CancelCooperative     = "cooperative"
)

// Definition is the catalog entry of one extension tool.
type Definition struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Effect       string          `json:"effect"`
	Idempotency  string          `json:"idempotency"`
	Timeout      time.Duration   `json:"timeout"`
	OutputBudget int64           `json:"output_budget"`
	Platforms    []string        `json:"platforms,omitempty"`
	Profiles     []string        `json:"profiles,omitempty"`
	Inverse      string          `json:"inverse,omitempty"`
	Probe        string          `json:"probe,omitempty"`
	DryRun       string          `json:"dry_run,omitempty"`
	Fast         bool            `json:"fast"`
	Cancel       string          `json:"cancel"`
	Group        string          `json:"group,omitempty"`
	Tags         []string        `json:"tags,omitempty"`
	Keywords     []string        `json:"keywords,omitempty"`
}

// Invocation is one validated tool call.
type Invocation struct {
	Tool           string          `json:"tool"`
	Arguments      json.RawMessage `json:"arguments"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Timeout        time.Duration   `json:"timeout,omitempty"`
}

// Result is the normalized tool outcome.
type Result struct {
	Status    string `json:"status"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
	BlobRef   string `json:"blob_ref,omitempty"`
	Transient bool   `json:"transient"`
}

// Tool is the interface every extension tool implements, in Go or over
// MCP/JSON-RPC with the same manifest.
type Tool interface {
	Definition() Definition
	Invoke(ctx context.Context, inv Invocation) (Result, error)
	ResourceKeys(args json.RawMessage) ([]string, error)
}

// Capability is the interface of an external capability adapter
// (Section 14.1): operations with a durable-task lifecycle
// (working/input_required/completed/failed/cancelled).
type Capability interface {
	// Manifest describes identity, operations and limits.
	Manifest() Manifest
	// Invoke runs one operation with an idempotency key.
	Invoke(ctx context.Context, operation string, args json.RawMessage, idempotencyKey string) (Result, error)
}
