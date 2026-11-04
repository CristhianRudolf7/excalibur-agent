package store

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// UNC shares and mapped remote drives need rollback journals rather than WAL,
// whose shared-memory coordination is unsupported across network filesystems.
func detectPlatformNetworkFS(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	volume := filepath.VolumeName(abs)
	return windowsNetworkVolume(volume, func(root string) uint32 {
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			return windows.DRIVE_UNKNOWN
		}
		return windows.GetDriveType(p)
	})
}
