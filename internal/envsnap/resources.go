package envsnap

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// EffectiveLimitsSwitch gates resource reporting and OOM diagnostics together.
const EffectiveLimitsSwitch = "EXCALIBUR_EFFECTIVE_LIMITS"

func EffectiveLimitsEnabled() bool { return ParseBool(os.Getenv(EffectiveLimitsSwitch), true) }

// ResourceReader reads Linux resource files. Empty roots use the live system;
// explicit roots let callers exercise container layouts without real limits.
type ResourceReader struct {
	ProcRoot string
	SysRoot  string
}

// ResourceLimits separates host capacity from the effective container budget.
// CPUs may be fractional; MemoryBytes includes memory.high as a usable budget.
type ResourceLimits struct {
	HostCPUs      int
	CPUs          float64
	HostMemory    int64
	MemoryBytes   int64
	MemoryLimited bool
}

// ParallelJobs reserves 1.5 GiB per build/test worker and respects fractional
// CPU quotas. A serial job is the minimum useful recommendation.
func (l ResourceLimits) ParallelJobs() int {
	jobs := l.CPUs
	if l.MemoryBytes > 0 || l.MemoryLimited {
		jobs = math.Min(jobs, float64(l.MemoryBytes)/(1.5*(1<<30)))
	}
	return max(1, int(math.Floor(jobs)))
}

func (r ResourceReader) roots() (string, string) {
	proc, sys := r.ProcRoot, r.SysRoot
	if proc == "" {
		proc = "/proc"
	}
	if sys == "" {
		sys = "/sys"
	}
	return proc, sys
}

func readResource(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

func positiveLimit(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

type cgroupLocation struct {
	dir, root string
	v2        bool
}

// locations resolves membership through mountinfo, including subtree mounts
// and cgroup namespaces. Ancestors up to the visible mount remain effective.
func (r ResourceReader) locations() []cgroupLocation {
	if runtime.GOOS != "linux" && r.ProcRoot == "" && r.SysRoot == "" {
		return nil
	}
	proc, sys := r.roots()
	members := map[string]string{}
	for _, line := range strings.Split(readResource(filepath.Join(proc, "self/cgroup")), "\n") {
		f := strings.SplitN(line, ":", 3)
		if len(f) != 3 {
			continue
		}
		for _, controller := range strings.Split(f[1], ",") {
			members[controller] = f[2]
		}
	}
	var out []cgroupLocation
	for _, line := range strings.Split(readResource(filepath.Join(proc, "self/mountinfo")), "\n") {
		left, right, ok := strings.Cut(line, " - ")
		lf, rf := strings.Fields(left), strings.Fields(right)
		if !ok || len(lf) < 5 || len(rf) < 3 || (rf[0] != "cgroup" && rf[0] != "cgroup2") {
			continue
		}
		v2 := rf[0] == "cgroup2"
		controllers := strings.Split(rf[2], ",")
		if v2 {
			controllers = []string{""}
		}
		for _, controller := range controllers {
			member, exists := members[controller]
			if !exists {
				continue
			}
			mountRoot, mountPoint := unescapeMount(lf[3]), unescapeMount(lf[4])
			// Remap only sysfs paths when a fake root is supplied.
			if r.SysRoot != "" {
				rel, err := filepath.Rel("/sys", mountPoint)
				if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
					continue
				}
				mountPoint = filepath.Join(sys, rel)
			}
			rel, err := filepath.Rel(mountRoot, member)
			if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
				// A cgroup namespace reports membership relative to its own
				// root even when mountinfo retains the host subtree root.
				rel = strings.TrimPrefix(filepath.Clean("/"+member), "/")
				if st, statErr := os.Stat(filepath.Join(mountPoint, rel)); statErr != nil || !st.IsDir() {
					continue
				}
			}
			out = append(out, cgroupLocation{filepath.Join(mountPoint, rel), mountPoint, v2})
			break
		}
	}
	if len(out) > 0 {
		return out
	}
	// Minimal proc/sys environments may omit mountinfo. Use conventional mounts
	// only for known memberships; never inspect unrelated controller subtrees.
	for controller, member := range members {
		if controller != "" && controller != "memory" && controller != "cpu" && controller != "cpuset" {
			continue
		}
		root := filepath.Join(sys, "fs/cgroup")
		if controller != "" {
			root = filepath.Join(root, controller)
		}
		out = append(out, cgroupLocation{filepath.Join(root, filepath.Clean("/"+member)), root, controller == ""})
	}
	return out
}

func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

func (c cgroupLocation) ancestors() []string {
	var dirs []string
	for dir := c.dir; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == c.root || filepath.Dir(dir) == dir {
			break
		}
	}
	return dirs
}

// Limits returns best-effort facts, retaining host values on missing, malformed
// or unlimited controls. Reading host facts also works with the switch disabled.
func (r ResourceReader) Limits() ResourceLimits {
	proc, sys := r.roots()
	l := ResourceLimits{HostCPUs: runtime.NumCPU()}
	if n := cpuSetCount(readResource(filepath.Join(sys, "devices/system/cpu/online"))); n > 0 {
		l.HostCPUs = n
	}
	for _, line := range strings.Split(readResource(filepath.Join(proc, "meminfo")), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "MemTotal:" && f[2] == "kB" {
			kb := positiveLimit(f[1])
			if kb <= math.MaxInt64/1024 {
				l.HostMemory = kb * 1024
			}
		}
	}
	l.CPUs, l.MemoryBytes = float64(l.HostCPUs), l.HostMemory
	if !EffectiveLimitsEnabled() {
		return l
	}
	for _, c := range r.locations() {
		for _, dir := range c.ancestors() {
			memFiles := []string{"memory.limit_in_bytes"}
			if c.v2 {
				memFiles = []string{"memory.max", "memory.high"}
			}
			for _, file := range memFiles {
				n, err := strconv.ParseInt(readResource(filepath.Join(dir, file)), 10, 64)
				// v1 uses a page-rounded LONG_MAX sentinel for unlimited memory.
				if err == nil && n >= 0 && n < 1<<60 && ((l.MemoryBytes == 0 && !l.MemoryLimited) || n <= l.MemoryBytes) {
					l.MemoryBytes, l.MemoryLimited = n, true
				}
			}
			quota, period := int64(0), int64(0)
			if c.v2 {
				f := strings.Fields(readResource(filepath.Join(dir, "cpu.max")))
				if len(f) == 2 {
					quota, period = positiveLimit(f[0]), positiveLimit(f[1])
				}
			} else {
				quota = positiveLimit(readResource(filepath.Join(dir, "cpu.cfs_quota_us")))
				period = positiveLimit(readResource(filepath.Join(dir, "cpu.cfs_period_us")))
			}
			if quota > 0 && period > 0 {
				l.CPUs = math.Min(l.CPUs, float64(quota)/float64(period))
			}
			for _, file := range []string{"cpuset.cpus.effective", "cpuset.effective_cpus", "cpuset.cpus"} {
				if n := cpuSetCount(readResource(filepath.Join(dir, file))); n > 0 {
					l.CPUs = math.Min(l.CPUs, float64(n))
					break
				}
			}
		}
	}
	return l
}

// cpuSetCount counts sorted, disjoint kernel CPU ranges without allocating a
// map proportional to CPU identifiers. Malformed sets degrade to absence.
func cpuSetCount(s string) int {
	total, previous := 0, -1
	for _, part := range strings.Split(s, ",") {
		lo, hi, ranged := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil || a < 0 || a <= previous {
			return 0
		}
		b := a
		if ranged {
			b, err = strconv.Atoi(hi)
		}
		if err != nil || b < a || b > 1<<20 {
			return 0
		}
		total += b - a + 1
		previous = b
	}
	return total
}

// MemoryLabel uses explicit binary units for command diagnostics.
func MemoryLabel(n int64) string {
	if n < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%gGiB", float64(n)/(1<<30))
}
