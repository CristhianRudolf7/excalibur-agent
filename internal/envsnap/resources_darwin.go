//go:build darwin

package envsnap

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func (r ResourceReader) hostResources() ResourceLimits {
	// Explicit fixture roots always retain proc/sys semantics on every OS.
	if r.ProcRoot != "" || r.SysRoot != "" {
		return r.fixtureHostResources()
	}
	l := ResourceLimits{HostCPUs: runtime.NumCPU()}
	if n, err := unix.SysctlUint32("hw.ncpu"); err == nil && n > 0 {
		l.HostCPUs = int(n)
	}
	if n, err := unix.SysctlUint64("hw.memsize"); err == nil && n <= math.MaxInt64 {
		l.HostMemory = int64(n)
	}
	pageSize, _ := unix.SysctlUint32("hw.pagesize")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "/usr/bin/vm_stat").Output(); err == nil {
		l.AvailableMemory = parseDarwinAvailableMemory(string(output), int64(pageSize))
		if l.HostMemory > 0 {
			l.AvailableMemory = min(l.AvailableMemory, l.HostMemory)
		}
	}
	return l
}

func (r ResourceReader) fixtureHostResources() ResourceLimits {
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
	return l
}

// parseDarwinAvailableMemory estimates reclaimable bytes from vm_stat, whose
// displayed free count excludes speculative pages. Purgeable pages overlap
// other queues and are omitted to avoid counting them twice.
func parseDarwinAvailableMemory(output string, pageSize int64) int64 {
	counts := map[string]int64{}
	for _, line := range strings.Split(output, "\n") {
		if _, header, ok := strings.Cut(line, "page size of "); ok {
			fields := strings.Fields(header)
			if len(fields) > 0 {
				pageSize = positiveLimit(fields[0])
			}
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "Pages free" && name != "Pages inactive" && name != "Pages speculative" {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(value), "."), 10, 64)
		if err != nil || n < 0 {
			return 0
		}
		if _, duplicate := counts[name]; duplicate {
			return 0
		}
		counts[name] = n
	}
	if pageSize <= 0 || len(counts) != 3 {
		return 0
	}
	var pages int64
	for _, n := range counts {
		if n > math.MaxInt64-pages {
			return 0
		}
		pages += n
	}
	if pages > math.MaxInt64/pageSize {
		return 0
	}
	return pages * pageSize
}
