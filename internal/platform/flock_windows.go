package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// WindowsFileLock serializes cooperating writers using an exclusive byte-range
// lock (Appendix design note [Concurrencia-archivos]). Use a separate lock file.
type WindowsFileLock struct{}

var _ FileLock = WindowsFileLock{}

// Acquire creates the lock file and parents as needed, then blocks for an
// exclusive lock. The returned release unlocks and closes, idempotently.
func (WindowsFileLock) Acquire(path string) (Release, error) {
	f, err := openWindowsLockFile(path)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		f.Close()
		return nil, err
	}
	return windowsLockRelease(f, overlapped), nil
}

// AcquireContext polls a nonblocking exclusive lock, honoring cancellation.
func (WindowsFileLock) AcquireContext(ctx context.Context, path string) (Release, error) {
	f, err := openWindowsLockFile(path)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
		if err == nil {
			return windowsLockRelease(f, overlapped), nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func openWindowsLockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
}

func windowsLockRelease(f *os.File, overlapped *windows.Overlapped) Release {
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() {
			err = errors.Join(windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped), f.Close())
		})
		return err
	}
}
