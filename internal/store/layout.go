package store

import (
	"os"
	"path/filepath"
)

// Layout is the per-user file family of Section 4.2 rooted at root
// (on Linux, the XDG data dir resolved via internal/platform Paths).
type Layout struct {
	// Root is the data directory (<root>/excalibur or a test dir).
	Root string
}

// NewLayout roots the file family at root.
func NewLayout(root string) Layout { return Layout{Root: root} }

// MainPath is excalibur.db: conversations, work, agents, memory.
// synchronous=FULL.
func (l Layout) MainPath() string { return filepath.Join(l.Root, "excalibur.db") }

// VaultPath is vault.db: the secrets vault (Section 20.1, milestone
// H10 owns its schema). synchronous=FULL, permissions 0600.
func (l Layout) VaultPath() string { return filepath.Join(l.Root, "vault.db") }

// TelemetryPath is telemetry.db: events, metrics, traces.
// synchronous=NORMAL.
func (l Layout) TelemetryPath() string { return filepath.Join(l.Root, "telemetry.db") }

// CodeGraphPath is projects/<id>/codegraph.db: the rebuildable code
// and document graph. synchronous=NORMAL.
func (l Layout) CodeGraphPath(projectID string) string {
	return filepath.Join(l.Root, "projects", projectID, "codegraph.db")
}

// BlobsDir is blobs/: the content-addressed store
// (sha256/ab/cd..., zstd-compressed).
func (l Layout) BlobsDir() string { return filepath.Join(l.Root, "blobs") }

// BackupsDir holds VACUUM INTO copies (Section 4.6).
func (l Layout) BackupsDir() string { return filepath.Join(l.Root, "backups") }

// MainBackupPrefix is the filename prefix for excalibur.db copies.
func (l Layout) MainBackupPrefix() string { return "excalibur-" }

// EnsureLayout creates the directory tree. It never creates database
// files themselves (each owner opens its own); it only guarantees the
// vault parent exists with restrictive permissions.
func EnsureLayout(l Layout) error {
	dirs := []string{
		l.Root,
		l.BlobsDir(),
		l.BackupsDir(),
		filepath.Join(l.Root, "projects"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
