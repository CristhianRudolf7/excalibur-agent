package migrations

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func upV12(ctx context.Context, ex store.Execer) error {
	_, err := ex.Exec(ctx, `ALTER TABLE principals ADD COLUMN timezone_source TEXT NOT NULL DEFAULT 'default' CHECK(timezone_source IN ('default','auto','manual'))`)
	return err
}
