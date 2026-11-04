//go:build darwin

package platform

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// DarwinFileLock serializes writers across processes with an advisory
// flock(2) (Appendix design note [Concurrencia-archivos]). It only
// coordinates cooperating processes; a non-cooperating writer is
// unaffected.
type DarwinFileLock struct{}

// Compile-time interface check.
var _ FileLock = DarwinFileLock{}

// Acquire implements platform.FileLock: it opens (creating with 0600)
// the lock file, creating parent directories as needed, and takes an
// exclusive blocking flock. The returned Release unlocks and closes;
// it is idempotent.
func (DarwinFileLock) Acquire(path string) (Release, error) {
	f, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return releaseFor(f), nil
}

// AcquireContext is the context-aware variant of Acquire. The blocking
// flock(2) cannot be interrupted directly, so it polls with
// LOCK_EX|LOCK_NB and honours ctx cancellation. The wave-0 interface
// comment promises ctx expiry; this additive method delivers it
// without changing the shared FileLock contract.
func (DarwinFileLock) AcquireContext(ctx context.Context, path string) (Release, error) {
	f, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
			return releaseFor(f), nil
		} else if err != unix.EWOULDBLOCK {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func openLockFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

func releaseFor(f *os.File) Release {
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() {
			_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
			err = f.Close()
		})
		return err
	}
}
