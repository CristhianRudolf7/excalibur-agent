//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Low-priority policy for tool processes and background CPU threads
// (Sections 3.2, 5.2, 13.4).
//
// When the service manager delegates cgroup control, tool processes run
// in a cgroup v2 with a cpu.weight below the daemon default (100) and,
// via SetMemoryMax, an optional memory.max. Otherwise the policy falls
// back to nice(+10) and an idle/best-effort I/O class.
//
// Nice is per-thread on Linux (setpriority on the tid), which is what
// LowerCurrentThread relies on for the CPU worker pool.
const (
	// DefaultToolWeight is the cgroup v2 cpu.weight applied to tool
	// processes. The kernel default is 100; lower values get
	// proportionally less CPU time (range 1-10000).
	DefaultToolWeight = 20
	// DefaultToolNice is the fallback nice increment for tool
	// processes and CPU pool threads.
	DefaultToolNice = 10
)

// LinuxProcessPriority lowers tool and background-thread priority. The
// zero value uses /sys/fs/cgroup and DefaultToolWeight. Tests override
// CgroupRoot with a writable stub tree.
type LinuxProcessPriority struct {
	// CgroupRoot is the cgroup v2 mount point. Empty means
	// /sys/fs/cgroup.
	CgroupRoot string
	// Weight overrides DefaultToolWeight when positive.
	Weight uint64
	// Nice overrides DefaultToolNice. Values are clamped to the
	// kernel range -20..19.
	Nice int
}

// Compile-time interface check.
var _ ProcessPriority = LinuxProcessPriority{}

func (p LinuxProcessPriority) root() string {
	if p.CgroupRoot != "" {
		return p.CgroupRoot
	}
	return "/sys/fs/cgroup"
}

func (p LinuxProcessPriority) weight() uint64 {
	if p.Weight > 0 {
		return p.Weight
	}
	return DefaultToolWeight
}

func (p LinuxProcessPriority) nice() int {
	if p.Nice != 0 {
		return p.Nice
	}
	return DefaultToolNice
}

// LowerProcess implements platform.ProcessPriority. It first tries the
// delegated-cgroup path (write cpu.weight in the process's own cgroup)
// and falls back to nice+ioprio when cgroup control is not delegated.
func (p LinuxProcessPriority) LowerProcess(pid int) error {
	if pid <= 0 {
		return errors.New("platform: LowerProcess requires a positive pid")
	}
	if err := p.lowerViaCgroup(pid); err == nil {
		return nil
	}
	return p.lowerViaNice(pid)
}

// lowerViaCgroup writes cpu.weight into the cgroup owning pid. Any
// error means "not delegated" and the caller falls back to nice.
func (p LinuxProcessPriority) lowerViaCgroup(pid int) error {
	dir, err := cgroupDirForPID(pid, p.root())
	if err != nil {
		return err
	}
	weightFile := filepath.Join(dir, "cpu.weight")
	if _, err := os.Stat(weightFile); err != nil {
		return fmt.Errorf("platform: cgroup control not delegated: %w", err)
	}
	if err := os.WriteFile(weightFile, []byte(strconv.FormatUint(p.weight(), 10)), 0o644); err != nil {
		return fmt.Errorf("platform: cgroup control not delegated: %w", err)
	}
	return nil
}

// lowerViaNice applies the fallback policy: raise nice by the
// configured increment and move the process to idle I/O priority.
// The I/O step is best-effort: permission errors there never fail the
// call as long as nice was applied.
func (p LinuxProcessPriority) lowerViaNice(pid int) error {
	cur, err := getNice(unix.PRIO_PROCESS, pid)
	if err != nil {
		return err
	}
	want := cur + p.nice()
	if want < cur {
		want = cur
	}
	if want > 19 {
		want = 19
	}
	if want > cur {
		if err := unix.Setpriority(unix.PRIO_PROCESS, pid, want); err != nil {
			return err
		}
	}
	// Best effort: idle I/O class so bulk tool I/O does not stall the
	// interactive path.
	_ = setIOPrioIdle(pid)
	return nil
}

// LowerCurrentThread implements platform.ProcessPriority: it raises the
// calling worker thread's nice value (Linux: setpriority on the tid).
// Each CPU pool worker locks its OS thread first, so this only affects
// that worker.
func (p LinuxProcessPriority) LowerCurrentThread() error {
	tid := unix.Gettid()
	cur, err := getNice(unix.PRIO_PROCESS, tid)
	if err != nil {
		return err
	}
	want := cur + p.nice()
	if want <= cur {
		return nil
	}
	if want > 19 {
		want = 19
	}
	return unix.Setpriority(unix.PRIO_PROCESS, tid, want)
}

// SetMemoryMax writes memory.max for the cgroup owning pid. A limit of
// 0 clears the file to "max". It is a separate step from LowerProcess
// because the harness derives per-run limits from the orchestrator
// budget (Sections 5.2, 13.4); LowerProcess alone never imposes a
// memory cap.
func (p LinuxProcessPriority) SetMemoryMax(pid int, limitBytes uint64) error {
	if pid <= 0 {
		return errors.New("platform: SetMemoryMax requires a positive pid")
	}
	dir, err := cgroupDirForPID(pid, p.root())
	if err != nil {
		return err
	}
	value := "max"
	if limitBytes > 0 {
		value = strconv.FormatUint(limitBytes, 10)
	}
	return os.WriteFile(filepath.Join(dir, "memory.max"), []byte(value), 0o644)
}

// Delegated reports whether cgroup v2 control is delegated for pid:
// the process's cgroup directory exists and cpu.weight is writable.
func (p LinuxProcessPriority) Delegated(pid int) bool {
	dir, err := cgroupDirForPID(pid, p.root())
	if err != nil {
		return false
	}
	f, err := os.OpenFile(filepath.Join(dir, "cpu.weight"), os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// EffectiveNice reads the current nice value of pid. It is exported for
// the Section 3.7 verification (read back the effective priority with
// and without delegated cgroups).
func EffectiveNice(pid int) (int, error) {
	return getNice(unix.PRIO_PROCESS, pid)
}

// EffectiveWeight reads cpu.weight from a cgroup directory. It is
// exported for the Section 3.7 verification.
func EffectiveWeight(cgroupDir string) (uint64, error) {
	raw, err := os.ReadFile(filepath.Join(cgroupDir, "cpu.weight"))
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
}

// getNice wraps unix.Getpriority: the raw getpriority(2) result is
// 20-nice, so the wrapper converts back to the -20..19 nice value.
func getNice(which, who int) (int, error) {
	raw, err := unix.Getpriority(which, who)
	if err != nil {
		return 0, err
	}
	return 20 - raw, nil
}

// cgroupDirForPID resolves the cgroup v2 directory owning pid by// reading /proc/<pid>/cgroup ("0::/path") under root. A relative path
// of "/" maps to root itself. The returned directory may not exist;
// callers probe the control files to decide delegation.
func cgroupDirForPID(pid int, root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", err
	}
	rel := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format controllers:path, v2 uses "0::/path".
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" {
			rel = parts[2]
			break
		}
		// Fall back to the first entry on v1-only kernels.
		if rel == "" {
			rel = parts[2]
		}
	}
	if rel == "" {
		return "", fmt.Errorf("platform: no cgroup entry for pid %d", pid)
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return root, nil
	}
	// Guard against ".." escapes from a hostile /proc (never trust
	// kernel-parsed paths blindly in tests with stub roots).
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("platform: invalid cgroup path %q", rel)
	}
	return filepath.Join(root, clean), nil
}

// ioprio class shifts per linux/ioprio.h.
const (
	ioprioClassShift = 13
	ioprioClassIdle  = 3
)

// setIOPrioIdle moves pid to the idle I/O scheduling class via
// ioprio_set(IOPRIO_WHO_PROCESS, pid, class<<13). Best effort: the
// caller ignores the error.
func setIOPrioIdle(pid int) error {
	prio := uintptr(ioprioClassIdle << ioprioClassShift)
	_, _, errno := unix.Syscall(unix.SYS_IOPRIO_SET, 1, uintptr(pid), prio)
	if errno != 0 {
		return errno
	}
	return nil
}
