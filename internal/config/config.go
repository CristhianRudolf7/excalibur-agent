package config

import "time"

// Config is the layered harness configuration. Zero values are never
// meaningful: Default fills every field with the Appendix B initial
// value, and later milestones overlay file, environment and flag
// layers on top.
type Config struct {
	Robot         RobotConfig         `json:"robot" toml:"robot"`
	Voice         VoiceConfig         `json:"voice" toml:"voice"`
	Master        MasterConfig        `json:"master" toml:"master"`
	Engine        EngineConfig        `json:"engine" toml:"engine"`
	Porter        PorterConfig        `json:"porter" toml:"porter"`
	Provider      ProviderConfig      `json:"provider" toml:"provider"`
	Context       ContextConfig       `json:"context" toml:"context"`
	Memory        MemoryConfig        `json:"memory" toml:"memory"`
	Tools         ToolsConfig         `json:"tools" toml:"tools"`
	Notifications NotificationsConfig `json:"notifications" toml:"notifications"`
	Vault         VaultConfig         `json:"vault" toml:"vault"`
	Resources     ResourcesConfig     `json:"resources" toml:"resources"`
	Models        ModelsConfig        `json:"models" toml:"models"`
	Browser       BrowserConfig       `json:"browser" toml:"browser"`
	Search        SearchConfig        `json:"search" toml:"search"`
}

// MasterConfig governs the master turn and reception (Sections 5.3,
// 6.2).
type MasterConfig struct {
	// MaxSteps caps one master turn (Appendix B: 8).
	MaxSteps int `json:"max_steps" toml:"max_steps"`
	// RegenCancelWindow: a new message within this window cancels an
	// unshown generation and restarts it (Appendix B: 2s).
	RegenCancelWindow time.Duration `json:"regen_cancel_window" toml:"regen_cancel_window"`
	// NoticeGroupWindow groups normal notices (Appendix B: 5s).
	NoticeGroupWindow time.Duration `json:"notice_group_window" toml:"notice_group_window"`
}

// EngineConfig governs runs and subagents (Sections 7.3, 15.3, 15.9).
type EngineConfig struct {
	// ChildMaxSteps caps a child run (Appendix B: 60).
	ChildMaxSteps int `json:"child_max_steps" toml:"child_max_steps"`
	// ParentMaxSteps caps a parent run (Appendix B: 150).
	ParentMaxSteps int `json:"parent_max_steps" toml:"parent_max_steps"`
	// MaxActiveChildren caps active children per parent (Appendix B: 4).
	MaxActiveChildren int `json:"max_active_children" toml:"max_active_children"`
	// MaxRunningPerUser caps runs in running state per user (App. B: 8).
	MaxRunningPerUser int `json:"max_running_per_user" toml:"max_running_per_user"`
	// CloseThreshold starts closing mode below this budget share (10%).
	CloseThreshold float64 `json:"close_threshold" toml:"close_threshold"`
	// HeartbeatTimeout declares a run stuck after this silence (10m).
	HeartbeatTimeout time.Duration `json:"heartbeat_timeout" toml:"heartbeat_timeout"`
	// JournalCheckpointEvery checkpoints the journal (Appendix B: 20).
	JournalCheckpointEvery int `json:"journal_checkpoint_every" toml:"journal_checkpoint_every"`
	// AskBeforeAssumeBelow asks when the top interpretation is below
	// this probability (Appendix B: 0.7).
	AskBelowProb float64 `json:"ask_below_prob" toml:"ask_below_prob"`
}

// PorterConfig governs the SQLite porter (Section 4.3).
type PorterConfig struct {
	// GroupCommitWait caps group-commit batching (Appendix B: 5ms).
	GroupCommitWait time.Duration `json:"group_commit_wait" toml:"group_commit_wait"`
	// GroupCommitMaxOps caps one group commit (Appendix B: 200).
	GroupCommitMaxOps int `json:"group_commit_max_ops" toml:"group_commit_max_ops"`
	// BusyTimeout is the SQLite busy handler ceiling (App. B: 5s).
	BusyTimeout time.Duration `json:"busy_timeout" toml:"busy_timeout"`
	// BusyRetries bounds external-lock retries (Appendix B: 3).
	BusyRetries int `json:"busy_retries" toml:"busy_retries"`
	// WALAlertBytes warns above this WAL size (Appendix B: 256MiB).
	WALAlertBytes int64 `json:"wal_alert_bytes" toml:"wal_alert_bytes"`
	// ReadPoolSize caps pooled readers; 0 means min(4, NumCPU).
	ReadPoolSize int `json:"read_pool_size" toml:"read_pool_size"`
}

// ProviderConfig governs failover (Section 19.5).
type ProviderConfig struct {
	// BreakerFailures opens the breaker after this many consecutive
	// failures (Appendix B: 5).
	BreakerFailures int `json:"breaker_failures" toml:"breaker_failures"`
	// BreakerErrorRate opens the breaker above this 1-minute error rate
	// (Appendix B: 0.5).
	BreakerErrorRate float64 `json:"breaker_error_rate" toml:"breaker_error_rate"`
	// BreakerOpenFor keeps the breaker open (Appendix B: 60s).
	BreakerOpenFor time.Duration `json:"breaker_open_for" toml:"breaker_open_for"`
}

// ModelLimitsRow is one per-model window override in the layered
// config, keyed by lowercased model id.
type ModelLimitsRow struct {
	ContextWindow int `json:"context_window" toml:"context_window"`
	MaxInput      int `json:"max_input" toml:"max_input"`
	MaxOutput     int `json:"max_output" toml:"max_output"`
}

// ContextConfig governs the context compiler (Section 11).
type ContextConfig struct {
	// CompactionCeiling is the optional cap on the compaction
	// trigger (owner decision 0051). Zero (default) means uncapped:
	// the trigger derives from the model's own max input. Set
	// EXCALIBUR_CONTEXT_CAP or this field to lower it.
	CompactionCeilingTokens int `json:"compaction_ceiling_tokens" toml:"compaction_ceiling_tokens"`
	// DefaultContextWindow is the conservative advertised window
	// for unknown models (full-window fallback).
	DefaultContextWindow int `json:"default_context_window" toml:"default_context_window"`
	// DefaultMaxInput is the conservative binding input cap for
	// unknown models.
	DefaultMaxInput int `json:"default_max_input" toml:"default_max_input"`
	// DefaultMaxOutput is the conservative completion cap for
	// unknown models.
	DefaultMaxOutput int `json:"default_max_output" toml:"default_max_output"`
	// CompactionReserveTokens reserves the next tool result behind
	// the trigger: threshold = maxInput - (reserve + safety).
	CompactionReserveTokens int `json:"compaction_reserve_tokens" toml:"compaction_reserve_tokens"`
	// ModelLimits overrides the shipped per-model table by
	// lowercased model id.
	ModelLimits map[string]ModelLimitsRow `json:"model_limits" toml:"model_limits"`
	// PostCompactionLiteral keeps this literal tail (App. B: 20K).
	PostCompactionLiteralTokens int `json:"post_compaction_literal_tokens" toml:"post_compaction_literal_tokens"`
	// LiteralObservations pins the newest observations (App. B: 10).
	LiteralObservations int `json:"literal_observations" toml:"literal_observations"`
	// MaskTriggerBytes starts masking above this unmasked total (24K).
	MaskTriggerTokens int `json:"mask_trigger_tokens" toml:"mask_trigger_tokens"`
	// MaskTargetBytes compacts masking toward this total (12K).
	MaskTargetTokens int `json:"mask_target_tokens" toml:"mask_target_tokens"`
	// MasterHistoryTokens caps literal master history (App. B: 12K,
	// summarized past 24K).
	MasterHistoryTokens int `json:"master_history_tokens" toml:"master_history_tokens"`
	// TargetTokens aim the compiler per role (Appendix B ranges).
	MasterTargetMin int `json:"master_target_min" toml:"master_target_min"`
	MasterTargetMax int `json:"master_target_max" toml:"master_target_max"`
	ChildTargetMax  int `json:"child_target_max" toml:"child_target_max"`
	ParentTargetMax int `json:"parent_target_max" toml:"parent_target_max"`
}

// MemoryConfig governs retrieval and consolidation (Section 10).
type MemoryConfig struct {
	// ExactDuplicateAt treats similarity at/above as duplicate (0.95).
	ExactDuplicateAt float64 `json:"exact_duplicate_at" toml:"exact_duplicate_at"`
	// AmbiguousBandLo opens the ambiguous band (Appendix B: 0.80).
	AmbiguousBandLo float64 `json:"ambiguous_band_lo" toml:"ambiguous_band_lo"`
	// RRFFusedK is the RRF constant (Appendix B: k=60).
	RRFK int `json:"rrf_k" toml:"rrf_k"`
	// CandidatesPerList caps each retrieval list (Appendix B: 50).
	CandidatesPerList int `json:"candidates_per_list" toml:"candidates_per_list"`
	// ConsolidationIdleDays triggers batch extraction (Appendix B: 5).
	ConsolidationIdleDays int `json:"consolidation_idle_days" toml:"consolidation_idle_days"`
	// EmbeddingDimensions sizes embeddings (Appendix B: 512 int8).
	EmbeddingDimensions int `json:"embedding_dimensions" toml:"embedding_dimensions"`
}

// ToolsConfig governs the tool pipeline (Section 13).
type ToolsConfig struct {
	// OutputBudgetBytes caps head+tail tool output (Appendix B: ~10K
	// tokens; stored as bytes here).
	OutputBudgetBytes int64 `json:"output_budget_bytes" toml:"output_budget_bytes"`
	// ReadWindowLines sizes read_file windows (Appendix B: 200).
	ReadWindowLines int `json:"read_window_lines" toml:"read_window_lines"`
	// DefaultTimeout bounds tool calls (Appendix B default: 60s;
	// exec yield 10s is H3 behavior, not config).
	DefaultTimeout time.Duration `json:"default_timeout" toml:"default_timeout"`
	// MaxSessionsPerRun caps terminal sessions (Appendix B: 16).
	MaxSessionsPerRun int `json:"max_sessions_per_run" toml:"max_sessions_per_run"`
	// MaxSessionsGlobal caps sessions daemon-wide (Appendix B: 64).
	MaxSessionsGlobal int `json:"max_sessions_global" toml:"max_sessions_global"`
	// LiveTailLines caps live output (Appendix B: 20 lines, 2KB).
	LiveTailLines int `json:"live_tail_lines" toml:"live_tail_lines"`
	// LiveTailBytes caps live output bytes (Appendix B: 2KB).
	LiveTailBytes int64 `json:"live_tail_bytes" toml:"live_tail_bytes"`
}

// NotificationsConfig governs notice delivery (Section 5.4).
type NotificationsConfig struct {
	// UserActiveWindow links urgent notices across conversations
	// (Appendix B: 10m).
	UserActiveWindow time.Duration `json:"user_active_window" toml:"user_active_window"`
}

// VaultConfig governs the secrets vault (Section 20.1).
type VaultConfig struct {
	// Argon2MemoryKB sizes passphrase KDF memory (Appendix B: 64MiB).
	Argon2MemoryKB uint32 `json:"argon2_memory_kb" toml:"argon2_memory_kb"`
	// Argon2Iterations sets KDF time cost (Appendix B: 3).
	Argon2Iterations uint32 `json:"argon2_iterations" toml:"argon2_iterations"`
	// Argon2Parallelism sets KDF parallelism (Appendix B: 4).
	Argon2Parallelism uint8 `json:"argon2_parallelism" toml:"argon2_parallelism"`
	// Argon2SaltLen sets the KDF salt length (Appendix B: 16).
	Argon2SaltLen uint32 `json:"argon2_salt_len" toml:"argon2_salt_len"`
	// PassphraseLockAfter auto-locks passphrase vaults (App. B: 12h).
	PassphraseLockAfter time.Duration `json:"passphrase_lock_after" toml:"passphrase_lock_after"`
}

// ModelsConfig governs production model wiring (Sections 19.1, 19.2):
// which provider and model serve every role, the reasoning effort
// fixed per role, the embedding and decisions models, and where the
// credentials live. Keys resolve in Section 20.1 order (vault, then
// environment, then the explicit value here); the eval profile never
// opens the vault and takes keys from the environment only
// (Section 23.2). All fields are optional: empty means "resolve from
// the next layer" (environment aliases, credentials file, defaults).
type ModelsConfig struct {
	// Provider is the registry kind (openai, anthropic, gemini,
	// deepseek, openrouter, ollama, generic) or "stub" for the
	// explicit deterministic stub. Empty means unconfigured.
	Provider string `json:"provider" toml:"provider"`
	// Model is the chat model id serving every role (single-model
	// mode in eval; one configured model elsewhere).
	Model string `json:"model" toml:"model"`
	// BaseURL overrides the provider default endpoint (tests,
	// proxies, compatible servers).
	BaseURL string `json:"base_url" toml:"base_url"`
	// ReasoningEffort is fixed per role (Section 19.2); empty means
	// the operator default low.
	ReasoningEffort string `json:"reasoning_effort" toml:"reasoning_effort"`
	// EmbeddingModel names the embedding model; its operations never
	// carry reasoning effort. Empty means text-embedding-3-small.
	EmbeddingModel string `json:"embedding_model" toml:"embedding_model"`
	// DecisionsModel names the decisions chat model; empty means
	// gpt-6-luna.
	DecisionsModel string `json:"decisions_model" toml:"decisions_model"`
	// CredentialsFile points at an OpenAI-style operator JSON file
	// (base_url, api_key, fallback_api_key, model,
	// reasoning_effort, embedding_model, decisions_model). Empty
	// means EXCALIBUR_CREDENTIALS_FILE. Production entry points
	// also try the default operator path when neither is set.
	CredentialsFile string `json:"credentials_file" toml:"credentials_file"`
	// APIKey is the explicit credential: last resort after the vault
	// hook and the environment.
	APIKey string `json:"api_key" toml:"api_key"`
	// APIKeyEnv names the environment variable holding the key.
	// Empty means the kind defaults plus EXCALIBUR_API_KEY.
	APIKeyEnv string `json:"api_key_env" toml:"api_key_env"`
	// APIKeySecret names the vault secret (or secret:// ref) holding
	// the key, resolved via the vault hook.
	APIKeySecret string `json:"api_key_secret" toml:"api_key_secret"`
	// FallbackKey is the explicit fallback credential, used only on
	// quota/rate-limit errors with bounded retries (Section 19.5).
	FallbackKey string `json:"fallback_api_key" toml:"fallback_api_key"`
	// FallbackKeyEnv names the environment variable holding the
	// fallback credential. Empty means EXCALIBUR_FALLBACK_API_KEY.
	FallbackKeyEnv string `json:"fallback_api_key_env" toml:"fallback_api_key_env"`
	// FallbackKeySecret names the vault secret holding the fallback
	// credential.
	FallbackKeySecret string `json:"fallback_api_key_secret" toml:"fallback_api_key_secret"`
}

// ResourcesConfig governs admission and watchdogs (Sections 3.2, 5.2).
type ResourcesConfig struct {
	// ReservedCPUs keeps cores for the interactive path (App. B: 2).
	ReservedCPUs int `json:"reserved_cpus" toml:"reserved_cpus"`
	// ResumeBelowFrac re-admits below this RSS fraction (App. B: 0.8).
	ResumeBelowFrac float64 `json:"resume_below_frac" toml:"resume_below_frac"`
	// ResumeStableFor re-admits after this stability (Appendix B: 60s).
	ResumeStableFor time.Duration `json:"resume_stable_for" toml:"resume_stable_for"`
	// SafeModeTrips enters safe mode (Appendix B: 3 crashes/10m).
	SafeModeTrips int `json:"safe_mode_trips" toml:"safe_mode_trips"`
	// SafeModeWindow sizes the crash window (Appendix B: 10m).
	SafeModeWindow time.Duration `json:"safe_mode_window" toml:"safe_mode_window"`
	// SafeModeCooldown exits safe mode (Appendix B: 30m calm).
	SafeModeCooldown time.Duration `json:"safe_mode_cooldown" toml:"safe_mode_cooldown"`
}

// Default returns the Appendix B initial values.
// RobotConfig gates physical robot access in the daemon assistant.
type RobotConfig struct {
	Enabled bool `json:"enabled" toml:"enabled"`
}

func Default() Config {
	return Config{
		Robot:   RobotConfig{Enabled: true},
		Voice:   VoiceConfig{Provider: "openai", STTModel: "gpt-4o-mini-transcribe", TTSModel: "gpt-4o-mini-tts", Voice: "alloy"},
		Browser: defaultBrowser(),
		Search:  defaultSearch(),
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
			CompactionCeilingTokens:     0,
			DefaultContextWindow:        200000,
			DefaultMaxInput:             128000,
			DefaultMaxOutput:            16384,
			CompactionReserveTokens:     16000,
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

// VoiceConfig configures client audio operations; nil Enabled follows key availability.
type VoiceConfig struct {
	Provider string `json:"provider" toml:"provider"`
	STTModel string `json:"stt_model" toml:"stt_model"`
	TTSModel string `json:"tts_model" toml:"tts_model"`
	Voice    string `json:"voice" toml:"voice"`
	Enabled  *bool  `json:"enabled,omitempty" toml:"enabled"`
}
