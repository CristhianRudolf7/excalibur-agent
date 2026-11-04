//go:build darwin

package tools

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"golang.org/x/sys/unix"
)

// readProcessTable snapshots kernel process identities. Command lines are
// best effort: ps supplies full arguments for build/package classification.
func readProcessTable() processTable {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, _ := exec.CommandContext(ctx, "/bin/ps", "-ww", "-axo", "pid=,args=").Output()
	argv := parseDarwinPSArgs(string(raw))
	table := make(processTable, 0, len(procs))
	for _, p := range procs {
		if p.Proc.P_pid <= 0 {
			continue
		}
		pid := int(p.Proc.P_pid)
		table = append(table, procInfo{PID: pid, PPID: int(p.Eproc.Ppid), PGID: int(p.Eproc.Pgid), Comm: unix.ByteSliceToString(p.Proc.P_comm[:]), Argv: argv[pid]})
	}
	return table
}

func parseDarwinPSArgs(raw string) map[int]string {
	out := map[int]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			continue
		}
		pid, err := strconv.Atoi(line[:i])
		if err != nil || pid <= 0 {
			continue
		}
		out[pid] = strings.TrimSpace(line[i:])
	}
	return out
}

// P_CONTROLT is defined in Darwin's <sys/proc.h>, but not exposed by x/sys.
const darwinPControlT = 0x00000002

func darwinHasControllingTerminal(p unix.KinfoProc) bool {
	// NODEV is (dev_t)-1. Device zero is valid; do not treat it as absent.
	return p.Proc.P_flag&darwinPControlT != 0 && p.Eproc.Tdev != -1
}

func processHoldsTerminal(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && darwinHasControllingTerminal(*p)
}

// signalGroup signals all members, including descendants whose parent exited.
// Reject group ids that would select the caller or broadcast to all processes.
func signalGroup(pgid int, kill bool) error {
	if pgid <= 1 {
		return syscall.EINVAL
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	return (platform.DarwinProcessGroup{}).Signal(pgid, sig)
}

func groupAlive(pgid int) bool {
	if pgid <= 1 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || err == syscall.EPERM
}

// lsof's AND selection restricts TCP listeners to this process tree. Without
// -a, -p and -i would select the union and protect unrelated services.
func processTreeListeningTCP(root int) bool {
	if root <= 0 {
		return false
	}
	tree := readProcessTable().tree(root)
	if len(tree) == 0 {
		return false
	}
	pids := make([]string, 0, len(tree))
	members := make(map[int]bool, len(tree))
	for _, p := range tree {
		pids = append(pids, strconv.Itoa(p.PID))
		members[p.PID] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, _ := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-iTCP", "-sTCP:LISTEN", "-Fp", "-p", strings.Join(pids, ",")).Output()
	for _, pid := range parseLsofPIDs(string(raw)) {
		if members[pid] {
			return true
		}
	}
	return false
}

// parseLsofPIDs reads newline-delimited -Fp fields, ignoring other fields and
// malformed records. lsof may repeat a process when multiple files match.
func parseLsofPIDs(raw string) []int {
	var out []int
	seen := map[int]bool{}
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "p") {
			continue
		}
		pid, err := strconv.Atoi(line[1:])
		if err == nil && pid > 0 && !seen[pid] {
			out = append(out, pid)
			seen[pid] = true
		}
	}
	return out
}
