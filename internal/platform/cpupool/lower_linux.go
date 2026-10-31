//go:build linux

package cpupool

import (
	"golang.org/x/sys/unix"
)

// lowerCurrentThread raises the calling worker thread's nice value by
// 10 (Linux: setpriority on the tid). Each worker locks its OS thread
// first, so only that worker is affected (Section 3.2).
func lowerCurrentThread() error {
	const increment = 10
	tid := unix.Gettid()
	cur, err := getThreadNice(unix.PRIO_PROCESS, tid)
	if err != nil {
		return err
	}
	want := cur + increment
	if want <= cur {
		return nil
	}
	if want > 19 {
		want = 19
	}
	return unix.Setpriority(unix.PRIO_PROCESS, tid, want)
}

// getThreadNice wraps unix.Getpriority: the raw getpriority(2) result
// is 20-nice, so the wrapper converts back to the -20..19 value.
func getThreadNice(which, who int) (int, error) {
	raw, err := unix.Getpriority(which, who)
	if err != nil {
		return 0, err
	}
	return 20 - raw, nil
}
