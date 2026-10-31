package capabilities

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TaskState is the durable-task lifecycle of Section 14.1, adopted from
// the MCP tasks extension (io.modelcontextprotocol/tasks). Native and
// MCP capabilities share it so the harness polls both the same way.
type TaskState string

// Durable task states.
const (
	StateWorking       TaskState = "working"
	StateInputRequired TaskState = "input_required"
	StateCompleted     TaskState = "completed"
	StateFailed        TaskState = "failed"
	StateCancelled     TaskState = "cancelled"
)

// ErrorClass classifies a failed operation for retry handling
// (Sections 14.1 and 17.7).
type ErrorClass string

// Error classes.
const (
	ErrTransient ErrorClass = "transient"
	ErrInput     ErrorClass = "input"
	ErrPermanent ErrorClass = "permanent"
)

// Operation is one capability operation exposed as a deferred tool
// (Sections 13.3 and 14.1). Group, Tags and Keywords steer
// search_tools discovery (verb, object, domain per Section 13.3);
// TimeoutSecs, OutputBudget, Inverse, Probe and DryRun carry the
// Section 13.1 execution fields so a capability operation converts to
// a deferred tool without loss. Zero values fall back to the harness
// defaults applied by the adapter.
type Operation struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Effect       string          `json:"effect"`
	Idempotency  string          `json:"idempotency"`
	Platforms    []string        `json:"platforms,omitempty"`
	Profiles     []string        `json:"profiles,omitempty"`
	Cancel       string          `json:"cancel"`
	Fast         bool            `json:"fast"`
	Group        string          `json:"group,omitempty"`
	Tags         []string        `json:"tags,omitempty"`
	Keywords     []string        `json:"keywords,omitempty"`
	TimeoutSecs  int64           `json:"timeout_secs,omitempty"`
	OutputBudget int64           `json:"output_budget,omitempty"`
	Inverse      string          `json:"inverse,omitempty"`
	Probe        string          `json:"probe,omitempty"`
	DryRun       string          `json:"dry_run,omitempty"`
}

// Validate checks the operation entry.
func (o Operation) Validate() error {
	if o.Name == "" {
		return fmt.Errorf("capabilities: operation needs a name")
	}
	if o.Description == "" {
		return fmt.Errorf("capabilities: operation %q needs a description", o.Name)
	}
	if len(o.InputSchema) == 0 {
		return fmt.Errorf("capabilities: operation %q needs an input schema", o.Name)
	}
	switch o.Effect {
	case "read_only", "write_local", "exec", "write_external", "communication":
	default:
		return fmt.Errorf("capabilities: operation %q has unknown effect %q", o.Name, o.Effect)
	}
	switch o.Idempotency {
	case "pure", "idempotent", "keyed", "non_idempotent":
	default:
		return fmt.Errorf("capabilities: operation %q has unknown idempotency %q", o.Name, o.Idempotency)
	}
	switch o.Cancel {
	case "immediate", "graceful", "after_completion", "cooperative":
	default:
		return fmt.Errorf("capabilities: operation %q has unknown cancel %q", o.Name, o.Cancel)
	}
	return nil
}

// ResourceLimits caps what a capability may consume (Section 14.1).
type ResourceLimits struct {
	RAMBytes    int64 `json:"ram_bytes,omitempty"`
	MaxSessions int   `json:"max_sessions,omitempty"`
	MaxCalls    int   `json:"max_calls,omitempty"`
	Concurrency int   `json:"concurrency,omitempty"`
}

// Manifest is the published contract of one capability (Section 14.1):
// identity, version, operations, effect classes, platforms, profiles,
// resources, concurrency and reportable states.
type Manifest struct {
	ID         string         `json:"id"`
	Version    string         `json:"version"`
	Operations []Operation    `json:"operations"`
	Platforms  []string       `json:"platforms,omitempty"`
	Profiles   []string       `json:"profiles,omitempty"`
	Resources  ResourceLimits `json:"resources,omitempty"`
}

// Validate checks the manifest.
func (m Manifest) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("capabilities: manifest needs an id")
	}
	if m.Version == "" {
		return fmt.Errorf("capabilities: capability %q needs a version", m.ID)
	}
	if len(m.Operations) == 0 {
		return fmt.Errorf("capabilities: capability %q declares no operations", m.ID)
	}
	seen := map[string]bool{}
	for _, op := range m.Operations {
		if err := op.Validate(); err != nil {
			return fmt.Errorf("capabilities: %s: %w", m.ID, err)
		}
		if seen[op.Name] {
			return fmt.Errorf("capabilities: %s declares duplicate operation %q", m.ID, op.Name)
		}
		seen[op.Name] = true
	}
	return nil
}

// TaskResult is one pollable outcome of a long operation.
type TaskResult struct {
	TaskID    string          `json:"task_id"`
	State     TaskState       `json:"state"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Need      string          `json:"need,omitempty"`
	ErrClass  ErrorClass      `json:"error_class,omitempty"`
	ErrorText string          `json:"error,omitempty"`
}

// Capability is the harness-side contract every external capability
// honors (Section 14.1). Short operations return a terminal state
// immediately; long ones return working with a task id the harness
// polls. The harness always sends an idempotency key for operations
// with effects, applies timeouts and budgets, and persists task ids
// to resume polling after a restart.
type Capability interface {
	// Manifest describes identity, operations and limits.
	Manifest() Manifest
	// Invoke runs one operation with an idempotency key. Repeats with
	// the same key must not repeat effects: the capability returns the
	// first outcome.
	Invoke(operation string, args json.RawMessage, idempotencyKey string) (TaskResult, error)
	// Poll resumes a working task by id for crash recovery (17.3).
	Poll(taskID string) (TaskResult, error)
	// Cancel asks a working task to stop. Cancellation is cooperative
	// and may not take effect; the harness re-polls the final state.
	Cancel(taskID string) (TaskResult, error)
	// Update answers an input_required task (CAPTCHA, login, file).
	Update(taskID string, answer json.RawMessage) (TaskResult, error)
}

// Terminal reports whether the state ends polling.
func Terminal(s TaskState) bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

// ClassifyError maps an error to the retry class of Section 17.7.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "temporar", "overload", "busy", "unavailable", "rate limit", "429", "503"} {
		if strings.Contains(msg, marker) {
			return ErrTransient
		}
	}
	for _, marker := range []string{"invalid", "bad ", "schema", "missing", "unknown", "parse"} {
		if strings.Contains(msg, marker) {
			return ErrInput
		}
	}
	return ErrPermanent
}

// BlockedBenchmarkSource reports whether a URL belongs to the benchmark
// itself (Section 23.2): Terminal-Bench and Harbor repos, Harbor Hub,
// tbench.ai and published copies of their tasks. Consulting them is
// reward hacking: the harness blocks the read and logs it. This is the
// single shared implementation used by the web tools and the eval
// profile check.
func BlockedBenchmarkSource(url string) bool {
	u := strings.ToLower(url)
	for _, marker := range []string{
		"terminal-bench", "harbor", "tbench.ai",
		"github.com/harbor-framework",
	} {
		if strings.Contains(u, marker) {
			return true
		}
	}
	return false
}

// TaskRecord persists a durable-task id for resume after restart
// (Section 14.1 obligations). The journal owns durability; this record
// is the in-memory shape the harness serializes.
type TaskRecord struct {
	CapabilityID   string          `json:"capability_id"`
	Operation      string          `json:"operation"`
	TaskID         string          `json:"task_id"`
	Args           json.RawMessage `json:"args,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	State          TaskState       `json:"state"`
	PollInterval   time.Duration   `json:"poll_interval_ms"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// DefaultPollInterval bounds how often the harness polls a working task.
const DefaultPollInterval = 2 * time.Second

// NewTaskRecord starts a durable-task record.
func NewTaskRecord(capabilityID, operation, taskID, key string, args json.RawMessage, now time.Time) TaskRecord {
	if now.IsZero() {
		now = time.Now()
	}
	return TaskRecord{
		CapabilityID: capabilityID, Operation: operation, TaskID: taskID,
		Args: args, IdempotencyKey: key, State: StateWorking,
		PollInterval: DefaultPollInterval, UpdatedAt: now,
	}
}

// MCPToolName maps a server tool to its catalog name (Section 13.3):
// mcp__<server>__<tool>, abbreviated with a hash when it exceeds the
// 64-char provider limit.
func MCPToolName(server, tool string) string {
	srv := sanitizeMCP(server)
	tl := sanitizeMCP(tool)
	name := "mcp__" + srv + "__" + tl
	if len(name) <= 64 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	short := hex.EncodeToString(sum[:])[:8]
	// Keep a readable head plus the hash; total stays within 64.
	head := 64 - len(short) - 1
	if head < 1 {
		head = 1
	}
	trimmed := name
	if len(trimmed) > head {
		trimmed = trimmed[:head]
	}
	trimmed = strings.TrimRight(trimmed, "_")
	return trimmed + "_" + short
}

func sanitizeMCP(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r == '-', r == '.', r == ' ', r == '/':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" {
		return "x"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "m_" + out
	}
	return out
}
