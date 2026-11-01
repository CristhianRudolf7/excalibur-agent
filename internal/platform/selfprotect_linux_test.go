//go:build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestReexecRemovesInstruction(t *testing.T) {
	const marker = "w17-private-instruction-9842"
	if phase := os.Getenv("W17_REEXEC_TEST"); phase != "" {
		if phase == "first" {
			argv := append([]string(nil), os.Args...)
			argv[len(argv)-1] = "[request transferred]"
			if err := os.Setenv("W17_REEXEC_TEST", "second"); err != nil {
				os.Exit(3)
			}
			env := append(os.Environ(), "W17_ORIGINAL_PID="+strconv.Itoa(os.Getpid()))
			if err := Reexec(argv, env); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		raw, err := os.ReadFile("/proc/self/cmdline")
		if err != nil || strings.Contains(string(raw), marker) || !strings.Contains(string(raw), "[request transferred]") || os.Getenv("W17_ORIGINAL_PID") != strconv.Itoa(os.Getpid()) {
			os.Exit(2)
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReexecRemovesInstruction$", "--", marker)
	cmd.Env = append(os.Environ(), "W17_REEXEC_TEST=first")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reexec: %v %s", err, out)
	}
}

func TestProtectedSignalTargets(t *testing.T) {
	targets, err := ProtectedSignalTargets()
	if err != nil {
		t.Fatal(err)
	}
	found := map[int]bool{}
	for _, p := range targets {
		found[p.PID] = true
		if p.Group <= 0 || p.Name == "" {
			t.Fatalf("bad target: %+v", p)
		}
	}
	if !found[os.Getpid()] || !found[os.Getppid()] {
		t.Fatalf("missing self or parent: %v", found)
	}
}
