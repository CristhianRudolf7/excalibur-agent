// Project registry persistence (Section 9.2). The registry keeps its
// working set in memory and mirrors every register, update and archive
// into the projects table of the main database; a daemon restart loads
// the mirror back, so projects survive restarts.
package daemon

import (
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// OpenProjectRegistry returns the project registry for the daemon. With
// a database it attaches the SQL mirror and restores the stored
// projects; without one (embedded or test setups) it stays in memory.
func OpenProjectRegistry(db *store.DB) (*workspace.Registry, error) {
	reg := workspace.NewRegistry()
	if db == nil {
		return reg, nil
	}
	reg.AttachProjectStore(workspace.OpenProjectStore(db))
	if err := reg.RestoreProjects(); err != nil {
		return nil, fmt.Errorf("daemon: restore projects: %w", err)
	}
	return reg, nil
}
