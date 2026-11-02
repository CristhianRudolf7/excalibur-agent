package api

import "time"

// TasksQueryParams selects a stable page ordered by task ID.
type TasksQueryParams struct {
	Status    []string `json:"status,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	Text      string   `json:"text,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Cursor    string   `json:"cursor,omitempty"`
}
type TaskSummary struct {
	ID            string     `json:"id"`
	ShortCode     string     `json:"short_code"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	Level         string     `json:"level"`
	ProjectID     string     `json:"project_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	CriteriaTotal int        `json:"criteria_total"`
	CriteriaMet   int        `json:"criteria_met"`
	BlockedReason string     `json:"blocked_reason,omitempty"`
}
type TasksQueryResult struct {
	Tasks      []TaskSummary  `json:"tasks"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Counts     map[string]int `json:"counts"`
}
type TaskText struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type TaskCriterion struct {
	TaskText
	State  string `json:"state"`
	Method string `json:"method"`
}
type TaskEvidence struct {
	CriterionID string    `json:"criterion_id"`
	Kind        string    `json:"kind"`
	Summary     string    `json:"summary"`
	At          time.Time `json:"at"`
	OK          bool      `json:"ok"`
}
type TaskPlanItem struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	DependsOn []string `json:"depends_on"`
}
type TaskRun struct {
	ID        string     `json:"id"`
	State     string     `json:"state"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}
type TaskDetail struct {
	TaskSummary
	Request      string          `json:"request"`
	Requirements []TaskText      `json:"requirements"`
	Criteria     []TaskCriterion `json:"criteria"`
	Evidence     []TaskEvidence  `json:"evidence"`
	Plan         []TaskPlanItem  `json:"plan"`
	Notes        string          `json:"notes,omitempty"`
	Runs         []TaskRun       `json:"runs"`
}
type Reminder struct {
	ID          string     `json:"id"`
	Text        string     `json:"text"`
	DueAt       time.Time  `json:"due_at"`
	Timezone    string     `json:"timezone"`
	Repeat      string     `json:"repeat,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
}
type TaskUpdatedEvent struct {
	Task TaskSummary `json:"task"`
}
type ReminderEvent struct {
	Reminder Reminder `json:"reminder"`
}

const (
	EventTaskUpdated     = "task.updated"
	EventReminderUpdated = "reminder.updated"
	EventReminderFired   = "reminder.fired"
)

// TasksGetParams identifies one durable task.
type TasksGetParams struct {
	ID string `json:"id"`
}

// RemindersParams is shared by CRUD operations; create requires Text and DueAt,
// and update requires ID. Pointer fields distinguish omission from empty edits.
type RemindersParams struct {
	ID          string     `json:"id"`
	Text        *string    `json:"text,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	Timezone    string     `json:"timezone,omitempty"`
	Repeat      *string    `json:"repeat,omitempty"`
	Status      string     `json:"status,omitempty"`
	SnoozeUntil *time.Time `json:"snooze_until,omitempty"`
	IncludeDone bool       `json:"include_done,omitempty"`
}
