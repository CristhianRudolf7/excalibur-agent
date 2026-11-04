//go:build !darwin

package envsnap

import (
	"math"
	"path/filepath"
	"runtime"
	"strings"
)

func (r ResourceReader) hostResources() ResourceLimits {
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
