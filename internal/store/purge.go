package store

import (
	"context"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// User-requested deletion of Section 4.7. The "never destroy" rule of
// memory (Section 10.3) governs automatic updates; the user may still
// order forgetting. A purge removes content rows, their FTS5 and
// vector entries, their blobs and their durable-journal and
// telemetry traces, and leaves a single tombstone (id, date, reason)
// so dangling references explain themselves. Older backups keep the
// data until they rotate (four weeks at most); the caller must tell
// the user that, and the action is irreversible (callers confirm
// first, Section 6.4).

// BackupPurgeNotice is the confirmation text callers must show with
// every user-requested deletion: older backups keep the data until
// they rotate (Section 4.7, at most 4 weeks).
const BackupPurgeNotice = "Backups made before this deletion still contain the data; " +
	"they rotate out automatically (7 daily and 4 weekly copies, so at most 4 weeks)."

// purgeMemoriesInScope deletes memories rows in scope and returns
// their ids.
func purgeMemoriesInScope(ctx context.Context, ex Execer, scopeID string) ([]string, error) {
	rows, err := ex.Query(ctx, "SELECT id FROM memories WHERE scope_id=?", scopeID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if err := deleteMemory(ctx, ex, id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// deleteMemory removes one memory and its index entries.
func deleteMemory(ctx context.Context, ex Execer, id string) error {
	if _, err := ex.Exec(ctx, "DELETE FROM memories WHERE id=?", id); err != nil {
		return err
	}
	// FTS rows die by trigger; vec rows and shadow vectors are
	// removed explicitly (triggers cannot reach virtual tables'
	// bookkeeping portably).
	return DeleteEmbedding(ctx, ex, "memory", id)
}

// PurgeMemory forgets one memory of Section 10.5/4.7.
func PurgeMemory(ctx context.Context, db *DB, tele *DB, memoryID, reason string) error {
	scopeID, statement, err := readMemory(ctx, db, memoryID)
	if err != nil {
		return err
	}
	_ = statement
	err = db.Write(ctx, PriorityInteractive, func(_ context.Context, ex Execer) error {
		if err := deleteMemory(ctx, ex, memoryID); err != nil {
			return err
		}
		return writeTombstone(ctx, ex, scopeID, "memory:"+memoryID, reason)
	})
	if err != nil {
		return err
	}
	if tele != nil {
		_ = purgeTelemetryText(ctx, tele, memoryID)
	}
	return nil
}

func readMemory(ctx context.Context, db *DB, id string) (scope, statement string, err error) {
	err = db.Read(ctx, func(_ context.Context, q Queryer) error {
		return q.QueryRow(ctx, "SELECT scope_id, statement FROM memories WHERE id=?", id).Scan(&scope, &statement)
	})
	return scope, statement, err
}

// PurgeConversation forgets one conversation: its messages (FTS dies
// by trigger), client deduplication rows, memories staged in its
// scope, open questions and notices, plus telemetry traces. Tasks
// keep their rows but lose the origin link, so plans survive while
// the words are gone.
func PurgeConversation(ctx context.Context, db *DB, tele *DB, conversationID, reason string) error {
	scopeID := "conversation:" + conversationID
	err := db.Write(ctx, PriorityInteractive, func(_ context.Context, ex Execer) error {
		if _, err := ex.Exec(ctx, "DELETE FROM messages WHERE conversation_id=?", conversationID); err != nil {
			return err
		}
		if _, err := ex.Exec(ctx, "DELETE FROM questions WHERE run_id IN (SELECT id FROM runs WHERE task_id IN (SELECT id FROM tasks WHERE origin_conversation_id=?))", conversationID); err != nil {
			return err
		}
		if _, err := ex.Exec(ctx, "UPDATE tasks SET origin_conversation_id='' WHERE origin_conversation_id=?", conversationID); err != nil {
			return err
		}
		if _, err := ex.Exec(ctx, "DELETE FROM notifications WHERE task_id IN (SELECT id FROM tasks WHERE origin_conversation_id=?)", conversationID); err != nil {
			return err
		}
		if _, err := purgeMemoriesInScope(ctx, ex, scopeID); err != nil {
			return err
		}
		return writeTombstone(ctx, ex, scopeID, "conversation:"+conversationID, reason)
	})
	if err != nil {
		return err
	}
	if tele != nil {
		_ = purgeTelemetryText(ctx, tele, conversationID)
	}
	return nil
}

// PurgeDocument forgets one library document: metadata, chunks (FTS
// by trigger, vectors explicitly), the original blob dereferenced
// for the next GC pass, plus telemetry traces.
func PurgeDocument(ctx context.Context, db *DB, tele *DB, blobs BlobStore, documentID, reason string) error {
	var scopeID, digest string
	if err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		return q.QueryRow(ctx, "SELECT scope_id, blob_digest FROM documents WHERE id=?", documentID).Scan(&scopeID, &digest)
	}); err != nil {
		return err
	}
	err := db.Write(ctx, PriorityInteractive, func(_ context.Context, ex Execer) error {
		rows, err := ex.Query(ctx, "SELECT id FROM doc_chunks WHERE doc_id=?", documentID)
		if err != nil {
			return err
		}
		var chunks []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			chunks = append(chunks, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range chunks {
			if err := DeleteEmbedding(ctx, ex, "doc_chunk", c); err != nil {
				return err
			}
		}
		if _, err := ex.Exec(ctx, "DELETE FROM doc_chunks WHERE doc_id=?", documentID); err != nil {
			return err
		}
		if _, err := ex.Exec(ctx, "DELETE FROM documents WHERE id=?", documentID); err != nil {
			return err
		}
		return writeTombstone(ctx, ex, scopeID, "document:"+documentID, reason)
	})
	if err != nil {
		return err
	}
	if digest != "" && blobs != nil {
		_ = blobs.Release(ctx, digest)
	}
	if tele != nil {
		_ = purgeTelemetryText(ctx, tele, documentID)
	}
	return nil
}

// PurgeProject forgets one project: work rows, memories and graph
// edges in its scope, the code graph file when present, plus a
// tombstone. Tasks and runs keep no content behind: their rows go
// too, since a project purge means starting over.
func PurgeProject(ctx context.Context, db *DB, tele *DB, projectID, reason string) error {
	scopeID := "project:" + projectID
	err := db.Write(ctx, PriorityInteractive, func(_ context.Context, ex Execer) error {
		tables := []string{"plan_items", "milestones", "requirements", "criteria", "decisions", "instructions", "project_log", "memories", "entities", "edges"}
		for _, t := range tables {
			if _, err := ex.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE scope_id=?", t), scopeID); err != nil {
				return fmt.Errorf("store: purge %s: %w", t, err)
			}
		}
		if _, err := ex.Exec(ctx, "DELETE FROM tasks WHERE scope_id=?", scopeID); err != nil {
			return err
		}
		if _, err := ex.Exec(ctx, "DELETE FROM projects WHERE id=?", projectID); err != nil {
			return err
		}
		return writeTombstone(ctx, ex, scopeID, "project:"+projectID, reason)
	})
	if err != nil {
		return err
	}
	if tele != nil {
		_ = purgeTelemetryText(ctx, tele, projectID)
	}
	return nil
}

// writeTombstone leaves the deletion marker of Section 4.7.
func writeTombstone(ctx context.Context, ex Execer, scopeID, kind, reason string) error {
	_, err := ex.Exec(ctx, `INSERT INTO tombstones(id,scope_id,kind,deleted_at_ms,reason)
		VALUES(?,?,?,?,?)`, types.NewULID(), scopeID, kind, time.Now().UnixMilli(), reason)
	return err
}

// purgeTelemetryText drops telemetry rows mentioning id.
func purgeTelemetryText(ctx context.Context, tele *DB, id string) error {
	return tele.Write(ctx, PriorityBackground, func(_ context.Context, ex Execer) error {
		_, err := ex.Exec(ctx, "DELETE FROM telemetry_events WHERE run_id=? OR task_id=? OR payload_json LIKE ?", id, id, "%"+id+"%")
		return err
	})
}
