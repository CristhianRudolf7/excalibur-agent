package config

import "time"

// Config is the layered harness configuration. Zero values are never
// meaningful: Default fills every field with the Appendix B initial
// value, and later milestones overlay file, environment and flag
// layers on top.
type Config struct {
	Master        MasterConfig        `json:"master"`
	Engine        EngineConfig        `json:"engine"`
	Porter        PorterConfig        `json:"porter"`
	Provider      ProviderConfig      `json:"provider"`
	Context       ContextConfig       `json:"context"`
	Memory        MemoryConfig        `json:"memory"`
	Tools         ToolsConfig         `json:"tools"`
	Notifications NotificationsConfig `json:"notifications"`
	Vault         VaultConfig         `json:"vault"`
	Resources     ResourcesConfig     `json:"resources"`
}

// MasterConfig governs the master turn and reception (Sections 5.3,
// 6.2).
type MasterConfig struct {
	// MaxSteps caps one master turn (Appendix B: 8).
	MaxSteps int `json:"max_steps"`
	// RegenCancelWindow: a new message within this window cancels an
	// unshown generation and restarts it (Appendix B: 2s).
	RegenCancelWindow time.Duration `json:"regen_cancel_window"`
	// NoticeGroupWindow groups normal notices (Appendix B: 5s).
	NoticeGroupWindow time.Duration `json:"notice_group_window"`
}

// EngineConfig governs runs and subagents (Sections 7.3, 15.3, 15.9).
type EngineConfig struct {
	// ChildMaxSteps caps a child run (Appendix B: 60).
	ChildMaxSteps int `json:"child_max_steps"`
	// ParentMaxSteps caps a parent run (Appendix B: 150).
	ParentMaxSteps int `json:"parent_max_steps"`
	// MaxActiveChildren caps active children per parent (Appendix B: 4).
	MaxActiveChildren int `json:"max_active_children"`
	// MaxRunningPerUser caps runs in running state per user (App. B: 8).
	MaxRunningPerUser int `json:"max_running_per_user"`
	// CloseThreshold starts closing mode below this budget share (10%).
	CloseThreshold float64 `json:"close_threshold"`
	// HeartbeatTimeout declares a run stuck after this silence (10m).
	HeartbeatTimeout time.Duration `json:"heartbeat_timeout"`
	// JournalCheckpointEvery checkpoints the journal (Appendix B: 20).
	JournalCheckpointEvery int `json:"journal_checkpoint_every"`
	// AskBeforeAssumeBelow asks when the top interpretation is below
	// this probability (Appendix B: 0.7).
	AskBelowProb float64 `json:"ask_below_prob"`
}

// PorterConfig governs the SQLite porter (Section 4.3).
type PorterConfig struct {
	// GroupCommitWait caps group-commit batching (Appendix B: 5ms).
	GroupCommitWait time.Duration `json:"group_commit_wait"`
	// GroupCommitMaxOps caps one group commit (Appendix B: 200).
	GroupCommitMaxOps int `json:"group_commit_max_ops"`
	// BusyTimeout is the SQLite busy handler ceiling (App. B: 5s).
	BusyTimeout time.Duration `json:"busy_timeout"`
	// BusyRetries bounds external-lock retries (Appendix B: 3).
	BusyRetries int `json:"busy_retries"`
	// WALAlertBytes warns above this WAL size (Appendix B: 256MiB).
	WALAlertBytes int64 `json:"wal_alert_bytes"`
	// ReadPoolSize caps pooled readers; 0 means min(4, NumCPU).
	ReadPoolSize int `json:"read_pool_size"`
}

// ProviderConfig governs failover (Section 19.5).
type ProviderConfig struct {
	// BreakerFailures opens the breaker after this many consecutive
	// failures (Appendix B: 5).
	BreakerFailures int `json:"breaker_failures"`
	// BreakerErrorRate opens the breaker above this 1-minute error rate
	// (Appendix B: 0.5).
	BreakerErrorRate float64 `json:"breaker_error_rate"`
	// BreakerOpenFor keeps the breaker open (Appendix B: 60s).
	BreakerOpenFor time.Duration `json:"breaker_open_for"`
}

// ContextConfig governs the context compiler (Section 11).
type ContextConfig struct {
	// CompactionCeiling bounds compaction triggering (Appendix B:
	// min(500000, 0.85*window - reserve)).
	CompactionCeilingTokens int `json:"compaction_ceiling_tokens"`
	// PostCompactionLiteral keeps this literal tail (App. B: 20K).
	PostCompactionLiteralTokens int `json:"post_compaction_literal_tokens"`
	// LiteralObservations pins the newest observations (App. B: 10).
	LiteralObservations int `json:"literal_observations"`
	// MaskTriggerBytes starts masking above this unmasked total (24K).
	MaskTriggerTokens int `json:"mask_trigger_tokens"`
	// MaskTargetBytes compacts masking toward this total (12K).
	MaskTargetTokens int `json:"mask_target_tokens"`
	// MasterHistoryTokens caps literal master history (App. B: 12K,
	// summarized past 24K).
	MasterHistoryTokens int `json:"master_history_tokens"`
	// TargetTokens aim the compiler per role (Appendix B ranges).
	MasterTargetMin int `json:"master_target_min"`
	MasterTargetMax int `json:"master_target_max"`
	ChildTargetMax  int `json:"child_target_max"`
	ParentTargetMax int `json:"parent_target_max"`
}

// MemoryConfig governs retrieval and consolidation (Section 10).
type MemoryConfig struct {
	// ExactDuplicateAt treats similarity at/above as duplicate (0.95).
	ExactDuplicateAt float64 `json:"exact_duplicate_at"`
	// AmbiguousBandLo opens the ambiguous band (Appendix B: 0.80).
	AmbiguousBandLo float64 `json:"ambiguous_band_lo"`
	// RRFFusedK is the RRF constant (Appendix B: k=60).
	RRFK int `json:"rrf_k"`
	// CandidatesPerList caps each retrieval list (Appendix B: 50).
	CandidatesPerList int `json:"candidates_per_list"`
	// ConsolidationIdleDays triggers batch extraction (Appendix B: 5).
	ConsolidationIdleDays int `json:"consolidation_idle_days"`
	// EmbeddingDimensions sizes embeddings (Appendix B: 512 int8).
	EmbeddingDimensions int `json:"embedding_dimensions"`
}

// ToolsConfig governs the tool pipeline (Section 13).
type ToolsConfig struct {
	// OutputBudgetBytes caps head+tail tool output (Appendix B: ~10K
	// tokens; stored as bytes here).
	OutputBudgetBytes int64 `json:"output_budget_bytes"`
	// ReadWindowLines sizes read_file windows (Appendix B: 200).
	ReadWindowLines int `json:"read_window_lines"`
	// DefaultTimeout bounds tool calls (Appendix B default: 60s;
	// exec yield 10s is H3 behavior, not config).
	DefaultTimeout time.Duration `json:"default_timeout"`
	// MaxSessionsPerRun caps terminal sessions (Appendix B: 16).
	MaxSessionsPerRun int `json:"max_sessions_per_run"`
	// MaxSessionsGlobal caps sessions daemon-wide (Appendix B: 64).
	MaxSessionsGlobal int `json:"max_sessions_global"`
	// LiveTailLines caps live output (Appendix B: 20 lines, 2KB).
	LiveTailLines int `json:"live_tail_lines"`
	// LiveTailBytes caps live output bytes (Appendix B: 2KB).
	LiveTailBytes int64 `json:"live_tail_bytes"`
}

// NotificationsConfig governs notice delivery (Section 5.4).
type NotificationsConfig struct {
	// UserActiveWindow links urgent notices across conversations
	// (Appendix B: 10m).
	UserActiveWindow time.Duration `json:"user_active_window"`
}

// VaultConfig governs the secrets vault (Section 20.1).
type VaultConfig struct {
	// Argon2MemoryKB sizes passphrase KDF memory (Appendix B: 64MiB).
	Argon2MemoryKB uint32 `json:"argon2_memory_kb"`
	// Argon2Iterations sets KDF time cost (Appendix B: 3).
	Argon2Iterations uint32 `json:"argon2_iterations"`
	// Argon2Parallelism sets KDF parallelism (Appendix B: 4).
	Argon2Parallelism uint8 `json:"argon2_parallelism"`
	// Argon2SaltLen sets the KDF salt length (Appendix B: 16).
	Argon2SaltLen uint32 `json:"argon2_salt_len"`
	// PassphraseLockAfter auto-locks passphrase vaults (App. B: 12h).
	PassphraseLockAfter time.Duration `json:"passphrase_lock_after"`
}

// ResourcesConfig governs admission and watchdogs (Sections 3.2, 5.2).
type ResourcesConfig struct {
	// ReservedCPUs keeps cores for the interactive path (App. B: 2).
	ReservedCPUs int `json:"reserved_cpus"`
	// ResumeBelowFrac re-admits below this RSS fraction (App. B: 0.8).
	ResumeBelowFrac float64 `json:"resume_below_frac"`
	// ResumeStableFor re-admits after this stability (Appendix B: 60s).
	ResumeStableFor time.Duration `json:"resume_stable_for"`
	// SafeModeTrips enters safe mode (Appendix B: 3 crashes/10m).
	SafeModeTrips int `json:"safe_mode_trips"`
	// SafeModeWindow sizes the crash window (Appendix B: 10m).
	SafeModeWindow time.Duration `json:"safe_mode_window"`
	// SafeModeCooldown exits safe mode (Appendix B: 30m calm).
	SafeModeCooldown time.Duration `json:"safe_mode_cooldown"`
}

// Default returns the Appendix B initial values.
func Default() Config {
	return Config{
		Master: MasterConfig{
			MaxSteps:          8,
			RegenCancelWindow: 2 * time.Second,
			NoticeGroupWindow: 5 * time.Second,
		},
		Engine: EngineConfig{
			ChildMaxSteps:          60,
			ParentMaxSteps:         150,
			MaxActiveChildren:      4,
			MaxRunningPerUser:      8,
			CloseThreshold:         0.10,
			HeartbeatTimeout:       10 * time.Minute,
			JournalCheckpointEvery: 20,
			AskBelowProb:           0.7,
		},
		Porter: PorterConfig{
			GroupCommitWait:   5 * time.Millisecond,
			GroupCommitMaxOps: 200,
			BusyTimeout:       5 * time.Second,
			BusyRetries:       3,
			WALAlertBytes:     256 << 20,
			ReadPoolSize:      0,
		},
		Provider: ProviderConfig{
			BreakerFailures:  5,
			BreakerErrorRate: 0.5,
			BreakerOpenFor:   60 * time.Second,
		},
		Context: ContextConfig{
			CompactionCeilingTokens:     500000,
			PostCompactionLiteralTokens: 20000,
			LiteralObservations:         10,
			MaskTriggerTokens:           24000,
			MaskTargetTokens:            12000,
			MasterHistoryTokens:         12000,
			MasterTargetMin:             8000,
			MasterTargetMax:             24000,
			ChildTargetMax:              32000,
			ParentTargetMax:             48000,
		},
		Memory: MemoryConfig{
			ExactDuplicateAt:      0.95,
			AmbiguousBandLo:       0.80,
			RRFK:                  60,
			CandidatesPerList:     50,
			ConsolidationIdleDays: 5,
			EmbeddingDimensions:   512,
		},
		Tools: ToolsConfig{
			OutputBudgetBytes: 40000,
			ReadWindowLines:   200,
			DefaultTimeout:    60 * time.Second,
			MaxSessionsPerRun: 16,
			MaxSessionsGlobal: 64,
			LiveTailLines:     20,
			LiveTailBytes:     2048,
		},
		Notifications: NotificationsConfig{
			UserActiveWindow: 10 * time.Minute,
		},
		Vault: VaultConfig{
			Argon2MemoryKB:      64 * 1024,
			Argon2Iterations:    3,
			Argon2Parallelism:   4,
			Argon2SaltLen:       16,
			PassphraseLockAfter: 12 * time.Hour,
		},
		Resources: ResourcesConfig{
			ReservedCPUs:     2,
			ResumeBelowFrac:  0.8,
			ResumeStableFor:  60 * time.Second,
			SafeModeTrips:    3,
			SafeModeWindow:   10 * time.Minute,
			SafeModeCooldown: 30 * time.Minute,
		},
	}
}

// Validate checks value ranges.
func (c Config) Validate() error {
	return validateStruct(c)
}
