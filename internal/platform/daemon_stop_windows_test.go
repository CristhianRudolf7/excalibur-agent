package platform

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestDaemonStopEventCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closeListener, err := ListenDaemonStop(ctx, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer closeListener()
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, daemonStopEventName(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(event)
	if err := windows.SetEvent(event); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("stop event did not cancel daemon context")
	}
}

// The helper child has no stop listener, exercising the termination fallback.
func TestWindowsForcedDaemonStop(t *testing.T) {
	if os.Getenv("EXCALIBUR_TEST_FORCE_STOP_CHILD") == "1" {
		time.Sleep(time.Minute)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsForcedDaemonStop$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_TEST_FORCE_STOP_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	if err := RequestDaemonStop(cmd.Process.Pid, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("explicit stop must exit successfully to avoid scheduler restart: %v", err)
	}
}
