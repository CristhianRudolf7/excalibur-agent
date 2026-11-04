package robot

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsRobotPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	if got, want := defaultRobotPython(), filepath.Join(dir, "excalibur", "robot-venv", "Scripts", "python.exe"); got != want {
		t.Fatalf("%q != %q", got, want)
	}
	if got, want := defaultRobotScript(), filepath.Join(dir, "excalibur", "robot-bridge", "server.py"); got != want {
		t.Fatalf("%q != %q", got, want)
	}
}
func TestWindowsRobotHelper(t *testing.T) {
	if os.Getenv("EXCALIBUR_ROBOT_PROCESS_HELPER") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}
func TestWindowsRobotProcessStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsRobotHelper$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_ROBOT_PROCESS_HELPER=1")
	configureProcess(cmd)
	defer cleanupRobotProcess(cmd)
	if err := startRobotProcess(cmd); err != nil {
		t.Fatal(err)
	}
	if err := stopProcess(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("stopped process succeeded")
	}
	cleanupRobotProcess(cmd)
	if _, ok := robotJobs.Load(cmd); ok {
		t.Fatal("job retained")
	}
}

// HTTPServer normally resolves its address before listening. A stalled Windows
// resolver must not prevent the numeric-loopback bridge from becoming healthy.
func TestWindowsRobotSupervisorStartsWithoutDNS(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	script, err := filepath.Abs("../../extensions/robot-ur/bridge/server.py")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "stalled_dns.py")
	source := `import os, runpy, socket, sys, time
def stalled_dns(*args):
    time.sleep(30)
    raise RuntimeError("unexpected DNS lookup")
socket.getfqdn = stalled_dns
script = sys.argv[1]
sys.path.insert(0, os.path.dirname(script))
sys.argv = sys.argv[1:]
runpy.run_path(script, run_name="__main__")
`
	if err := os.WriteFile(wrapper, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		URL: fmt.Sprintf("http://127.0.0.1:%d", port), Python: python, Script: script,
		PollInterval: time.Hour,
		Command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, name, append([]string{wrapper}, args...)...)
			cmd.Env = append(os.Environ(), fmt.Sprintf("EXCALIBUR_ROBOT_BRIDGE_PORT=%d", port), "PYTHONDONTWRITEBYTECODE=1")
			return cmd
		},
	})
	s.Start(context.Background())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	if err := s.ensure(ctx); err != nil {
		t.Fatalf("bridge health after %s: %v", time.Since(started), err)
	}
	t.Logf("bridge healthy without DNS after %s", time.Since(started))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
