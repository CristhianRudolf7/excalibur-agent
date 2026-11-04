//go:build !windows

package platform

import "os"

// SyncDir persists directory entries after renames.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
