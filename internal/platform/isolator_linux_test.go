//go:build linux

package platform

import (
	"os/exec"
	"testing"
)

func TestLinuxIsolatorPassThrough(t *testing.T) {
	cmd := exec.Command("echo", "hi")
	before := cmd.Args
	if err := (LinuxIsolator{}).Isolate(cmd); err != nil {
		t.Fatalf("Isolate: %v", err)
	}
	if len(cmd.Args) != len(before) || cmd.Args[0] != before[0] {
		t.Fatal("Isolate must run the process as-is")
	}
	if err := (LinuxIsolator{}).Isolate(nil); err != nil {
		t.Fatalf("Isolate(nil): %v", err)
	}
}
