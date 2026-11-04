package daemon

import (
	"fmt"
	"os"
)

// binaryPaths resolves the three image paths.
func (s *FileImageStore) binaryPaths() (cur, next, prev string) {
	return joinBin(s.Dir), joinBin(s.Dir) + ".next", joinBin(s.Dir) + ".prev"
}

func joinBin(dir string) string {
	if dir == "" {
		return "excalibur"
	}
	return dir + string(os.PathSeparator) + "excalibur"
}

// Swap implements ImageStore.
func (s *FileImageStore) Swap() error {
	cur, next, prev := s.binaryPaths()
	if _, err := os.Stat(next); err != nil {
		return fmt.Errorf("daemon: swap without staged binary: %w", err)
	}
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, prev); err != nil {
			return err
		}
	}
	if err := os.Rename(next, cur); err != nil {
		return err
	}
	return nil
}

// RollbackBinary implements ImageStore.
func (s *FileImageStore) RollbackBinary() error {
	cur, _, prev := s.binaryPaths()
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("daemon: no previous binary to roll back to: %w", err)
	}
	if err := os.Rename(cur, cur+".bad"); err != nil {
		return err
	}
	return os.Rename(prev, cur)
}

// CleanupPreviousImage has no work on this platform; .prev supports rollback.
func CleanupPreviousImage() {}
