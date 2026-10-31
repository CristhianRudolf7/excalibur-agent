package types

// Typed internal identifiers. Each kind has its own string type so that
// mixing, say, a task id with a run id is a compile-time error. Models
// always see these full ULIDs or the short per-project codes, never list
// positions (Section 4.4).

// ID constructors and validators. One group per identifier kind.

type ProjectID string

// NewProjectID returns a fresh project identifier.
func NewProjectID() ProjectID { return ProjectID(NewID(PrefixProject)) }

// Validate reports whether id is a well-formed project identifier.
func (id ProjectID) Validate() error { return ValidateID(string(id), PrefixProject) }
func (id ProjectID) String() string  { return string(id) }

type ConversationID string

// NewConversationID returns a fresh conversation identifier.
func NewConversationID() ConversationID { return ConversationID(NewID(PrefixConversation)) }

// Validate reports whether id is a well-formed conversation identifier.
func (id ConversationID) Validate() error { return ValidateID(string(id), PrefixConversation) }
func (id ConversationID) String() string  { return string(id) }

type MessageID string

// NewMessageID returns a fresh message identifier.
func NewMessageID() MessageID { return MessageID(NewID(PrefixMessage)) }

// Validate reports whether id is a well-formed message identifier.
func (id MessageID) Validate() error { return ValidateID(string(id), PrefixMessage) }
func (id MessageID) String() string  { return string(id) }

type TaskID string

// NewTaskID returns a fresh task identifier.
func NewTaskID() TaskID { return TaskID(NewID(PrefixTask)) }

// Validate reports whether id is a well-formed task identifier.
func (id TaskID) Validate() error { return ValidateID(string(id), PrefixTask) }
func (id TaskID) String() string  { return string(id) }

type RunID string

// NewRunID returns a fresh run identifier.
func NewRunID() RunID { return RunID(NewID(PrefixRun)) }

// Validate reports whether id is a well-formed run identifier.
func (id RunID) Validate() error { return ValidateID(string(id), PrefixRun) }
func (id RunID) String() string  { return string(id) }

type AgentID string

// NewAgentID returns a fresh agent identifier.
func NewAgentID() AgentID { return AgentID(NewID(PrefixAgent)) }

// Validate reports whether id is a well-formed agent identifier.
func (id AgentID) Validate() error { return ValidateID(string(id), PrefixAgent) }
func (id AgentID) String() string  { return string(id) }

type RequirementID string

// NewRequirementID returns a fresh requirement identifier.
func NewRequirementID() RequirementID { return RequirementID(NewID(PrefixRequirement)) }

// Validate reports whether id is a well-formed requirement identifier.
func (id RequirementID) Validate() error { return ValidateID(string(id), PrefixRequirement) }
func (id RequirementID) String() string  { return string(id) }

type CriterionID string

// NewCriterionID returns a fresh criterion identifier.
func NewCriterionID() CriterionID { return CriterionID(NewID(PrefixCriterion)) }

// Validate reports whether id is a well-formed criterion identifier.
func (id CriterionID) Validate() error { return ValidateID(string(id), PrefixCriterion) }
func (id CriterionID) String() string  { return string(id) }

type CheckID string

// NewCheckID returns a fresh check identifier.
func NewCheckID() CheckID { return CheckID(NewID(PrefixCheck)) }

// Validate reports whether id is a well-formed check identifier.
func (id CheckID) Validate() error { return ValidateID(string(id), PrefixCheck) }
func (id CheckID) String() string  { return string(id) }

type EvidenceID string

// NewEvidenceID returns a fresh evidence identifier.
func NewEvidenceID() EvidenceID { return EvidenceID(NewID(PrefixEvidence)) }

// Validate reports whether id is a well-formed evidence identifier.
func (id EvidenceID) Validate() error { return ValidateID(string(id), PrefixEvidence) }
func (id EvidenceID) String() string  { return string(id) }

type MemoryID string

// NewMemoryID returns a fresh memory identifier.
func NewMemoryID() MemoryID { return MemoryID(NewID(PrefixMemory)) }

// Validate reports whether id is a well-formed memory identifier.
func (id MemoryID) Validate() error { return ValidateID(string(id), PrefixMemory) }
func (id MemoryID) String() string  { return string(id) }

type SkillID string

// NewSkillID returns a fresh skill identifier.
func NewSkillID() SkillID { return SkillID(NewID(PrefixSkill)) }

// Validate reports whether id is a well-formed skill identifier.
func (id SkillID) Validate() error { return ValidateID(string(id), PrefixSkill) }
func (id SkillID) String() string  { return string(id) }

type JobID string

// NewJobID returns a fresh job identifier.
func NewJobID() JobID { return JobID(NewID(PrefixJob)) }

// Validate reports whether id is a well-formed job identifier.
func (id JobID) Validate() error { return ValidateID(string(id), PrefixJob) }
func (id JobID) String() string  { return string(id) }
