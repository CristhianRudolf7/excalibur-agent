package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ATIF versions emitted by this harness. The Harbor docs name ATIF
// v1.7 with an RFC for v1.8; the validator pins the accepted one, so
// the version is selectable per trial and recorded in metadata
// (Section 23.2). See docs/decisions for the pinned choice.
const (
	ATIFv17 = "v1.7"
	ATIFv18 = "v1.8"
)

// ATIFMetadata is the run header stored in every trajectory.
type ATIFMetadata struct {
	TaskID          string    `json:"task_id"`
	Benchmark       string    `json:"benchmark,omitempty"`
	ExcaliburCommit string    `json:"excalibur_commit"`
	Profile         string    `json:"profile"`
	ProfileHash     string    `json:"profile_hash"`
	Model           string    `json:"model"`
	Provider        string    `json:"provider"`
	StartedAt       time.Time `json:"started_at"`
}

// ATIFStep is one trajectory step: a message or a tool call/result.
type ATIFStep struct {
	Seq        int             `json:"seq"`
	At         time.Time       `json:"at"`
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []ATIFToolCall  `json:"tool_calls,omitempty"`
	ToolResult *ATIFToolResult `json:"tool_result,omitempty"`
}

// ATIFToolCall is one model-requested invocation.
type ATIFToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ATIFToolResult is the executed outcome.
type ATIFToolResult struct {
	CallID   string `json:"call_id"`
	Name     string `json:"name"`
	Output   string `json:"output"`
	IsError  bool   `json:"is_error"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// Trajectory is the ATIF document written to trajectory.json.
type Trajectory struct {
	Version  string       `json:"atif_version"`
	Metadata ATIFMetadata `json:"metadata"`
	Steps    []ATIFStep   `json:"steps"`
}

// Writer incrementally writes trajectory.json: every Append rewrites
// the whole file atomically (temp + fsync + rename) so a trial cut by
// timeout, OOM or crash keeps a valid partial trajectory.
type Writer struct {
	mu   sync.Mutex
	path string
	traj Trajectory
}

// NewWriter creates dir/trajectory.json with the header. Version must
// be ATIFv17 or ATIFv18.
func NewWriter(dir, version string, meta ATIFMetadata) (*Writer, error) {
	if version != ATIFv17 && version != ATIFv18 {
		return nil, fmt.Errorf("telemetry: unsupported ATIF version %q", version)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("telemetry: mkdir atif: %w", err)
	}
	w := &Writer{
		path: filepath.Join(dir, "trajectory.json"),
		traj: Trajectory{Version: version, Metadata: meta},
	}
	if err := w.flushLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

// OpenWriter reopens an existing trajectory for append (resume).
func OpenWriter(path string) (*Writer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("telemetry: read atif: %w", err)
	}
	var traj Trajectory
	if err := json.Unmarshal(raw, &traj); err != nil {
		return nil, fmt.Errorf("telemetry: parse atif: %w", err)
	}
	if err := Validate(&traj); err != nil {
		return nil, err
	}
	return &Writer{path: path, traj: traj}, nil
}

// Append adds one step and flushes incrementally.
func (w *Writer) Append(step ATIFStep) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	step.Seq = len(w.traj.Steps) + 1
	if step.At.IsZero() {
		step.At = time.Now().UTC()
	}
	w.traj.Steps = append(w.traj.Steps, step)
	return w.flushLocked()
}

// Steps returns a copy of the steps so far.
func (w *Writer) Steps() []ATIFStep {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]ATIFStep(nil), w.traj.Steps...)
}

// Path returns the trajectory file path.
func (w *Writer) Path() string { return w.path }

// Version returns the ATIF version.
func (w *Writer) Version() string { return w.traj.Version }

func (w *Writer) flushLocked() error {
	buf, err := json.MarshalIndent(w.traj, "", "  ")
	if err != nil {
		return fmt.Errorf("telemetry: marshal atif: %w", err)
	}
	buf = append(buf, '\n')
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("telemetry: write atif tmp: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("telemetry: open atif tmp: %w", err)
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	if syncErr != nil {
		return fmt.Errorf("telemetry: sync atif: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("telemetry: close atif: %w", closeErr)
	}
	if err := os.Rename(tmp, w.path); err != nil {
		return fmt.Errorf("telemetry: rename atif: %w", err)
	}
	return nil
}

// Validate checks an ATIF trajectory: supported version, required
// metadata, ordered steps and well-formed tool calls. v1.8 additionally
// requires provider and profile hash metadata.
func Validate(t *Trajectory) error {
	if t == nil {
		return fmt.Errorf("telemetry: nil trajectory")
	}
	if t.Version != ATIFv17 && t.Version != ATIFv18 {
		return fmt.Errorf("telemetry: unsupported ATIF version %q", t.Version)
	}
	if t.Metadata.TaskID == "" {
		return fmt.Errorf("telemetry: ATIF metadata missing task_id")
	}
	if t.Metadata.ExcaliburCommit == "" {
		return fmt.Errorf("telemetry: ATIF metadata missing excalibur_commit")
	}
	if t.Version == ATIFv18 {
		if t.Metadata.Provider == "" {
			return fmt.Errorf("telemetry: ATIF v1.8 metadata missing provider")
		}
		if t.Metadata.ProfileHash == "" {
			return fmt.Errorf("telemetry: ATIF v1.8 metadata missing profile_hash")
		}
	}
	for i, s := range t.Steps {
		if s.Seq != i+1 {
			return fmt.Errorf("telemetry: ATIF step %d has seq %d", i, s.Seq)
		}
		if s.Role == "" {
			return fmt.Errorf("telemetry: ATIF step %d missing role", s.Seq)
		}
		if s.ToolResult != nil && s.ToolResult.CallID == "" {
			return fmt.Errorf("telemetry: ATIF step %d tool result missing call_id", s.Seq)
		}
		for _, c := range s.ToolCalls {
			if c.ID == "" || c.Name == "" {
				return fmt.Errorf("telemetry: ATIF step %d tool call missing id/name", s.Seq)
			}
		}
	}
	return nil
}

// ValidateFile reads path and validates it.
func ValidateFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("telemetry: read atif %s: %w", path, err)
	}
	var t Trajectory
	if err := json.Unmarshal(raw, &t); err != nil {
		return fmt.Errorf("telemetry: parse atif %s: %w", path, err)
	}
	return Validate(&t)
}
