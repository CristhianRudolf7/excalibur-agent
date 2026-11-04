//go:build darwin

package platform

import (
	"math"
	"os/exec"
	"testing"
	"time"
)

func TestDarwinPSUsage(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		cpu     bool
		rss     uint64
		seconds float64
	}{
		{" 1234 \n", false, 1234 * 1024, 0},
		{" 1024  12:34.56\n", true, 1 << 20, 754.56},
		{" 1 02:03:04.50", true, 1024, 7384.5},
		{" 0 2-01:02:03", true, 0, 176523},
	} {
		rss, cpu, err := parseDarwinUsage(tc.raw, tc.cpu)
		if err != nil || rss != tc.rss || math.Abs(cpu-tc.seconds) > 1e-6 {
			t.Fatalf("%q = %d, %f, %v", tc.raw, rss, cpu, err)
		}
	}
	for _, raw := range []string{"", "x", "-1", "18446744073709551615", "1 2"} {
		if _, _, err := parseDarwinUsage(raw, false); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"", "12", "1:NaN", "1:Inf", "1:-2", "1:60", "1:60:00", "x:01", "1-2:03", "0-2:03"} {
		if _, err := parseDarwinCPUTime(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestDarwinDescendants(t *testing.T) {
	raw := " 1 0 S\n 10 1 S\n 12 10 S+\n 13 12 R\n 14 10 Z\n 15 1 S\n 10 13 S\n"
	if got, err := parseDarwinDescendants(raw, 10); got != 2 || err != nil {
		t.Fatalf("descendants = %d, %v", got, err)
	}
	if got, err := parseDarwinDescendants(raw, 10, 12); got != 0 || err != nil {
		t.Fatalf("excluded sampler: %d, %v", got, err)
	}
	for _, raw := range []string{"1 2", "a 1 S", "1 -1 S"} {
		if _, err := parseDarwinDescendants(raw, 10); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestDarwinSamplerSelfAndChild(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	for _, pid := range []int{0, cmd.Process.Pid} {
		sample, err := (DarwinResourceSampler{}).Sample(pid)
		if err != nil {
			t.Fatal(err)
		}
		if sample.RSSBytes == 0 || sample.CPUSeconds < 0 || sample.OOMKills != 0 || time.Since(sample.At) > time.Minute {
			t.Fatalf("invalid sample: %+v", sample)
		}
		if pid == 0 && sample.Processes < 1 {
			t.Fatal("missing child in descendant count")
		}
	}
	if _, err := (DarwinResourceSampler{}).Sample(1 << 30); err == nil {
		t.Fatal("sampled missing pid")
	}
	if ram, err := PhysicalMemoryBytes(); err != nil || ram == 0 {
		t.Fatalf("RAM = %d, %v", ram, err)
	}
}

func TestDarwinSamplePIDAndUnits(t *testing.T) {
	// The daemon and a sibling exceed the PDF worker limit; neither belongs
	// in the selected worker's RSS. The observer is a daemon child too.
	raw := "1 0 S 1024 0:00.00\n10 1 S 2097152 1:00.00\n20 10 S 65536 0:02.50\n21 10 S 3145728 0:03.00\n22 20 S 128 0:00.00\n23 20 Z 0 0:00.00\n30 10 R 4096 0:00.01\n"
	for _, tc := range []struct {
		pid   int
		rss   uint64
		cpu   float64
		count int
	}{
		{20, 64 << 20, 2.5, 1}, {10, 2 << 30, 60, 3},
	} {
		rss, cpu, count, err := parseDarwinSample(raw, tc.pid, 30)
		if err != nil || rss != tc.rss || cpu != tc.cpu || count != tc.count {
			t.Fatalf("pid %d: rss=%d cpu=%f count=%d: %v", tc.pid, rss, cpu, count, err)
		}
	}
	for _, raw := range []string{"", "20 1 S 100", "bad 1 S 100 0:00", "20 1 S -1 0:00", "20 1 S 1 0:00\n20 1 S 1 0:00"} {
		if _, _, _, err := parseDarwinSample(raw, 20, 30); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
