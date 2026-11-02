package migrations

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func upV8(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE whiteboards (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, created_at INTEGER NOT NULL, data_json TEXT NOT NULL)`,
		`CREATE INDEX idx_whiteboards_conversation ON whiteboards(conversation_id)`,
		`CREATE TABLE whiteboard_versions (board_id TEXT NOT NULL, version INTEGER NOT NULL, data_json TEXT NOT NULL, PRIMARY KEY(board_id, version))`,
		`CREATE TABLE whiteboard_ops (board_id TEXT NOT NULL, client_op_id TEXT NOT NULL, version INTEGER NOT NULL, ops_json TEXT NOT NULL, author TEXT NOT NULL, PRIMARY KEY(board_id,client_op_id))`,
	})
}
