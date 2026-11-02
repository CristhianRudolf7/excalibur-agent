// Package automations implements classic scheduled automations: a
// schedule plus a small graph of real steps (agent, script, tool,
// condition, notify) executed on the agenda workflow engine.
//
// Only the agent creates or edits automations (through the
// "automations" tool); the user and the agent can run, pause, resume
// and delete them. The wire shapes mirror apps/web/src/lib/rpc/contract.ts.
package automations

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned for unknown automation ids. RPC layers must
// keep "not found" in the error message.
var ErrNotFound = errors.New("automation not found")

// ErrAlreadyRunning is returned by RunNow while a run is in progress.
var ErrAlreadyRunning = errors.New("already running")

// MaxRunDuration caps every run.
const MaxRunDuration = 15 * time.Minute

// MaxConsecutiveFailures pauses an automation after this many failed runs in a row.
const MaxConsecutiveFailures = 3

type Status string

const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
)

type ScheduleKind string

const (
	ScheduleOnce     ScheduleKind = "once"
	ScheduleInterval ScheduleKind = "interval"
	ScheduleDaily    ScheduleKind = "daily"
	ScheduleWeekly   ScheduleKind = "weekly"
	ScheduleMonthly  ScheduleKind = "monthly"
	ScheduleCron     ScheduleKind = "cron"
)

type Schedule struct {
	Kind         ScheduleKind `json:"kind"`
	Timezone     string       `json:"timezone"`
	At           *time.Time   `json:"at,omitempty"`
	EveryMinutes int          `json:"every_minutes,omitempty"`
	Time         string       `json:"time,omitempty"`
	Weekdays     []int        `json:"weekdays,omitempty"`
	DayOfMonth   int          `json:"day_of_month,omitempty"`
	Cron         string       `json:"cron,omitempty"`
}

type StepType string

const (
	StepAgent     StepType = "agent"
	StepScript    StepType = "script"
	StepTool      StepType = "tool"
	StepCondition StepType = "condition"
	StepNotify    StepType = "notify"
)

type Step struct {
	ID          string         `json:"id"`
	Type        StepType       `json:"type"`
	Title       string         `json:"title"`
	Instruction string         `json:"instruction,omitempty"`
	Language    string         `json:"language,omitempty"` // python | node | bash
	Code        string         `json:"code,omitempty"`
	Tool        string         `json:"tool,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
	Expression  string         `json:"expression,omitempty"`
	Message     string         `json:"message,omitempty"`
}

type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"` // "yes" | "no" for condition steps
}

// Spec is what the agent provides on create/update.
type Spec struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Schedule    Schedule `json:"schedule"`
	Steps       []Step   `json:"steps"`
	Edges       []Edge   `json:"edges"`
}

type Automation struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Description         string     `json:"description"`
	Status              Status     `json:"status"`
	PausedReason        string     `json:"paused_reason,omitempty"`
	Version             int        `json:"version"`
	Schedule            Schedule   `json:"schedule"`
	Steps               []Step     `json:"steps"`
	Edges               []Edge     `json:"edges"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastRun             *Run       `json:"last_run,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type Version struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Note      string    `json:"note,omitempty"`
}

type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

type StepRunStatus string

const (
	StepPending   StepRunStatus = "pending"
	StepRunning   StepRunStatus = "running"
	StepSucceeded StepRunStatus = "succeeded"
	StepFailed    StepRunStatus = "failed"
	StepSkipped   StepRunStatus = "skipped"
)

type StepRun struct {
	StepID        string        `json:"step_id"`
	Status        StepRunStatus `json:"status"`
	StartedAt     *time.Time    `json:"started_at,omitempty"`
	FinishedAt    *time.Time    `json:"finished_at,omitempty"`
	OutputPreview string        `json:"output_preview,omitempty"` // <= 2000 chars
	Error         string        `json:"error,omitempty"`
}

type RunTrigger string

const (
	TriggerSchedule RunTrigger = "schedule"
	TriggerManual   RunTrigger = "manual"
	TriggerCatchUp  RunTrigger = "catch_up"
)

type Run struct {
	ID           string     `json:"id"`
	AutomationID string     `json:"automation_id"`
	Version      int        `json:"version"`
	Trigger      RunTrigger `json:"trigger"`
	Status       RunStatus  `json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Summary      string     `json:"summary,omitempty"`
	Error        string     `json:"error,omitempty"`
	Steps        []StepRun  `json:"steps"`
}

// EventKind names live events; the daemon maps them to
// "automation.updated", "automation.deleted" and "automation.run".
type EventKind string

const (
	EventUpdated EventKind = "automation.updated"
	EventDeleted EventKind = "automation.deleted"
	EventRun     EventKind = "automation.run"
)

type Event struct {
	Kind       EventKind
	Automation *Automation // EventUpdated
	ID         string      // EventDeleted
	Run        *Run        // EventRun
}

// Service is the whole automation surface used by the RPC routes and
// the agent tool. Implemented by *Manager (owned by worker AUTO-BE1).
type Service interface {
	List(ctx context.Context) ([]Automation, error)
	Get(ctx context.Context, id string) (Automation, []Version, error)
	Runs(ctx context.Context, id string, limit int) ([]Run, error)
	Create(ctx context.Context, spec Spec) (Automation, error)
	Update(ctx context.Context, id string, spec Spec, note string) (Automation, error)
	RestoreVersion(ctx context.Context, id string, version int) (Automation, error)
	RunNow(ctx context.Context, id string) (Run, error)
	Pause(ctx context.Context, id, reason string) (Automation, error)
	Resume(ctx context.Context, id string) (Automation, error)
	Delete(ctx context.Context, id string) error
	// Subscribe registers a listener for live events; the returned func unsubscribes.
	Subscribe(fn func(Event)) func()
}

// Dependencies the Manager needs from the daemon. AUTO-BE2 implements
// the daemon adapters; AUTO-BE1 uses fakes in tests.

// AgentRunner runs one agent step: a fresh Excalibur turn with the
// instruction plus the outputs of previous steps, and returns the final
// answer text. It must honour ctx cancellation.
type AgentRunner interface {
	RunAgentStep(ctx context.Context, automation Automation, step Step, prior map[string]string) (string, error)
}

// ToolInvoker calls one registry tool with JSON args and returns its output as text.
type ToolInvoker interface {
	InvokeTool(ctx context.Context, name string, args map[string]any) (string, error)
}

// Notifier posts a short message to the Excalibur chat.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}
