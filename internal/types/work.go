package types

import "time"

// Core planning and execution structs. Column sources are Appendix A;
// every table also carries created_at, updated_at and, where relevant,
// scope_id and principal_id, which the store layer owns.

// TaskLevel distinguishes conversation (N0), short task (N1), project
// task (N2) and long initiative (N3). N0 produces messages only and has
// no Task row (Section 8.1).
type TaskLevel string

// Task levels.
const (
	TaskN1 TaskLevel = "n1"
	TaskN2 TaskLevel = "n2"
	TaskN3 TaskLevel = "n3"
)

// RequirementType classifies a requirement (Section 6.3).
type RequirementType string

// Requirement types.
const (
	RequirementFunctional    RequirementType = "functional"
	RequirementNonFunctional RequirementType = "non_functional"
	RequirementConstraint    RequirementType = "constraint"
)

// Priority is must/should/could (Section 8.2).
type Priority string

// Priorities.
const (
	PriorityMust   Priority = "must"
	PriorityShould Priority = "should"
	PriorityCould  Priority = "could"
)

// RequirementState tracks requirement lifecycle (Section 8.2).
type RequirementState string

// Requirement states.
const (
	RequirementProposed RequirementState = "proposed"
	RequirementAccepted RequirementState = "accepted"
	RequirementRetired  RequirementState = "retired"
)

// CriterionMethod is how a criterion is verified (Sections 6.3, 8.2).
type CriterionMethod string

// Criterion methods.
const (
	CriterionCommand          CriterionMethod = "command"
	CriterionTest             CriterionMethod = "test"
	CriterionInspection       CriterionMethod = "inspection"
	CriterionMetric           CriterionMethod = "metric"
	CriterionUserConfirmation CriterionMethod = "user_confirmation"
)

// EvidenceResult is the recorded outcome of a check (Section 8.2).
type EvidenceResult string

// Evidence outcomes.
const (
	EvidencePass  EvidenceResult = "pass"
	EvidenceFail  EvidenceResult = "fail"
	EvidenceError EvidenceResult = "error"
)

// PlanType distinguishes macro plans from per-task micro plans.
type PlanType string

// Plan types.
const (
	PlanMacro PlanType = "macro"
	PlanMicro PlanType = "micro"
)

// PlanItemState tracks a plan step (Section 8.2).
type PlanItemState string

// Plan item states.
const (
	PlanItemPending    PlanItemState = "pending"
	PlanItemInProgress PlanItemState = "in_progress"
	PlanItemBlocked    PlanItemState = "blocked"
	PlanItemDone       PlanItemState = "done"
	PlanItemCanceled   PlanItemState = "canceled"
)

// DependencyType links plan items (Section 8.2).
type DependencyType string

// Dependency types.
const (
	DependencyBlocks  DependencyType = "blocks"
	DependencyInforms DependencyType = "informs"
)

// NoticeClass groups notification urgency (Section 5.4).
type NoticeClass string

// Notice classes.
const (
	NoticeUrgent      NoticeClass = "urgent"
	NoticeNormal      NoticeClass = "normal"
	NoticeInformative NoticeClass = "informative"
)

// RequestFile is the request record every task carries: the literal
// source messages, the interpretation, requirements, criteria,
// assumptions, open questions and pinned instructions (Section 6.3).
type RequestFile struct {
	Original           string   `json:"original"`
	OriginalMessageIDs []string `json:"original_message_ids"`
	Interpretation     string   `json:"interpretation"`
	Requirements       []string `json:"requirements"`
	Criteria           []string `json:"criteria"`
	Assumptions        []string `json:"assumptions"`
	OpenQuestions      []string `json:"open_questions"`
	PinnedInstructions []string `json:"pinned_instructions"`
}

// Budget caps a run or task: steps, tokens, cost, time and processes
// (Sections 7.3, 15.9).
type Budget struct {
	MaxSteps   int     `json:"max_steps"`
	MaxTokens  int     `json:"max_tokens"`
	MaxCostUSD float64 `json:"max_cost_usd"`
	MaxSeconds int64   `json:"max_seconds"`
	MaxProcs   int     `json:"max_procs"`
}

// Task is the unit of work the user asked for (Section 8.2).
type Task struct {
	ID             TaskID         `json:"id"`
	Code           TaskRef        `json:"code"`
	Scope          Scope          `json:"scope"`
	Level          TaskLevel      `json:"level"`
	Request        RequestFile    `json:"request"`
	ConversationID ConversationID `json:"conversation_id"`
	NoticePolicy   NoticeClass    `json:"notice_policy"`
	RootRunID      *RunID         `json:"root_run_id,omitempty"`
	Budget         Budget         `json:"budget"`
	State          TaskState      `json:"state"`
	Version        int            `json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Requirement is a single extracted requirement (Section 8.2).
type Requirement struct {
	ID        RequirementID    `json:"id"`
	Code      RequirementRef   `json:"code"`
	Scope     Scope            `json:"scope"`
	Text      string           `json:"text"`
	OriginMsg *MessageID       `json:"origin_msg,omitempty"`
	Type      RequirementType  `json:"type"`
	Priority  Priority         `json:"priority"`
	State     RequirementState `json:"state"`
	Version   int              `json:"version"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// Criterion is an observable acceptance criterion with a verification
// method and an impact area: path or resource patterns whose change
// invalidates its evidence (Sections 8.2, 8.3).
type Criterion struct {
	ID            CriterionID     `json:"id"`
	Code          CriterionRef    `json:"code"`
	RequirementID RequirementID   `json:"requirement_id"`
	Statement     string          `json:"statement"`
	Method        CriterionMethod `json:"method"`
	Expected      string          `json:"expected"`
	ImpactArea    []string        `json:"impact_area"`
	State         CriterionState  `json:"state"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Check is the executable definition behind a criterion (Section 8.2).
type Check struct {
	ID          CheckID     `json:"id"`
	CriterionID CriterionID `json:"criterion_id"`
	// Definition is the executable form: command, test, script, HTTP
	// probe or file/snapshot assertion, plus its environment.
	Definition  string    `json:"definition"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Evidence records one check execution: outcome, output blob, code
// version, fingerprints and the producing tool call (Section 8.2, 8.6).
type Evidence struct {
	ID           EvidenceID        `json:"id"`
	CheckID      CheckID           `json:"check_id"`
	RunID        RunID             `json:"run_id"`
	Result       EvidenceResult    `json:"result"`
	OutputBlob   string            `json:"output_blob"`
	CodeVersion  string            `json:"code_version"`
	Fingerprints map[string]string `json:"fingerprints"`
	ToolCallID   string            `json:"tool_call_id"`
	CreatedAt    time.Time         `json:"created_at"`
}

// Run is one agent working on one task: the execution instance whose
// lifecycle Section 7.2 governs.
type Run struct {
	ID                  RunID        `json:"id"`
	AgentID             AgentID      `json:"agent_id"`
	TaskID              TaskID       `json:"task_id"`
	State               RunState     `json:"state"`
	WaitReason          *WaitReason  `json:"wait_reason,omitempty"`
	PauseReason         *PauseReason `json:"pause_reason,omitempty"`
	BlockReason         *BlockReason `json:"block_reason,omitempty"`
	PausedBy            string       `json:"paused_by,omitempty"`
	QueuePosition       int          `json:"queue_position"`
	Budget              Budget       `json:"budget"`
	HeartbeatAt         time.Time    `json:"heartbeat_at"`
	LastProgress        string       `json:"last_progress"`
	InstructionsRevSeen int          `json:"instructions_rev_seen"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

// Agent has a readable name, an id, a role description, profile, parent,
// level and scope (Section 15.2).
type Agent struct {
	ID          AgentID   `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Profile     string    `json:"profile"`
	ParentID    *AgentID  `json:"parent_id,omitempty"`
	Level       int       `json:"level"`
	Scope       Scope     `json:"scope"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Question is a pending user question or plan approval (Sections 6.4,
// 15.5).
type Question struct {
	ID        string    `json:"id"`
	RunID     RunID     `json:"run_id"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Blocking  bool      `json:"blocking"`
	Default   string    `json:"default"`
	Deadline  time.Time `json:"deadline"`
	State     string    `json:"state"`
	Answer    string    `json:"answer,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Notification is a posted run notice with per-channel delivery state
// (Section 5.4).
type Notification struct {
	ID        string      `json:"id"`
	TaskID    TaskID      `json:"task_id"`
	RunID     RunID       `json:"run_id"`
	Class     NoticeClass `json:"class"`
	Content   string      `json:"content"`
	Channels  []string    `json:"channels"`
	State     string      `json:"state"`
	Acked     bool        `json:"acked"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// JournalEntry is one durable journal record (Section 17.1).
type JournalEntry struct {
	Seq            uint64    `json:"seq"`
	RunID          RunID     `json:"run_id"`
	Type           string    `json:"type"`
	Payload        []byte    `json:"payload,omitempty"`
	BlobRef        string    `json:"blob_ref,omitempty"`
	State          string    `json:"state"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Instruction is a durable literal instruction with a revision number
// (Section 15.7).
type Instruction struct {
	ID        string    `json:"id"`
	Scope     Scope     `json:"scope"`
	Text      string    `json:"text"`
	Revision  int       `json:"revision"`
	Origin    string    `json:"origin"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Assumption and Decision mirror the Appendix A tables.
type Assumption struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Reason    string    `json:"reason"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Decision is an architecture decision with motive and discarded
// alternatives (Section 8.2, also used for docs/decisions records).
type Decision struct {
	ID           string    `json:"id"`
	Statement    string    `json:"statement"`
	Reason       string    `json:"reason"`
	Alternatives []string  `json:"alternatives"`
	State        string    `json:"state"`
	ReplacedBy   string    `json:"replaced_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
