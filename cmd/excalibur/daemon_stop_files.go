package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

// clearStoppedDaemonFiles removes rendezvous files after confirmed process
// exit, including forced Windows stops that cannot run the daemon's cleanup.
func clearStoppedDaemonFiles(runtime string, pid int) error {
	pidPath := filepath.Join(runtime, daemonPidFile)
	raw, err := os.ReadFile(pidPath)
	if err == nil && strings.TrimSpace(string(raw)) != strconv.Itoa(pid) {
		// Another daemon has published its PID since the stop began.
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	daemon.RemoveAddrFile(runtime)
	if err := os.Remove(pidPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
