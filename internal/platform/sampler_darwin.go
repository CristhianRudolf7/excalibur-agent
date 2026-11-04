//go:build darwin

package platform

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// DarwinResourceSampler observes current RSS and cumulative CPU on
// macOS (Sections 5.2, 22.2). No cgroup OOM counters exist on Darwin.
type DarwinResourceSampler struct{}

var _ ResourceSampler = DarwinResourceSampler{}

// Sample uses getrusage for self CPU and ps for current RSS. Other
// processes use ps CPU time. Processes counts live descendants of pid,
// excluding pid itself and zombies; OOMKills is always zero.
func (DarwinResourceSampler) Sample(pid int) (ResourceSample, error) {
	if pid <= 0 {
		pid = os.Getpid()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	self := pid == os.Getpid()
	listing := exec.CommandContext(ctx, "/bin/ps", "-A", "-o", "pid=,ppid=,stat=,rss=,time=")
	raw, err := listing.Output()
	if err != nil {
		return ResourceSample{}, err
	}
	rss, cpu, processes, err := parseDarwinSample(string(raw), pid, listing.Process.Pid)
	if err != nil {
		return ResourceSample{}, err
	}
	if self {
		var usage unix.Rusage
		if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err != nil {
			return ResourceSample{}, err
		}
		cpu = float64(usage.Utime.Sec) + float64(usage.Stime.Sec) + (float64(usage.Utime.Usec)+float64(usage.Stime.Usec))/1e6
	}
	return ResourceSample{At: time.Now(), RSSBytes: rss, CPUSeconds: cpu, Processes: processes}, nil
}

// Select RSS and CPU by exact PID from the same snapshot used to count
// descendants. RSS is KiB in ps, including on Apple Silicon; it is neither
// bytes nor the RSS of the daemon, its other workers, or the ps observer.
func parseDarwinSample(output string, root, observer int) (uint64, float64, int, error) {
	var rss uint64
	var cpu float64
	found := false
	var descendants strings.Builder
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 5 {
			return 0, 0, 0, errors.New("platform: malformed ps sample")
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0, 0, 0, err
		}
		if pid == root {
			if found {
				return 0, 0, 0, errors.New("platform: duplicate ps sample pid")
			}
			rss, cpu, err = parseDarwinUsage(strings.Join(fields[3:], " "), true)
			if err != nil {
				return 0, 0, 0, err
			}
			found = true
		}
		descendants.WriteString(strings.Join(fields[:3], " "))
		descendants.WriteByte('\n')
	}
	if !found {
		return 0, 0, 0, unix.ESRCH
	}
	count, err := parseDarwinDescendants(descendants.String(), root, observer)
	return rss, cpu, count, err
}

func parseDarwinUsage(output string, withCPU bool) (uint64, float64, error) {
	fields := strings.Fields(output)
	want := 1
	if withCPU {
		want = 2
	}
	if len(fields) != want {
		return 0, 0, errors.New("platform: malformed ps resource output")
	}
	kb, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || kb > math.MaxUint64/1024 {
		return 0, 0, errors.New("platform: invalid ps RSS")
	}
	var cpu float64
	if withCPU {
		cpu, err = parseDarwinCPUTime(fields[1])
		if err != nil {
			return 0, 0, err
		}
	}
	return kb * 1024, cpu, nil
}

// parseDarwinCPUTime accepts ps cumulative time as mm:ss.fraction,
// hh:mm:ss.fraction or dd-hh:mm:ss.fraction.
func parseDarwinCPUTime(value string) (float64, error) {
	var days uint64
	hasDays := false
	if d, rest, found := strings.Cut(value, "-"); found {
		n, err := strconv.ParseUint(d, 10, 32)
		if err != nil {
			return 0, errors.New("platform: invalid ps CPU days")
		}
		days, value = n, rest
		hasDays = true
	}
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 || (hasDays && len(parts) != 3) {
		return 0, errors.New("platform: invalid ps CPU time")
	}
	seconds, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds >= 60 {
		return 0, errors.New("platform: invalid ps CPU seconds")
	}
	total := float64(days)*86400 + seconds
	multiplier := float64(60)
	for i := len(parts) - 2; i >= 0; i-- {
		n, err := strconv.ParseUint(parts[i], 10, 32)
		if err != nil || (i > 0 && n >= 60) {
			return 0, errors.New("platform: invalid ps CPU component")
		}
		total += float64(n) * multiplier
		multiplier *= 60
	}
	return total, nil
}

func parseDarwinDescendants(output string, root int, exclude ...int) (int, error) {
	children := map[int][]int{}
	live := map[int]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			return 0, errors.New("platform: malformed ps process output")
		}
		pid, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		if e1 != nil || e2 != nil || pid < 0 || parent < 0 {
			return 0, errors.New("platform: invalid ps process id")
		}
		children[parent] = append(children[parent], pid)
		live[pid] = !strings.Contains(fields[2], "Z")
	}
	seen := map[int]bool{root: true}
	// Exclude the ps observation process itself when sampling this daemon.
	for _, pid := range exclude {
		seen[pid] = true
	}
	queue := []int{root}
	count := 0
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, pid := range children[parent] {
			if seen[pid] {
				continue
			}
			seen[pid] = true
			if live[pid] {
				count++
			}
			queue = append(queue, pid)
		}
	}
	return count, nil
}

// PhysicalMemoryBytes returns installed physical RAM from hw.memsize.
func PhysicalMemoryBytes() (uint64, error) { return unix.SysctlUint64("hw.memsize") }
