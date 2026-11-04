package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// SyncDir attempts to flush directory metadata using a directory handle.
func SyncDir(dir string) error {
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err == nil {
		defer windows.CloseHandle(h)
		err = windows.FlushFileBuffers(h)
	}
	// Windows can reject directory opens for write access or directory flushes.
	// NTFS journals metadata updates for recovery; file contents have already
	// been flushed before rename. Unsupported directory flushes are best effort.
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return nil
	}
	if err != nil {
		return &os.PathError{Op: "sync", Path: dir, Err: err}
	}
	return nil
}
