//go:build !linux && !darwin && !windows

package cli

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

func cliRuntimeDir() string {
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		return daemon.RuntimeAddrDir(v)
	}
	uid := os.Getuid()
	if dir := "/run/user/" + strconv.Itoa(uid); dirExists(dir) {
		return daemon.RuntimeAddrDir(dir)
	}
	return daemon.RuntimeAddrDir(filepath.Join(os.TempDir(), "excalibur-"+strconv.Itoa(uid)))
}
