package store

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// darwinNetworkFSTypes are the macOS f_fstypename values of network
// filesystems; they share the Linux names where those exist.
var darwinNetworkFSTypes = map[string]bool{"afpfs": true, "webdav": true, "osxfuse": true, "macfuse": true}

// detectPlatformNetworkFS asks statfs(2) for the filesystem type of dir
// (macOS has no /proc/mounts). The nearest existing ancestor is probed
// so a database directory that is about to be created still resolves.
func detectPlatformNetworkFS(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for {
		var st unix.Statfs_t
		if err := unix.Statfs(abs, &st); err == nil {
			return isDarwinNetworkFSType(unix.ByteSliceToString(st.Fstypename[:]))
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}

// isDarwinNetworkFSType reports whether a statfs type name is a network
// filesystem.
func isDarwinNetworkFSType(name string) bool {
	name = strings.ToLower(name)
	return networkFSTypes[name] || darwinNetworkFSTypes[name]
}
