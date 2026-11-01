package config

import (
	"testing"
	"time"
)

func TestDefaultMatchesAppendixB(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	if c.Master.MaxSteps != 8 {
		t.Fatal("master.max_steps")
	}
	if c.Engine.ChildMaxSteps != 60 || c.Engine.ParentMaxSteps != 150 {
		t.Fatal("engine steps")
	}
	if c.Engine.MaxActiveChildren != 4 || c.Engine.MaxRunningPerUser != 8 {
		t.Fatal("engine fan-out")
	}
	if c.Porter.GroupCommitWait != 5*time.Millisecond || c.Porter.GroupCommitMaxOps != 200 {
		t.Fatal("porter group commit")
	}
	if c.Porter.BusyTimeout != 5*time.Second || c.Porter.BusyRetries != 3 {
		t.Fatal("porter busy")
	}
	if c.Porter.WALAlertBytes != 256<<20 {
		t.Fatal("porter wal alert")
	}
	if c.Context.CompactionCeilingTokens != 0 {
		t.Fatal("compaction ceiling defaults to uncapped (owner decision 0051)")
	}
	if c.Context.DefaultMaxInput != 128000 || c.Context.DefaultContextWindow != 200000 || c.Context.DefaultMaxOutput != 16384 {
		t.Fatal("conservative model defaults")
	}
	if c.Context.CompactionReserveTokens != 16000 {
		t.Fatal("compaction reserve")
	}
	if c.Context.MaskTriggerTokens != 24000 || c.Context.MaskTargetTokens != 12000 {
		t.Fatal("masking")
	}
	if c.Memory.ExactDuplicateAt != 0.95 || c.Memory.AmbiguousBandLo != 0.80 || c.Memory.RRFK != 60 {
		t.Fatal("memory")
	}
	if c.Memory.CandidatesPerList != 50 || c.Memory.EmbeddingDimensions != 512 {
		t.Fatal("memory lists")
	}
	if c.Provider.BreakerFailures != 5 || c.Provider.BreakerOpenFor != 60*time.Second {
		t.Fatal("provider breaker")
	}
	if c.Vault.Argon2MemoryKB != 64*1024 || c.Vault.Argon2Iterations != 3 ||
		c.Vault.Argon2Parallelism != 4 || c.Vault.Argon2SaltLen != 16 {
		t.Fatal("vault argon2")
	}
	if c.Resources.ReservedCPUs != 2 || c.Resources.ResumeBelowFrac != 0.8 {
		t.Fatal("resources")
	}
	if c.Engine.AskBelowProb != 0.7 {
		t.Fatal("ask threshold")
	}
	if c.Tools.ReadWindowLines != 200 || c.Tools.MaxSessionsPerRun != 16 || c.Tools.MaxSessionsGlobal != 64 {
		t.Fatal("tools")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	bad := Default()
	bad.Engine.ChildMaxSteps = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
	bad = Default()
	bad.Context.MaskTriggerTokens = bad.Context.MaskTargetTokens
	if err := bad.Validate(); err == nil {
		t.Fatal("expected mask ordering error")
	}
}
