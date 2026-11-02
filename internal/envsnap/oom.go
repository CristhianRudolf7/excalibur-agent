package envsnap

import (
	"path/filepath"
	"strconv"
	"strings"
)

// OOMWatch holds a command's pre-launch counters. It has no goroutines and does
// not change the cgroup. Each command owns a watch, including background jobs.
type OOMWatch struct {
	limits   ResourceLimits
	counters []oomCounter
}

type oomCounter struct {
	path, key string
	before    uint64
	// v1 failcnt records allocation failures, not necessarily killed processes.
	pressureOnly bool
}

// OOMObservation is frozen at process completion, so later unrelated events
// cannot change the diagnosis when a background session is polled again.
type OOMObservation struct {
	Killed           bool
	Pressure         bool
	CounterAvailable bool
	MemoryBytes      int64
	RetryJobs        int
}

func readCounter(path, key string) (uint64, bool) {
	raw := readResource(path)
	if key != "" {
		found := false
		for _, line := range strings.Split(raw, "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == key {
				raw, found = f[1], true
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	return n, err == nil
}

// WatchOOM samples the nearest readable memory controller before launch.
// v2 memory.events includes descendant kills, so a shell or build driver need
// not itself exit 137 for a killed worker to be reported (Section 13.5).
func (r ResourceReader) WatchOOM() *OOMWatch {
	if !EffectiveLimitsEnabled() {
		return nil
	}
	w := &OOMWatch{limits: r.Limits()}
	seen := map[string]bool{}
	for _, c := range r.locations() {
		for _, dir := range c.ancestors() {
			file, key := "memory.oom_control", "oom_kill"
			if c.v2 {
				file, key = "memory.events", "oom_kill"
			}
			path := filepath.Join(dir, file)
			if value, ok := readCounter(path, key); ok {
				if !seen[path] {
					w.counters = append(w.counters, oomCounter{path: path, key: key, before: value})
					seen[path] = true
				}
				break
			}
			if !c.v2 {
				path = filepath.Join(dir, "memory.failcnt")
				if value, ok := readCounter(path, ""); ok {
					if !seen[path] {
						w.counters = append(w.counters, oomCounter{path: path, before: value, pressureOnly: true})
						seen[path] = true
					}
					break
				}
			}
		}
	}
	return w
}

// Finish compares the same files and keys; missing or reset counters never
// establish a kill. failcnt alone can only support a possible OOM diagnosis.
func (w *OOMWatch) Finish() OOMObservation {
	if w == nil || !EffectiveLimitsEnabled() {
		return OOMObservation{}
	}
	o := OOMObservation{MemoryBytes: w.limits.MemoryBytes, RetryJobs: max(1, w.limits.ParallelJobs()/2)}
	for _, c := range w.counters {
		after, ok := readCounter(c.path, c.key)
		if !ok {
			continue
		}
		if !c.pressureOnly {
			o.CounterAvailable = true
		}
		if after <= c.before {
			continue
		}
		if c.pressureOnly {
			o.Pressure = true
		} else {
			o.Killed = true
		}
	}
	return o
}
