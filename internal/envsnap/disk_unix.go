//go:build linux || darwin

package envsnap

import (
	"golang.org/x/sys/unix"
)

// detectDiskFree reports free bytes on the filesystem holding dir.
func detectDiskFree(dir string) (int64, bool) {
	if dir == "" {
		dir = "/tmp"
	}
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
