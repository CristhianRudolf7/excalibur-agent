package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

// embeddedReminders shares the assistant database with the daemon. Embedded
// exec never starts a second delivery timer; the daemon delivers persisted work.
func embeddedReminders(ctx context.Context, profile ProfileName) (*agenda.ReminderService, func(), error) {
	closeDB := func() {}
	if profile == ProfileEval || os.Getenv("EXCALIBUR_PERSISTENT_REMINDERS") == "0" || strings.EqualFold(os.Getenv("EXCALIBUR_PERSISTENT_REMINDERS"), "false") {
		return nil, closeDB, nil
	}
	dir, err := browserDataDir()
	if err != nil {
		return nil, closeDB, err
	}
	if home := os.Getenv("EXCALIBUR_HOME"); home != "" {
		dir = filepath.Join(home, "state")
	}
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(dir, "blobs")
	db, err := store.Open(ctx, filepath.Join(dir, "excalibur.db"), opts, migrations.Main())
	if err != nil {
		return nil, closeDB, err
	}
	return &agenda.ReminderService{Store: agenda.NewSQLReminderStore(db), DB: db}, func() { _ = db.Close() }, nil
}
