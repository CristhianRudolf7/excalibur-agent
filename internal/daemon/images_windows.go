package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *FileImageStore) binaryPaths() (cur, next, prev string) {
	cur = joinBin(s.Dir)
	return cur, cur + ".next", cur + ".old"
}
func joinBin(dir string) string { return filepath.Join(dir, "excalibur.exe") }

// moveWindowsImage renames an in-use executable out of the destination
// before placing the replacement, and restores it if that second move fails.
func moveWindowsImage(candidate, current, backup string) error {
	if _, err := os.Stat(candidate); err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	saved := false
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, backup); err != nil {
			return err
		}
		saved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(candidate, current); err != nil {
		if saved {
			if restoreErr := os.Rename(backup, current); restoreErr != nil {
				return fmt.Errorf("daemon: image move failed (%v), restore failed: %w", err, restoreErr)
			}
		}
		return err
	}
	return nil
}
func (s *FileImageStore) Swap() error {
	cur, next, prev := s.binaryPaths()
	return moveWindowsImage(next, cur, prev)
}
func (s *FileImageStore) RollbackBinary() error {
	cur, _, prev := s.binaryPaths()
	return moveWindowsImage(prev, cur, cur+".bad")
}

// CleanupPreviousImage runs on the next binary start. A still-running old
// process can hold the image open; a later start retries the cleanup.
func CleanupPreviousImage() {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(executable + ".old")
	_ = os.Remove(executable + ".bad")
}
