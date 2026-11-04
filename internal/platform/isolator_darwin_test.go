//go:build darwin

package platform

import (
	"os/exec"
	"testing"
)

func TestDarwinIsolatorPassThrough(t *testing.T) {
	cmd := exec.Command("echo", "hi")
	before := cmd.Args
	if err := (DarwinIsolator{}).Isolate(cmd); err != nil {
		t.Fatalf("Isolate: %v", err)
	}
	if len(cmd.Args) != len(before) || cmd.Args[0] != before[0] {
		t.Fatal("Isolate must run the process as-is")
	}
	if err := (DarwinIsolator{}).Isolate(nil); err != nil {
		t.Fatalf("Isolate(nil): %v", err)
	}
}
