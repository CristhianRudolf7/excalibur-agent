package config

import "fmt"

// validateStruct checks the configuration ranges.
func validateStruct(c Config) error {
	if err := validateBrowserSearch(c); err != nil {
		return err
	}
	checks := []struct {
		ok   bool
		what string
	}{
		{c.Master.MaxSteps > 0, "master.max_steps"},
		{c.Master.RegenCancelWindow > 0, "master.regen_cancel_window"},
		{c.Master.NoticeGroupWindow > 0, "master.notice_group_window"},
		{c.Engine.ChildMaxSteps > 0, "engine.child_max_steps"},
		{c.Engine.ParentMaxSteps > 0, "engine.parent_max_steps"},
		{c.Engine.MaxActiveChildren > 0, "engine.max_active_children"},
		{c.Engine.MaxRunningPerUser > 0, "engine.max_running_per_user"},
		{c.Engine.CloseThreshold > 0 && c.Engine.CloseThreshold < 1, "engine.close_threshold"},
		{c.Engine.HeartbeatTimeout > 0, "engine.heartbeat_timeout"},
		{c.Engine.JournalCheckpointEvery > 0, "engine.journal_checkpoint_every"},
		{c.Engine.AskBelowProb > 0 && c.Engine.AskBelowProb < 1, "engine.ask_below_prob"},
		{c.Porter.GroupCommitWait > 0, "porter.group_commit_wait"},
		{c.Porter.GroupCommitMaxOps > 0, "porter.group_commit_max_ops"},
		{c.Porter.BusyTimeout > 0, "porter.busy_timeout"},
		{c.Porter.BusyRetries >= 0, "porter.busy_retries"},
		{c.Porter.WALAlertBytes > 0, "porter.wal_alert_bytes"},
		{c.Porter.ReadPoolSize >= 0, "porter.read_pool_size"},
		{c.Provider.BreakerFailures > 0, "provider.breaker_failures"},
		{c.Provider.BreakerErrorRate > 0 && c.Provider.BreakerErrorRate <= 1, "provider.breaker_error_rate"},
		{c.Provider.BreakerOpenFor > 0, "provider.breaker_open_for"},
		{c.Context.CompactionCeilingTokens >= 0, "context.compaction_ceiling_tokens"},
		{c.Context.DefaultContextWindow > 0, "context.default_context_window"},
		{c.Context.DefaultMaxInput > 0, "context.default_max_input"},
		{c.Context.DefaultMaxOutput > 0, "context.default_max_output"},
		{c.Context.CompactionReserveTokens > 0, "context.compaction_reserve_tokens"},
		{c.Context.PostCompactionLiteralTokens > 0, "context.post_compaction_literal_tokens"},
		{c.Context.LiteralObservations > 0, "context.literal_observations"},
		{c.Context.MaskTriggerTokens > c.Context.MaskTargetTokens, "context.mask_trigger_gt_target"},
		{c.Context.MasterHistoryTokens > 0, "context.master_history_tokens"},
		{c.Context.MasterTargetMin > 0 && c.Context.MasterTargetMin <= c.Context.MasterTargetMax, "context.master_target_range"},
		{c.Context.ChildTargetMax > 0, "context.child_target_max"},
		{c.Context.ParentTargetMax > 0, "context.parent_target_max"},
		{c.Memory.ExactDuplicateAt > c.Memory.AmbiguousBandLo, "memory.duplicate_gt_ambiguous"},
		{c.Memory.RRFK > 0, "memory.rrf_k"},
		{c.Memory.CandidatesPerList > 0, "memory.candidates_per_list"},
		{c.Memory.ConsolidationIdleDays > 0, "memory.consolidation_idle_days"},
		{c.Memory.EmbeddingDimensions > 0, "memory.embedding_dimensions"},
		{c.Tools.OutputBudgetBytes > 0, "tools.output_budget_bytes"},
		{c.Tools.ReadWindowLines > 0, "tools.read_window_lines"},
		{c.Tools.DefaultTimeout > 0, "tools.default_timeout"},
		{c.Tools.MaxSessionsPerRun > 0, "tools.max_sessions_per_run"},
		{c.Tools.MaxSessionsGlobal >= c.Tools.MaxSessionsPerRun, "tools.sessions_global_ge_run"},
		{c.Tools.LiveTailLines > 0, "tools.live_tail_lines"},
		{c.Tools.LiveTailBytes > 0, "tools.live_tail_bytes"},
		{c.Notifications.UserActiveWindow > 0, "notifications.user_active_window"},
		{c.Vault.Argon2MemoryKB > 0, "vault.argon2_memory_kb"},
		{c.Vault.Argon2Iterations > 0, "vault.argon2_iterations"},
		{c.Vault.Argon2Parallelism > 0, "vault.argon2_parallelism"},
		{c.Vault.Argon2SaltLen > 0, "vault.argon2_salt_len"},
		{c.Vault.PassphraseLockAfter > 0, "vault.passphrase_lock_after"},
		{c.Resources.ReservedCPUs >= 0, "resources.reserved_cpus"},
		{c.Resources.ResumeBelowFrac > 0 && c.Resources.ResumeBelowFrac < 1, "resources.resume_below_frac"},
		{c.Resources.ResumeStableFor > 0, "resources.resume_stable_for"},
		{c.Resources.SafeModeTrips > 0, "resources.safe_mode_trips"},
		{c.Resources.SafeModeWindow > 0, "resources.safe_mode_window"},
		{c.Resources.SafeModeCooldown > 0, "resources.safe_mode_cooldown"},
	}
	for _, c := range checks {
		if !c.ok {
			return fmt.Errorf("config: invalid value for %s", c.what)
		}
	}
	for name, lim := range c.Context.ModelLimits {
		if lim.ContextWindow <= 0 || lim.MaxInput <= 0 || lim.MaxOutput <= 0 {
			return fmt.Errorf("config: invalid value for context.model_limits[%q]", name)
		}
	}
	return nil
}
