package agenda

import (
	"fmt"
	"time"
)

// Internal scheduled jobs (21.9). They run in the low-activity window
// at P3 priority and yield to user activity (21.10: "corren en P3 en
// su ventana y ceden ante la actividad").

// Internal job IDs (21.9).
const (
	JobDailyMemoryReview = "daily-memory-review"
	JobActivityHistogram = "activity-histogram"
	JobMemoryExpiry      = "memory-expiry"
	JobEmbeddingReindex  = "embedding-reindex"
	JobCodegraphRefresh  = "codegraph-refresh"
	JobWALMaintenance    = "wal-maintenance"
	JobDBBackup          = "db-backup"
	JobBlobGC            = "blob-gc"
	JobUpdateCheck       = "update-check"
	JobHealthWatchdogs   = "health-watchdogs"
)

// InternalJob describes one scheduled internal job (21.9).
type InternalJob struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	// CronExpr is the default cadence in the user zone.
	CronExpr string `json:"cron_expr"`
	// WindowOnly restricts the job to the low-activity window.
	WindowOnly bool `json:"window_only"`
	// Lane is the execution priority lane (always P3 for internal
	// jobs: background work that yields to the user).
	Lane string `json:"lane"`
}

// DefaultInternalJobs returns the ten jobs of Section 21.9.
func DefaultInternalJobs() []InternalJob {
	return []InternalJob{
		{ID: JobDailyMemoryReview, Description: "Daily memory review in the low-activity window (10.6)", CronExpr: "30 3 * * *", WindowOnly: true, Lane: "P3"},
		{ID: JobActivityHistogram, Description: "Recompute the activity histogram and low-activity window weekly (10.6)", CronExpr: "0 4 * * 0", WindowOnly: true, Lane: "P3"},
		{ID: JobMemoryExpiry, Description: "Apply expired memories without wiping history (10.3)", CronExpr: "45 3 * * *", WindowOnly: true, Lane: "P3"},
		{ID: JobEmbeddingReindex, Description: "Reindex vectors on embedding-model change with dual read (10.8)", CronExpr: "0 5 * * 0", WindowOnly: true, Lane: "P3"},
		{ID: JobCodegraphRefresh, Description: "Finish incremental code-graph refresh for active projects (10.10)", CronExpr: "15 4 * * *", WindowOnly: true, Lane: "P3"},
		{ID: JobWALMaintenance, Description: "PASSIVE checkpoints idle, TRUNCATE daily (4.3)", CronExpr: "0 4 * * *", WindowOnly: false, Lane: "P3"},
		{ID: JobDBBackup, Description: "Daily VACUUM INTO with 7-daily/4-weekly rotation (4.6)", CronExpr: "0 2 * * *", WindowOnly: false, Lane: "P3"},
		{ID: JobBlobGC, Description: "Collect unreferenced blobs (4.2)", CronExpr: "30 4 * * *", WindowOnly: true, Lane: "P3"},
		{ID: JobUpdateCheck, Description: "Check the update channel and stage the update (25.3)", CronExpr: "0 6 * * *", WindowOnly: false, Lane: "P3"},
		{ID: JobHealthWatchdogs, Description: "Watch heartbeats, RAM, disk, WAL, provider errors, cache drift (22.5)", CronExpr: "*/10 * * * *", WindowOnly: false, Lane: "P3"},
	}
}

// Validate checks the job table contract.
func ValidateInternalJobs(jobs []InternalJob) error {
	if len(jobs) != 10 {
		return fmt.Errorf("agenda: want 10 internal jobs (21.9), have %d", len(jobs))
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		if j.ID == "" {
			return fmt.Errorf("agenda: internal job needs an id")
		}
		if seen[j.ID] {
			return fmt.Errorf("agenda: duplicate internal job %q", j.ID)
		}
		seen[j.ID] = true
		if _, err := ParseCron(j.CronExpr); err != nil {
			return fmt.Errorf("agenda: internal job %q: %w", j.ID, err)
		}
		if j.Lane != "P3" {
			return fmt.Errorf("agenda: internal job %q must run in P3, have %q", j.ID, j.Lane)
		}
	}
	for _, want := range []string{JobDailyMemoryReview, JobActivityHistogram, JobMemoryExpiry, JobEmbeddingReindex, JobCodegraphRefresh, JobWALMaintenance, JobDBBackup, JobBlobGC, JobUpdateCheck, JobHealthWatchdogs} {
		if !seen[want] {
			return fmt.Errorf("agenda: missing internal job %q", want)
		}
	}
	return nil
}

// ShouldRun decides whether an internal job fires now: window-only
// jobs wait for the low-activity window, and every job yields while
// the user is active (21.9/21.10).
func (j InternalJob) ShouldRun(now time.Time, inWindow, userActive bool) bool {
	if userActive {
		return false
	}
	if j.WindowOnly && !inWindow {
		return false
	}
	return true
}
