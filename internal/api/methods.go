package api

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Method name constants for every method family of Section 3.5, plus
// the task delegation family of Section 3.6. The Methods registry below
// enumerates them for schema export and conformance tests.
const (
	MethodActivityList    = "activity.list"
	MethodFilesOpen       = "files.open"
	MethodVoiceStatus     = "voice.status"
	MethodVoiceTranscribe = "voice.transcribe"
	MethodVoiceSpeak      = "voice.speak"

	MethodLibraryIngest = "library.ingest"
	MethodLibraryList   = "library.list"
	MethodLibraryGet    = "library.get"
	MethodLibraryPages  = "library.pages"
	MethodLibraryChunk  = "library.chunk"
	MethodLibraryDelete = "library.delete"
	MethodLibraryCancel = "library.cancel"

	MethodTasksQuery        = "tasks.query"
	MethodTasksGet          = "tasks.get"
	MethodAutomationsList   = "automations.list"
	MethodAutomationsGet    = "automations.get"
	MethodAutomationsRuns   = "automations.runs"
	MethodAutomationsRun    = "automations.run"
	MethodAutomationsPause  = "automations.pause"
	MethodAutomationsResume = "automations.resume"
	MethodAutomationsDelete = "automations.delete"
	MethodRemindersList     = "reminders.list"
	MethodRemindersCreate   = "reminders.create"
	MethodRemindersUpdate   = "reminders.update"
	MethodRemindersDelete   = "reminders.delete"

	// Principal family: local user profile.
	MethodPrincipalGet = "principal.get"

	// Conversation family: send, subscribe, history.
	MethodConversationSend      = "conversation.send"
	MethodConversationSubscribe = "conversation.subscribe"
	MethodConversationHistory   = "conversation.history"

	// Run control family.
	MethodRunList      = "run.list"
	MethodRunStatus    = "run.status"
	MethodRunPause     = "run.pause"
	MethodRunResume    = "run.resume"
	MethodRunInterrupt = "run.interrupt"
	MethodRunCancel    = "run.cancel"
	MethodRunRedirect  = "run.redirect"

	// Task delegation family (Section 3.6, CLI and MCP parity).
	MethodTaskStart  = "task.start"
	MethodTaskStatus = "task.status"
	MethodTaskWait   = "task.wait"
	MethodTaskResult = "task.result"
	MethodTaskReply  = "task.reply"
	MethodTaskCancel = "task.cancel"
	MethodTaskPause  = "task.pause"
	MethodTaskResume = "task.resume"
	MethodTaskList   = "task.list"
	MethodTaskLogs   = "task.logs"

	// Project family.
	MethodProjectList     = "project.list"
	MethodProjectRegister = "project.register"
	MethodProjectGet      = "project.get"

	// Plan approval.
	MethodPlanApprove = "plan.approve"
	MethodPlanDeny    = "plan.deny"

	// Memory family: query, correct, inspect provenance.
	MethodMemoryQuery      = "memory.query"
	MethodMemoryCorrect    = "memory.correct"
	MethodMemoryProvenance = "memory.provenance"

	// Skills family.
	MethodSkillsSearch  = "skills.search"
	MethodSkillsLoad    = "skills.load"
	MethodSkillsInstall = "skills.install"

	MethodSkillsGroups   = "skills.groups"
	MethodSkillsList     = "skills.list"
	MethodSkillsGet      = "skills.get"
	MethodSkillsUpdate   = "skills.update"
	MethodSkillsSetState = "skills.set_state"
	MethodSkillsDelete   = "skills.delete"
	MethodUsageSummary   = "usage.summary"

	// Capability family: app and extension registry (Section 14.12).
	MethodCapabilityRegister = "capability.register"
	MethodCapabilityInvoke   = "capability.invoke"
	MethodCapabilityList     = "capability.list"

	// Vault family: values never travel over the conversation (20.1).
	MethodVaultCreate       = "vault.create"
	MethodVaultUpdate       = "vault.update"
	MethodVaultListMetadata = "vault.list_metadata"
	MethodVaultLock         = "vault.lock"
	MethodVaultUnlock       = "vault.unlock"
	MethodVaultDelete       = "vault.delete"

	// Questions, metrics, files, config.
	MethodQuestionAnswer = "question.answer"
	MethodMetricsQuery   = "metrics.query"
	MethodFilesList      = "files.list"
	MethodConfigGet      = "config.get"
	MethodConfigSet      = "config.set"
	MethodConfigList     = "config.list"
)

// MethodInfo describes one method for documentation and schema export.
type MethodInfo struct {
	Name    string `json:"name"`
	Family  string `json:"family"`
	Summary string `json:"summary"`
}

// Methods enumerates every method of the gate contract. The gate must
// reject unknown methods with CodeMethodNotFound and the contracts
// export test requires every entry to appear in packages/contracts.
var Methods = []MethodInfo{
	{MethodArtifactsGet, "artifacts", "Fetch a visual artifact and its source in a conversation run."},
	{MethodArtifactsList, "artifacts", "List retained visual artifacts in a conversation."},
	{MethodActivityList, "activity", "List conversation activity."},
	{MethodFilesOpen, "files", "Open a workspace file."},
	{MethodVoiceStatus, "voice", "Read voice availability."},
	{MethodVoiceTranscribe, "voice", "Transcribe an audio blob."},
	{MethodVoiceSpeak, "voice", "Generate speech as an audio blob."},
	{MethodTasksQuery, "tasks", "tasks.query."},
	{MethodTasksGet, "tasks", "tasks.get."},
	{MethodAutomationsList, "automations", "automations.list."},
	{MethodAutomationsGet, "automations", "automations.get."},
	{MethodAutomationsRuns, "automations", "automations.runs."},
	{MethodAutomationsRun, "automations", "automations.run."},
	{MethodAutomationsPause, "automations", "automations.pause."},
	{MethodAutomationsResume, "automations", "automations.resume."},
	{MethodAutomationsDelete, "automations", "automations.delete."},
	{MethodRemindersList, "reminders", "reminders.list."},
	{MethodRemindersCreate, "reminders", "reminders.create."},
	{MethodRemindersUpdate, "reminders", "reminders.update."},
	{MethodRemindersDelete, "reminders", "reminders.delete."},

	{MethodWhiteboardList, "whiteboard", "List interactive whiteboards."},
	{MethodWhiteboardGet, "whiteboard", "Get interactive whiteboards."},
	{MethodWhiteboardApply, "whiteboard", "Apply interactive whiteboards."},
	{MethodWhiteboardSnapshot, "whiteboard", "Snapshot interactive whiteboards."},
	{MethodWhiteboardClose, "whiteboard", "Close interactive whiteboards."},

	{MethodLibraryIngest, "library", "Ingest library documents."},
	{MethodLibraryList, "library", "List library documents."},
	{MethodLibraryGet, "library", "Get library documents."},
	{MethodLibraryPages, "library", "Pages library documents."},
	{MethodLibraryChunk, "library", "Chunk library documents."},
	{MethodLibraryDelete, "library", "Delete library documents."},
	{MethodLibraryCancel, "library", "Cancel library documents."},

	{MethodPrincipalGet, "principal", "Read the local user's profile."},
	{MethodConversationSend, "conversation", "Send a message; idempotent on client_msg_id."},
	{MethodConversationSubscribe, "conversation", "Subscribe to typed events from a cursor."},
	{MethodConversationHistory, "conversation", "Read message history with pagination."},
	{MethodRunList, "run", "List runs with state and queue position."},
	{MethodRunStatus, "run", "Deterministic snapshot of a run (Section 15.6)."},
	{MethodRunPause, "run", "Pause a run and its subtree with immediate interrupt."},
	{MethodRunResume, "run", "Resume what a matching pause paused."},
	{MethodRunInterrupt, "run", "Stop the current model call and tool per cancel class."},
	{MethodRunCancel, "run", "Cancel a run and its descendants bottom-up."},
	{MethodRunRedirect, "run", "Send a directive, resuming the target if paused."},
	{MethodTaskStart, "task", "Create a delegated task and return immediately."},
	{MethodTaskStatus, "task", "Status, current step, plan progress, live tool line."},
	{MethodTaskWait, "task", "Wait for completion or for a pending answer."},
	{MethodTaskResult, "task", "Delivery report: summary, files, branch/diff, criteria, pending."},
	{MethodTaskReply, "task", "Answer a pending question or add a directive."},
	{MethodTaskCancel, "task", "Cancel a delegated task."},
	{MethodTaskPause, "task", "Pause a delegated task."},
	{MethodTaskResume, "task", "Resume a delegated task."},
	{MethodTaskList, "task", "List tasks, optionally filtered by project."},
	{MethodTaskLogs, "task", "Live event stream of a task as JSONL."},
	{MethodProjectList, "project", "List registered projects."},
	{MethodProjectRegister, "project", "Register a project with roots and settings."},
	{MethodProjectGet, "project", "Describe one project."},
	{MethodPlanApprove, "plan", "Approve a pending macro plan."},
	{MethodPlanDeny, "plan", "Reject a pending macro plan with a reason."},
	{MethodMemoryQuery, "memory", "Query memories within the allowed scopes."},
	{MethodMemoryCorrect, "memory", "Correct a memory from a client."},
	{MethodMemoryProvenance, "memory", "Inspect memory provenance."},
	{MethodSkillsSearch, "skills", "Search skills by text and group."},
	{MethodSkillsLoad, "skills", "Load a SKILL.md body and resolve deferred tools."},
	{MethodSkillsInstall, "skills", "Install a skill from the store or a path."},
	{MethodSkillsGroups, "skills", "List skill groups with localized labels and effective counts."},
	{MethodSkillsList, "skills", "List the effective skill catalog with filters."},
	{MethodSkillsGet, "skills", "Read a complete skill source and diagnostics."},
	{MethodSkillsUpdate, "skills", "Validate and update a skill with a backup."},
	{MethodSkillsSetState, "skills", "Enable or disable an indexed skill."},
	{MethodSkillsDelete, "skills", "Back up and remove a writable skill."},
	{MethodUsageSummary, "usage", "Aggregate recorded token, cost and execution usage."},
	{MethodCapabilityRegister, "capability", "Register an app or extension manifest."},
	{MethodCapabilityInvoke, "capability", "Invoke a capability operation."},
	{MethodCapabilityList, "capability", "List registered capabilities."},
	{MethodVaultCreate, "vault", "Create a secret entry (separate secure call)."},
	{MethodVaultUpdate, "vault", "Update a secret entry value or policy."},
	{MethodVaultListMetadata, "vault", "List secret metadata, never values."},
	{MethodVaultLock, "vault", "Lock the vault."},
	{MethodVaultUnlock, "vault", "Unlock the vault."},
	{MethodVaultDelete, "vault", "Delete a secret entry after confirmation."},
	{MethodQuestionAnswer, "question", "Answer a pending blocking or non-blocking question."},
	{MethodMetricsQuery, "metrics", "Query token, cost and execution metrics."},
	{MethodFilesList, "files", "Lazy paginated file listing honoring .gitignore."},
	{MethodConfigGet, "config", "Read configuration values."},
	{MethodConfigSet, "config", "Write configuration values."},
	{MethodConfigList, "config", "List configuration keys."},
}

// ---- Conversation ----

// ConversationSendParams carries one client message. ClientMsgID is
// generated by the client; the gate ignores duplicates with the same id
// and returns the already assigned MsgID (Section 3.5).
type ConversationSendParams struct {
	ConversationID string   `json:"conversation_id"`
	ClientMsgID    string   `json:"client_msg_id"`
	Text           string   `json:"text"`
	Attachments    []string `json:"attachments,omitempty"`
	ReplyTo        *string  `json:"reply_to,omitempty"`
}

// ConversationSendResult is the submission receipt.
type ConversationSendResult struct {
	MsgID string `json:"msg_id"`
}

// ConversationSubscribeParams opens an event subscription from a cursor.
type ConversationSubscribeParams struct {
	Cursor string   `json:"cursor,omitempty"`
	Types  []string `json:"types,omitempty"`
}

// ConversationHistoryParams pages message history.
type ConversationHistoryParams struct {
	ConversationID string `json:"conversation_id"`
	Before         string `json:"before,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

// ---- Run control ----

// RunTarget addresses one run or, by name, a subtree.
type RunTarget struct {
	RunID *types.RunID `json:"run_id,omitempty"`
	Name  *string      `json:"name,omitempty"`
}

// RunStatusResult is the deterministic snapshot of Section 15.6.
type RunStatusResult struct {
	RunID         string         `json:"run_id"`
	State         types.RunState `json:"state"`
	Reason        string         `json:"reason,omitempty"`
	QueuePosition int            `json:"queue_position,omitempty"`
	CurrentStep   string         `json:"current_step,omitempty"`
	ToolInFlight  string         `json:"tool_in_flight,omitempty"`
	LiveOutput    string         `json:"live_output,omitempty"`
	LastProgress  string         `json:"last_progress,omitempty"`
	PlanDone      int            `json:"plan_done"`
	PlanTotal     int            `json:"plan_total"`
	BudgetUsedPct float64        `json:"budget_used_pct"`
}

// RunRedirectParams carries a directive to a running subtree.
type RunRedirectParams struct {
	Target    RunTarget `json:"target"`
	Text      string    `json:"text"`
	Interrupt bool      `json:"interrupt"`
}

// ---- Task delegation (Section 3.6) ----

// TaskStartParams mirrors `excalibur task start`.
type TaskStartParams struct {
	Request     string   `json:"request"`
	ProjectDir  string   `json:"project_dir,omitempty"`
	Profile     string   `json:"profile,omitempty"`
	NoMemory    bool     `json:"no_memory,omitempty"`
	Worktree    *bool    `json:"worktree,omitempty"`
	BudgetUSD   *float64 `json:"budget_usd,omitempty"`
	MaxSteps    *int     `json:"max_steps,omitempty"`
	TimeoutSecs *int64   `json:"timeout_s,omitempty"`
}

// TaskStartResult returns the id immediately.
type TaskStartResult struct {
	TaskID types.TaskID    `json:"task_id"`
	Status types.TaskState `json:"status"`
}

// TaskWaitParams bounds one wait call; questions return early.
type TaskWaitParams struct {
	TaskID      types.TaskID `json:"task_id"`
	TimeoutSecs int64        `json:"timeout_s,omitempty"`
}

// TaskReplyParams answers a question or adds a directive.
type TaskReplyParams struct {
	TaskID types.TaskID `json:"task_id"`
	Text   string       `json:"text"`
}

// TaskLogsParams streams live events as JSONL.
type TaskLogsParams struct {
	TaskID types.TaskID `json:"task_id"`
	Follow bool         `json:"follow,omitempty"`
}

// ---- Projects ----

// ProjectRegisterParams registers a project (Section 9.2).
type ProjectRegisterParams struct {
	Name        string   `json:"name"`
	Alias       string   `json:"alias,omitempty"`
	Description string   `json:"description,omitempty"`
	Roots       []string `json:"roots"`
}

// ---- Plans ----

// PlanDecisionParams resolves a pending plan approval.
type PlanDecisionParams struct {
	PlanID string `json:"plan_id"`
	Reason string `json:"reason,omitempty"`
}

// ---- Memory ----

// MemoryQueryParams requires an explicit scope set (Section 9.5).
type MemoryQueryParams struct {
	Query  string   `json:"query"`
	Scopes []string `json:"scopes"`
	Limit  int      `json:"limit,omitempty"`
}

// ---- Skills ----

// SkillsSearchParams searches the skill index.
type SkillsSearchParams struct {
	Query string `json:"query"`
	Group string `json:"group,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// ---- Capabilities (Section 14.12) ----

// CapabilityInvokeParams invokes one capability operation.
type CapabilityInvokeParams struct {
	Capability     string         `json:"capability"`
	Operation      string         `json:"operation"`
	Args           map[string]any `json:"args,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

// ---- Vault (Section 20.1) ----

// VaultEntryParams creates or updates a secret entry. The value travels
// only in this dedicated call, never in conversation messages.
type VaultEntryParams struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Value        string            `json:"value,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Scopes       []string          `json:"scopes,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
}

// ---- Questions ----

// QuestionAnswerParams answers a pending question.
type QuestionAnswerParams struct {
	QuestionID string `json:"question_id"`
	Answer     string `json:"answer"`
}

// ---- Metrics ----

// MetricsQueryParams scopes a metrics query to a task, project, user or
// time window (Section 22.2).
type MetricsQueryParams struct {
	TaskID    *string    `json:"task_id,omitempty"`
	ProjectID *string    `json:"project_id,omitempty"`
	Since     *time.Time `json:"since,omitempty"`
	Until     *time.Time `json:"until,omitempty"`
}

// ---- Files ----

// FilesListParams pages a lazy listing that honors .gitignore,
// collapses heavy directories and can filter to agent-touched files.
type FilesListParams struct {
	ProjectID    string `json:"project_id"`
	Path         string `json:"path,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	AgentTouched bool   `json:"agent_touched,omitempty"`
}

// ---- Config ----

// ConfigSetParams writes configuration values.
type ConfigSetParams struct {
	Values map[string]any `json:"values"`
}

// PrincipalGetResult is the local user's read-only profile.
type PrincipalGetResult struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	Language string `json:"language"`
}

// Whiteboard methods are additive conversation drawing operations.
const (
	MethodWhiteboardList     = "whiteboard.list"
	MethodWhiteboardGet      = "whiteboard.get"
	MethodWhiteboardApply    = "whiteboard.apply"
	MethodWhiteboardSnapshot = "whiteboard.snapshot"
	MethodWhiteboardClose    = "whiteboard.close"
)
