//go:build linux

package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// readProcessTable snapshots the process table from /proc. Processes
// that disappear while the snapshot is taken are skipped.
func readProcessTable() processTable {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var table processTable
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		info, ok := parseProcStat(pid, string(raw))
		if !ok {
			continue
		}
		if args, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline")); err == nil && len(args) > 0 {
			if len(args) > 4096 {
				args = args[:4096]
			}
			info.Argv = strings.TrimSpace(strings.ReplaceAll(string(args), "\x00", " "))
		}
		table = append(table, info)
	}
	return table
}

// parseProcStat reads the parent and group ids and the command name from
// one /proc/<pid>/stat line. The command name is wrapped in parentheses and
// may itself contain spaces or parentheses, so the last ")" ends it.
func parseProcStat(pid int, raw string) (procInfo, bool) {
	left, right := strings.Index(raw, "("), strings.LastIndex(raw, ")")
	if left < 0 || right <= left {
		return procInfo{}, false
	}
	fields := strings.Fields(raw[right+1:])
	if len(fields) < 3 {
		return procInfo{}, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	pgid, err2 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil {
		return procInfo{}, false
	}
	return procInfo{PID: pid, PPID: ppid, PGID: pgid, Comm: raw[left+1 : right]}, true
}

// processHoldsTerminal reports whether pid has a terminal device open on
// any descriptor. A process that prompts through /dev/tty of the harness
// keeps such a descriptor, which is how a silent session is recognised as
// waiting for input.
func processHoldsTerminal(pid int) bool {
	if pid <= 0 {
		return false
	}
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "/dev/pts/") || strings.HasPrefix(target, "/dev/tty") {
			return true
		}
	}
	return false
}

// signalGroup sends SIGTERM, or SIGKILL when kill is set, to the process
// group pgid. A group with no members reports success.
func signalGroup(pgid int, kill bool) error {
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	err := syscall.Kill(-pgid, sig)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

// groupAlive reports whether any process still belongs to group pgid.
func groupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || err == syscall.EPERM
}

// processTreeListeningTCP protects services by actual socket ownership,
// regardless of whether their launch command looks like a server.
func processTreeListeningTCP(root int) bool {
	listening := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 9 && fields[3] == "0A" {
				listening[fields[9]] = true
			}
		}
	}
	if len(listening) == 0 {
		return false
	}
	for _, p := range readProcessTable().tree(root) {
		dir := filepath.Join("/proc", strconv.Itoa(p.PID), "fd")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join(dir, entry.Name()))
			if err != nil || !strings.HasPrefix(target, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			if listening[inode] {
				return true
			}
		}
	}
	return false
}
