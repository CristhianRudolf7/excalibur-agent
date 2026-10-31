//go:build linux

package platform

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLinuxPTYEcho(t *testing.T) {
	sess, err := (LinuxPTY{}).Start(exec.Command("echo", "hello-pty"), Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.Resize(Winsize{Rows: 40, Cols: 100}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	buf := make([]byte, 256)
	var out strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, rerr := sess.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if strings.Contains(out.String(), "hello-pty") {
			break
		}
		if rerr != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "hello-pty") {
		t.Fatalf("PTY output = %q, want hello-pty", out.String())
	}
	if err := sess.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close is idempotent.
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestLinuxPTYCat(t *testing.T) {
	sess, err := (LinuxPTY{}).Start(exec.Command("cat"), Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sess.Close() }()
	line := "ping-through-pty\n"
	if _, err := sess.Write([]byte(line)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 256)
	deadline := time.Now().Add(5 * time.Second)
	var out strings.Builder
	for time.Now().Before(deadline) {
		n, rerr := sess.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if strings.Contains(out.String(), "ping-through-pty") {
			break
		}
		if rerr != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "ping-through-pty") {
		t.Fatalf("PTY echo = %q", out.String())
	}
}

func TestLinuxPTYNilCmd(t *testing.T) {
	if _, err := (LinuxPTY{}).Start(nil, Winsize{}); err == nil {
		t.Fatal("Start(nil) must fail")
	}
}
