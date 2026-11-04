//go:build darwin

package automations

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDarwinScriptGroupCleanup(t *testing.T) {
	for _, mode := range []string{"cancel", "cleanup", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "grandchild.pid")
			script := `/bin/sh -c 'sleep 30 & echo $! > "$PID_FILE"; wait' & wait`
			if mode == "orphan" {
				script = `sleep 30 & echo $! > "$PID_FILE"; exit 0`
			}
			cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", script)
			cmd.Env = append(os.Environ(), "PID_FILE="+path)
			if err := configureScriptProcess(cmd); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Cancel(); !errors.Is(err, os.ErrProcessDone) {
				t.Fatalf("cancel before start: %v", err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cleanupScriptProcess(cmd); _ = cmd.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			var pid int
			for time.Now().Before(deadline) {
				raw, _ := os.ReadFile(path)
				pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
				if pid > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid <= 0 {
				t.Fatal("grandchild never started")
			}
			defer unix.Kill(pid, unix.SIGKILL)
			info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
			if err != nil || int(info.Eproc.Pgid) != cmd.Process.Pid {
				t.Fatalf("grandchild outside script group: %+v %v", info, err)
			}
			if mode == "orphan" {
				if err := cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cancel" {
				if err := cmd.Cancel(); err != nil {
					t.Fatal(err)
				}
			} else {
				cleanupScriptProcess(cmd)
			}
			for time.Now().Before(deadline) {
				p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
				if err == unix.EIO || err == unix.ESRCH || err == unix.ENOENT || (err == nil && p.Proc.P_stat == 5) {
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("script descendant %d survived %s", pid, mode)
		})
	}
}
