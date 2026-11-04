//go:build darwin

package robot

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDarwinRobotProcess(t *testing.T) {
	if err := supported(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sleep", "30")
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid != cmd.Process.Pid {
		t.Fatalf("robot pgid=%d: %v", pgid, err)
	}
	if err := stopProcess(cmd); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	if err == nil {
		t.Fatal("robot process was not killed")
	}
}

// A resolver that fails immediately makes the startup regression deterministic,
// including on machines where the real loopback lookup happens to be fast.
func TestDarwinRobotStartupWithoutDNS(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "numeric_server.py")
	source := `import http.server, socket
def no_dns(*args):
    raise RuntimeError("unexpected DNS lookup")
socket.getfqdn = no_dns
server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), http.server.BaseHTTPRequestHandler)
assert server.server_name == "127.0.0.1"
assert server.server_port > 0
server.server_close()
`
	if err := os.WriteFile(script, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	configureProcess(cmd)
	if err := startRobotProcess(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("bridge startup: %v: %s", err, out.String())
	}
}
