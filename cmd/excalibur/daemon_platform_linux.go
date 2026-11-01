package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// daemonSampler is the resource sampler behind the RAM watchdog
// (Section 5.2): /proc and cgroup counters on Linux.
func daemonSampler() platform.ResourceSampler { return platform.LinuxResourceSampler{} }

// physicalRAMBytes returns MemTotal from /proc/meminfo, or zero when
// the figure is unavailable.
func physicalRAMBytes() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0
			}
			return kb * 1024
		}
	}
	return 0
}

// daemonKeyring is the master-key source of the vault: Secret Service
// over D-Bus on Linux (Section 20.1). Unreachable keyrings leave the
// vault passphrase-only.
func daemonKeyring() platform.Keyring { return platform.LinuxKeyring{} }
