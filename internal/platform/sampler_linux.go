//go:build linux

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LinuxResourceSampler reads RAM, CPU, process and OOM counters from
// /proc and cgroup v2 (Sections 5.2, 22.2). The zero value samples the
// live system; CgroupRoot overrides /sys/fs/cgroup for tests.
type LinuxResourceSampler struct {
	// CgroupRoot overrides the cgroup v2 mount point. Empty means
	// /sys/fs/cgroup.
	CgroupRoot string
}

// Compile-time interface check.
var _ ResourceSampler = LinuxResourceSampler{}

func (s LinuxResourceSampler) root() string {
	if s.CgroupRoot != "" {
		return s.CgroupRoot
	}
	return "/sys/fs/cgroup"
}

// Sample implements platform.ResourceSampler. A pid <= 0 samples the
// calling process. RSSBytes comes from /proc/<pid>/status (VmRSS);
// CPUSeconds from utime+stime in /proc/<pid>/stat; Processes from the
// owning cgroup's pids.current (falling back to the thread count);
// OOMKills from the owning cgroup's memory.events (0 without cgroup).
func (s LinuxResourceSampler) Sample(pid int) (ResourceSample, error) {
	if pid <= 0 {
		pid = os.Getpid()
	}
	rss, err := procRSS(pid)
	if err != nil {
		return ResourceSample{}, err
	}
	cpu, err := procCPUSeconds(pid)
	if err != nil {
		return ResourceSample{}, err
	}
	processes := 1
	oom := 0
	if dir, cerr := cgroupDirForPID(pid, s.root()); cerr == nil {
		if n, err := readUintFile(filepath.Join(dir, "pids.current")); err == nil {
			processes = int(n)
		} else {
			// No pids controller: count threads instead.
			if n, err := procThreadCount(pid); err == nil {
				processes = n
			}
		}
		if n, err := readOOMKills(filepath.Join(dir, "memory.events")); err == nil {
			oom = n
		}
	} else {
		if n, err := procThreadCount(pid); err == nil {
			processes = n
		}
	}
	return ResourceSample{
		At:         time.Now(),
		RSSBytes:   rss,
		CPUSeconds: cpu,
		Processes:  processes,
		OOMKills:   oom,
	}, nil
}

// procRSS parses VmRSS from /proc/<pid>/status (reported in kB).
func procRSS(pid int) (uint64, error) {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, errors.New("platform: malformed VmRSS line")
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return kb * 1024, nil
	}
	return 0, errors.New("platform: VmRSS not found")
}

// procCPUSeconds sums utime+stime from /proc/<pid>/stat field 14+15,
// divided by the clock tick rate. The comm field may contain spaces
// and parentheses, so fields are located from the last ")".
func procCPUSeconds(pid int) (float64, error) {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	rest := string(raw)
	end := strings.LastIndex(rest, ")")
	if end < 0 {
		return 0, errors.New("platform: malformed stat")
	}
	fields := strings.Fields(rest[end+1:])
	if len(fields) < 13 {
		return 0, errors.New("platform: malformed stat fields")
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}
	return float64(utime+stime) / float64(clockTicks()), nil
}

// procThreadCount counts entries in /proc/<pid>/task.
func procThreadCount(pid int) (int, error) {
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "task"))
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// clockTicks returns the kernel HZ value. USER_HZ is 100 on every
// supported Linux architecture, so the constant is used directly.
func clockTicks() uint64 {
	return 100
}

// readUintFile reads a decimal counter file (pids.current).
func readUintFile(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
}

// readOOMKills extracts the oom_kill counter from a memory.events
// file ("oom_kill <n>" line). A missing file yields an error so the
// caller can distinguish "no cgroup" from "zero kills".
func readOOMKills(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "oom_kill" {
			n, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, err
			}
			return n, nil
		}
	}
	return 0, errors.New("platform: oom_kill not found in memory.events")
}
