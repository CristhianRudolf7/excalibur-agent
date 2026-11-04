//go:build darwin

package platform

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinPriorityNice(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	before, err := EffectiveNice(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	p := DarwinProcessPriority{}
	if err := p.LowerProcess(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	after, err := EffectiveNice(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if want := p.targetNice(before); after < want {
		t.Fatalf("nice %d, want >= %d", after, want)
	}
	if p.Delegated(cmd.Process.Pid) {
		t.Fatal("macOS has no delegated cgroups")
	}
	if !errors.Is(p.SetMemoryMax(cmd.Process.Pid, 1024), errors.ErrUnsupported) {
		t.Fatal("memory cap must be unsupported")
	}
	if p.LowerProcess(0) == nil || p.LowerProcess(-1) == nil {
		t.Fatal("invalid pid accepted")
	}
}

func TestDarwinPriorityClamp(t *testing.T) {
	for _, tc := range []struct{ cur, nice, want int }{{0, 0, 10}, {15, 10, 15}, {0, -10, 0}, {-20, 10, 10}, {5, 15, 15}, {19, 15, 19}, {0, int(^uint(0) >> 1), 20}} {
		if got := (DarwinProcessPriority{Nice: tc.nice}).targetNice(tc.cur); got != tc.want {
			t.Fatalf("%+v: %d", tc, got)
		}
	}
}

func TestDarwinPriorityAbsoluteAndIdempotent(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	before, err := EffectiveNice(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	want := max(before, 15)
	for range 2 {
		if err := (DarwinProcessPriority{Nice: 15}).LowerProcess(cmd.Process.Pid); err != nil {
			t.Fatal(err)
		}
		raw, err := exec.Command("/bin/ps", "-o", "nice=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
		if err != nil {
			t.Fatal(err)
		}
		got, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || got != want {
			t.Fatalf("nice=%q, want %d: %v", raw, want, err)
		}
		info, err := unix.SysctlKinfoProc("kern.proc.pid", cmd.Process.Pid)
		if err != nil || int(info.Proc.P_nice) != want {
			t.Fatalf("kernel nice=%+v, want %d: %v", info, want, err)
		}
	}
}

func TestDarwinPriorityThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { _ = unix.Setpriority(prioDarwinThread, 0, 0) }()
	if err := (DarwinProcessPriority{}).LowerCurrentThread(); err != nil {
		t.Fatal(err)
	}
	bg, err := unix.Getpriority(prioDarwinThread, 0)
	if err != nil || bg != 1 {
		t.Fatalf("thread background = %d, %v", bg, err)
	}
}
