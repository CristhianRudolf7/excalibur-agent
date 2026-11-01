//go:build linux

package platform

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// SignalTarget describes a process that terminal commands must not signal.
type SignalTarget struct {
	PID, Group    int
	Name, Command string
}

// ProtectedSignalTargets includes the harness, its parent and both groups.
func ProtectedSignalTargets() ([]SignalTarget, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self, parent := os.Getpid(), os.Getppid()
	groups := map[int]bool{}
	for _, pid := range []int{self, parent} {
		g, e := syscall.Getpgid(pid)
		if e != nil {
			return nil, e
		}
		groups[g] = true
	}
	var targets []SignalTarget
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		group, e := syscall.Getpgid(pid)
		if e != nil {
			continue
		}
		if pid != self && pid != parent && !groups[group] {
			continue
		}
		name, e := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if e != nil {
			return nil, e
		}
		cmd, e := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if e != nil {
			return nil, e
		}
		targets = append(targets, SignalTarget{pid, group, strings.TrimSpace(string(name)), strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " "))})
	}
	return targets, nil
}

// Reexec replaces the current image without changing PID or signal ownership.
func Reexec(argv, env []string) error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, env)
}
