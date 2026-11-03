package migrations

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func upV11(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE personal_agents(id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL UNIQUE, data_json TEXT NOT NULL)`,
		`ALTER TABLE messages ADD COLUMN normalized_json TEXT NOT NULL DEFAULT '{}'`,
	})
}
