//go:build !linux && !darwin && !windows

package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

func listenDaemonStop(context.Context, context.CancelFunc) (func(), error) { return func() {}, nil }
func stopDaemonProcess(pid int, _ string) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("daemon stop: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("daemon stop: signal %d: %w", pid, err)
	}
	return nil
}
