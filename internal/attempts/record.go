package attempts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AttemptRecord is one row of the per-attempt trial record.
type AttemptRecord struct {
	Index        int      `json:"index"`
	WorkDir      string   `json:"work_dir,omitempty"`
	LogDir       string   `json:"log_dir,omitempty"`
	State        string   `json:"state"`
	Summary      string   `json:"summary,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Question     string   `json:"question,omitempty"`
	ToolCalls    int64    `json:"tool_calls"`
	ToolErrors   int64    `json:"tool_errors"`
	GateAttempts int      `json:"gate_attempts"`
	Verified     int64    `json:"verified_checks"`
	DurationSecs float64  `json:"duration_secs"`
	ChangedFiles int      `json:"changed_files"`
	FilesChanged []string `json:"files_changed,omitempty"`
	DiffStat     string   `json:"diff_stat,omitempty"`
	Deliverable  []string `json:"deliverable_missing,omitempty"`
	Passes       bool     `json:"passes"`
	Winner       bool     `json:"winner"`
}

// Record is the top-level attempts.json document.
type Record struct {
	Version   string          `json:"version"`
	N         int             `json:"n"`
	Winner    int             `json:"winner"`
	CreatedAt time.Time       `json:"at"`
	Attempts  []AttemptRecord `json:"attempts"`
	// Isolated is true when the attempts ran in copies of a real
	// working directory (the default for a project run).
	Isolated bool `json:"isolated"`
	// AppliedFiles lists the paths promoted from the winner into the
	// real directory, and AppliedFileCount their number.
	AppliedFiles     []string `json:"applied_files,omitempty"`
	AppliedFileCount int      `json:"applied_file_count"`
	// Fallback names why the run fell back to a single attempt in the
	// real directory (empty when it did not).
	Fallback string `json:"fallback,omitempty"`
}

// RecordVersion pins the attempts.json schema.
const RecordVersion = "attempts@1"

// ToRecord folds outcomes plus the winner position into a record.
func ToRecord(outcomes []Outcome, winner int) Record {
	rec := Record{Version: RecordVersion, N: len(outcomes), Winner: winner, CreatedAt: time.Now().UTC()}
	for _, o := range outcomes {
		rec.Attempts = append(rec.Attempts, AttemptRecord{
			Index: o.Index, WorkDir: o.WorkDir, LogDir: o.LogDir,
			State: o.State, Summary: o.Summary, Reason: o.Reason,
			Question:  o.Question,
			ToolCalls: o.ToolCalls, ToolErrors: o.ToolErrors,
			GateAttempts: o.GateAttempts, Verified: o.Verified,
			DurationSecs: o.DurationSecs, ChangedFiles: o.ChangedCount,
			FilesChanged: o.FilesChanged, DiffStat: o.DiffStat,
			Deliverable: o.Deliverable, Passes: o.Passes(),
			Winner: o.Index == winner,
		})
	}
	return rec
}

// WriteRecord writes rec as attempts.json into dir (creating it).
func WriteRecord(dir string, rec Record) error {
	if dir == "" {
		return fmt.Errorf("attempts: record dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("attempts: mkdir record dir: %w", err)
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("attempts: marshal record: %w", err)
	}
	tmp := filepath.Join(dir, "attempts.json.tmp")
	if err := os.WriteFile(tmp, append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("attempts: write record tmp: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "attempts.json")); err != nil {
		return fmt.Errorf("attempts: rename record: %w", err)
	}
	return nil
}

// OutcomeFromRecord converts one record row back into an Outcome for
// selection tests over persisted files.
func OutcomeFromRecord(r AttemptRecord) Outcome {
	return Outcome{
		Index: r.Index, WorkDir: r.WorkDir, LogDir: r.LogDir,
		State: r.State, Summary: r.Summary, Reason: r.Reason,
		Question:  r.Question,
		ToolCalls: r.ToolCalls, ToolErrors: r.ToolErrors,
		GateAttempts: r.GateAttempts, Verified: r.Verified,
		DurationSecs: r.DurationSecs, ChangedCount: r.ChangedFiles,
		FilesChanged: r.FilesChanged, DiffStat: r.DiffStat,
		Deliverable: r.Deliverable,
	}
}
