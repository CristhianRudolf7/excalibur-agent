package main

import (
	"context"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func listenDaemonStop(ctx context.Context, cancel context.CancelFunc) (func(), error) {
	return platform.ListenDaemonStop(ctx, cancel)
}
func stopDaemonProcess(pid int, runtime string) error {
	// Match the daemon service-loop shutdown grace period.
	if err := platform.RequestDaemonStop(pid, 5*time.Second); err != nil {
		return fmt.Errorf("daemon stop: process %d: %w", pid, err)
	}
	return clearStoppedDaemonFiles(runtime, pid)
}
