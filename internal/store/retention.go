package store

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

// Retention implements Section 4.7: bounded history, unbounded user
// content. Messages, tasks, requirements, evidence, decisions and
// memories live indefinitely unless the user purges them; the
// run journal, tool-output blobs and raw telemetry rotate.

// RetentionPolicy tunes the horizons; Zero Policy is the Section 4.7
// default table.
type RetentionPolicy struct {
	// JournalDays keeps full run journals this long (default 30);
	// older terminal runs compact into one summary blob.
	JournalDays int
	// ToolBlobDays releases tool-output blob references this long
	// after their run ends (default 30), unless evidence, documents
	// or memory still reference them.
	ToolBlobDays int
	// TelemetryDays trims raw telemetry events (default 90); daily
	// aggregates live indefinitely.
	TelemetryDays int
}

// DefaultRetention is the Section 4.7 table.
func DefaultRetention() RetentionPolicy {
	return RetentionPolicy{JournalDays: 30, ToolBlobDays: 30, TelemetryDays: 90}
}

// ApplyRetention enforces the policy on main and tele (tele may be
// nil). It runs each phase as short porter transactions, safe to
// re-run and to interrupt: every phase is idempotent.
func ApplyRetention(ctx context.Context, main, tele *DB, blobs BlobStore, p RetentionPolicy) error {
	if p.JournalDays <= 0 {
		p.JournalDays = 30
	}
	if p.ToolBlobDays <= 0 {
		p.ToolBlobDays = 30
	}
	if p.TelemetryDays <= 0 {
		p.TelemetryDays = 90
	}
	if err := compactJournals(ctx, main, blobs, time.Now().AddDate(0, 0, -p.JournalDays)); err != nil {
		return err
	}
	if err := releaseOldToolBlobs(ctx, main, blobs, time.Now().AddDate(0, 0, -p.ToolBlobDays)); err != nil {
		return err
	}
	if tele != nil {
		if err := trimTelemetry(ctx, tele, time.Now().AddDate(0, 0, -p.TelemetryDays)); err != nil {
			return err
		}
	}
	return nil
}

var terminalRunStates = map[string]bool{
	"completed":             true,
	"completed_with_issues": true,
	"failed":                true,
	"canceled":              true,
}

// compactJournals replaces the journal of each terminal run older
// than cutoff with one marker row pointing at a summary blob: steps,
// tools, results, enough for traceability (Section 4.7). Runs marked
// for evaluation or replay keep their full journal: model responses
// survive only there (Section 23.6).
func compactJournals(ctx context.Context, db *DB, blobs BlobStore, cutoff time.Time) error {
	type run struct {
		id     string
		status string
	}
	var runs []run
	err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		rows, err := q.Query(ctx, `SELECT id, status FROM runs WHERE updated_at<?`, cutoff.UnixMilli())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r run
			if err := rows.Scan(&r.id, &r.status); err != nil {
				return err
			}
			if terminalRunStates[r.status] {
				runs = append(runs, r)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, r := range runs {
		if err := compactOneJournal(ctx, db, blobs, r.id); err != nil {
			return err
		}
	}
	return nil
}

func compactOneJournal(ctx context.Context, db *DB, blobs BlobStore, runID string) error {
	// Skip runs already compacted or kept for evaluation.
	var marker int
	if err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		return q.QueryRow(ctx, "SELECT COUNT(*) FROM journal WHERE run_id=? AND seq=-1", runID).Scan(&marker)
	}); err != nil {
		return err
	}
	if marker > 0 {
		return nil
	}
	keep, err := runKeptForEval(ctx, db, runID)
	if err != nil {
		return err
	}
	if keep {
		return nil
	}
	type entry struct {
		Seq     int    `json:"seq"`
		Type    string `json:"type"`
		Payload string `json:"payload"`
		Status  string `json:"status"`
	}
	var entries []entry
	if err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		rows, err := q.Query(ctx, "SELECT seq,type,payload,status FROM journal WHERE run_id=? AND seq>=0 ORDER BY seq", runID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.Seq, &e.Type, &e.Payload, &e.Status); err != nil {
				return err
			}
			entries = append(entries, e)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	summary, err := json.Marshal(map[string]any{
		"run_id":      runID,
		"compactedAt": time.Now().UTC().Format(time.RFC3339),
		"steps":       len(entries),
		"entries":     entries,
	})
	if err != nil {
		return err
	}
	digest, err := putBlobBytes(ctx, blobs, summary, "journal", runID)
	if err != nil {
		return err
	}
	return db.Write(ctx, PriorityBackground, func(_ context.Context, ex Execer) error {
		if _, err := ex.Exec(ctx, "DELETE FROM journal WHERE run_id=? AND seq>=0", runID); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `INSERT INTO journal(run_id,seq,type,payload,blob_digest,status,idempotency_key)
			VALUES(?,-1,'compacted','',?,'done','retention') ON CONFLICT(run_id,seq) DO NOTHING`, runID, digest)
		return err
	})
}

// runKeptForEval reports whether the run is marked for evaluation or
// replay (spent_json.keep_full_journal), which preserves model
// responses per Section 23.6.
func runKeptForEval(ctx context.Context, db *DB, runID string) (bool, error) {
	var spent string
	if err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		return q.QueryRow(ctx, "SELECT spent_json FROM runs WHERE id=?", runID).Scan(&spent)
	}); err != nil {
		return false, err
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(spent), &decoded); err != nil {
		return false, nil
	}
	keep, _ := decoded["keep_full_journal"].(bool)
	return keep, nil
}

// putBlobBytes stores raw bytes through the BlobStore with kind and
// run attribution for retention.
func putBlobBytes(ctx context.Context, blobs BlobStore, data []byte, kind, runID string) (string, error) {
	digest, err := blobs.Put(ctx, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if bs, ok := blobs.(*blobStore); ok {
		_ = bs.setKindRun(ctx, digest, kind, runID)
	}
	return digest, nil
}

// releaseOldToolBlobs releases tool-output references older than the
// cutoff whose run ended, unless evidence, documents, file versions
// or compacted journal markers still point at them (Section 4.7).
func releaseOldToolBlobs(ctx context.Context, db *DB, blobs BlobStore, cutoff time.Time) error {
	var digests []string
	err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		rows, err := q.Query(ctx, `SELECT digest FROM blob_refs
			WHERE kind='tool_output' AND refcount>0 AND created_at<?`, cutoff.UnixMilli())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				return err
			}
			digests = append(digests, d)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, d := range digests {
		referenced, err := blobReferenced(ctx, db, d)
		if err != nil {
			return err
		}
		if referenced {
			continue
		}
		if err := blobs.Release(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

// blobReferenced checks the keepers of Section 4.7: evidence,
// documents, file versions and compacted journal markers.
func blobReferenced(ctx context.Context, db *DB, digest string) (bool, error) {
	var n int
	err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		queries := []string{
			"SELECT COUNT(*) FROM evidence WHERE blob_digest=?",
			"SELECT COUNT(*) FROM documents WHERE blob_digest=?",
			"SELECT COUNT(*) FROM file_versions WHERE preimage_digest=? OR postimage_digest=?",
			"SELECT COUNT(*) FROM journal WHERE blob_digest=?",
		}
		for i, qs := range queries {
			var c int
			var err error
			if i == 2 {
				err = q.QueryRow(ctx, qs, digest, digest).Scan(&c)
			} else {
				err = q.QueryRow(ctx, qs, digest).Scan(&c)
			}
			if err != nil {
				return err
			}
			n += c
		}
		return nil
	})
	return n > 0, err
}

// trimTelemetry deletes raw events older than the cutoff; daily
// aggregates are indefinite (Section 4.7).
func trimTelemetry(ctx context.Context, tele *DB, cutoff time.Time) error {
	return tele.Write(ctx, PriorityBackground, func(_ context.Context, ex Execer) error {
		_, err := ex.Exec(ctx, "DELETE FROM telemetry_events WHERE created_ms<?", cutoff.UnixMilli())
		return err
	})
}

// TombstoneCount counts deletion markers (Section 4.7 evidence).
func TombstoneCount(ctx context.Context, db *DB) (int, error) {
	var n int
	err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		return q.QueryRow(ctx, "SELECT COUNT(*) FROM tombstones").Scan(&n)
	})
	return n, err
}

// setKindRun tags a blob row; used by retention-aware writers.
func (b *blobStore) setKindRun(ctx context.Context, digest, kind, runID string) error {
	return b.db.Write(ctx, PriorityNormal, func(_ context.Context, ex Execer) error {
		_, err := ex.Exec(ctx, "UPDATE blob_refs SET kind=?, run_id=? WHERE digest=?", kind, runID, digest)
		return err
	})
}
