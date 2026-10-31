// Retention jobs (Section 4.7).
//
// The horizons live in internal/store (ApplyRetention, Backup,
// BlobStore): this file owns only the daemon side, the daily
// scheduler that runs each phase as an idempotent step and the
// tombstone evidence that purges leave behind. Every phase is safe to
// re-run and safe to interrupt mid-way.
package daemon

import (
	"context"
	"fmt"
	"time"
)

// RetentionPhases is the injectable work of one retention pass. Each
// hook must be idempotent; hooks run in order and a failing hook
// aborts the pass with its error so the next scheduled pass retries.
type RetentionPhases struct {
	// CompactJournals replaces terminal-run journals older than the
	// horizon with one summary blob each (30 days, 4.7).
	CompactJournals func(ctx context.Context) error
	// ReleaseToolBlobs drops tool-output blob references past the
	// horizon unless evidence, documents, file versions or compacted
	// markers still point at them (30 days, 4.7).
	ReleaseToolBlobs func(ctx context.Context) error
	// TrimTelemetry deletes raw telemetry events past the horizon
	// (90 days); daily aggregates are indefinite (4.7).
	TrimTelemetry func(ctx context.Context) error
	// CheckpointWAL runs the idle PASSIVE checkpoint and the daily
	// TRUNCATE in the maintenance window (4.3).
	CheckpointWAL func(ctx context.Context) error
	// BackupAll runs VACUUM INTO of every database and rotates to 7
	// daily plus 4 weekly copies (4.6).
	BackupAll func(ctx context.Context) error
}

// RetentionReport counts what one pass did; hooks that cannot count
// report -1 for their phase.
type RetentionReport struct {
	At             time.Time
	Journals       int
	ToolBlobs      int
	Telemetry      int
	Checkpointed   bool
	BackedUp       bool
	Backups        []string
	DurationMillis int64
}

// RunRetention executes one full retention pass in phase order and
// returns its report. A nil hook is skipped, so partial wiring (no
// telemetry database, no backup dir) still runs the rest.
func RunRetention(ctx context.Context, ph RetentionPhases) (RetentionReport, error) {
	start := time.Now().UTC()
	rep := RetentionReport{At: start}
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"compact journals", ph.CompactJournals},
		{"release tool blobs", ph.ReleaseToolBlobs},
		{"trim telemetry", ph.TrimTelemetry},
		{"checkpoint WAL", ph.CheckpointWAL},
		{"backup all", ph.BackupAll},
	}
	for _, s := range steps {
		if s.fn == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return rep, ctx.Err()
		default:
		}
		if err := s.fn(ctx); err != nil {
			return rep, fmt.Errorf("daemon: retention %s: %w", s.name, err)
		}
		switch s.name {
		case "checkpoint WAL":
			rep.Checkpointed = true
		case "backup all":
			rep.BackedUp = true
		}
	}
	rep.DurationMillis = time.Since(start).Milliseconds()
	return rep, nil
}

// RetentionScheduler decides when the daily pass runs: once per
// calendar day in the maintenance window, plus an explicit RunNow
// for operator or test triggers.
type RetentionScheduler struct {
	// WindowHour is the local hour of the maintenance window
	// (Appendix B and 10.6: low-activity hours).
	WindowHour int
	lastRun    time.Time
}

// NewRetentionScheduler returns a scheduler firing in the given local
// hour each day.
func NewRetentionScheduler(windowHour int) *RetentionScheduler {
	return &RetentionScheduler{WindowHour: windowHour}
}

// Due reports whether the daily pass should run now: never ran today
// and the local clock is inside the window hour.
func (s *RetentionScheduler) Due(now time.Time) bool {
	if now.Hour() != s.WindowHour {
		return false
	}
	y1, m1, d1 := s.lastRun.Date()
	y2, m2, d2 := now.Date()
	return !(y1 == y2 && m1 == m2 && d1 == d2)
}

// MarkRan records a completed pass.
func (s *RetentionScheduler) MarkRan(now time.Time) { s.lastRun = now }

// MaybeRun runs one pass when due and records it; otherwise it
// returns a zero report and false.
func (s *RetentionScheduler) MaybeRun(ctx context.Context, now time.Time, ph RetentionPhases) (RetentionReport, bool, error) {
	if !s.Due(now) {
		return RetentionReport{}, false, nil
	}
	rep, err := RunRetention(ctx, ph)
	if err != nil {
		return rep, false, err
	}
	s.MarkRan(now)
	return rep, true, nil
}
