//go:build darwin

package cli

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestDarwinChildProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	cmd.SysProcAttr = newChildAttr()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid != cmd.Process.Pid {
		t.Fatalf("child pgid=%d: %v", pgid, err)
	}
}

func TestDarwinCLIBackends(t *testing.T) {
	if _, ok := newResourceSampler().(platform.DarwinResourceSampler); !ok {
		t.Fatal("incorrect sampler")
	}
	got, err := browserDataDir()
	want, wantErr := (platform.DarwinPaths{}).DataDir()
	if got != want || (err == nil) != (wantErr == nil) {
		t.Fatalf("data root=%q, %v; want %q, %v", got, err, want, wantErr)
	}
}
