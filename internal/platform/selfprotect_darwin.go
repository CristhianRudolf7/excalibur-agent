//go:build darwin

package platform

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// SignalTarget describes a process that terminal commands must not signal.
type SignalTarget struct {
	PID, Group    int
	Name, Command string
}

// ProtectedSignalTargets includes the harness, its parent and both
// groups, using the kernel process snapshot instead of /proc.
func ProtectedSignalTargets() ([]SignalTarget, error) {
	self, parent := os.Getpid(), os.Getppid()
	groups := map[int]bool{}
	for _, pid := range []int{self, parent} {
		group, err := unix.Getpgid(pid)
		if err != nil {
			return nil, err
		}
		groups[group] = true
	}
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	return darwinSignalTargets(procs, self, parent, groups, func(pid int) ([]byte, error) { return unix.SysctlRaw("kern.procargs2", pid) }), nil
}

func darwinSignalTargets(procs []unix.KinfoProc, self, parent int, groups map[int]bool, readArgs func(int) ([]byte, error)) []SignalTarget {
	var targets []SignalTarget
	for _, proc := range procs {
		pid, group := int(proc.Proc.P_pid), int(proc.Eproc.Pgid)
		if pid <= 0 || (pid != self && pid != parent && !groups[group]) {
			continue
		}
		nameBytes := proc.Proc.P_comm[:]
		if end := bytes.IndexByte(nameBytes, 0); end >= 0 {
			nameBytes = nameBytes[:end]
		}
		name := string(nameBytes)
		if name == "" {
			name = "pid-" + strconv.Itoa(pid)
		}
		command := name
		if raw, err := readArgs(pid); err == nil {
			if _, args, err := parseDarwinProcArgs(raw); err == nil && len(args) > 0 {
				command = strings.Join(args, " ")
			}
		}
		// EPERM/EINVAL (other users, hardened processes or vanished pids)
		// must not drop a protected target: comm remains a useful fallback.
		targets = append(targets, SignalTarget{pid, group, name, command})
	}
	return targets
}

// parseDarwinProcArgs reads the native int32 argc, saved executable
// path, NUL padding and exactly argc arguments. It never includes the
// environment that follows argv. Both supported Darwin CPUs are LE.
func parseDarwinProcArgs(raw []byte) (string, []string, error) {
	invalid := errors.New("platform: malformed kern.procargs2")
	if len(raw) < 4 {
		return "", nil, invalid
	}
	argc := int(binary.LittleEndian.Uint32(raw[:4]))
	rest := raw[4:]
	end := bytes.IndexByte(rest, 0)
	if end <= 0 || argc > len(rest) {
		return "", nil, invalid
	}
	executable := string(rest[:end])
	rest = rest[end+1:]
	if argc == 0 {
		return executable, nil, nil
	}
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	args := make([]string, 0, argc)
	for range argc {
		end = bytes.IndexByte(rest, 0)
		if end < 0 {
			return "", nil, invalid
		}
		args = append(args, string(rest[:end]))
		rest = rest[end+1:]
	}
	return executable, args, nil
}

// Reexec replaces the current image without changing PID or signal
// ownership. Darwin execve supports the CLI's private request-file flow.
func Reexec(argv, env []string) error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, env)
}
