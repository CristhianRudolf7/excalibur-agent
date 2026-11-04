package store

import (
	"os"
	"path/filepath"
	"strings"
)

func detectPlatformNetworkFS(dir string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	m := mountForDir(parseMounts(string(data)), abs)
	if m == nil {
		return false
	}
	return networkFSTypes[strings.ToLower(m.fsType)]
}
