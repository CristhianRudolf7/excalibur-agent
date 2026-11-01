//go:build linux

package tools

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// TestAncestorChainIsProtected builds a three-process chain. The grandparent
// sits in its own process group, so platform protection (the harness, its
// parent and their groups) does not cover it; only the ancestor walk does.
func TestAncestorChainIsProtected(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process groups")
	}
	switch os.Getenv("W22_TREE_ROLE") {
	case "A":
		treeChild("B", &syscall.SysProcAttr{Setpgid: true})
		os.Exit(0)
	case "B":
		treeChild("C", nil)
		os.Exit(0)
	case "C":
		ancestors := readProcessTable().ancestors(os.Getpid())
		if len(ancestors) < 2 {
			fmt.Println("CHAIN too short")
			os.Exit(3)
		}
		grandparent := ancestors[1]
		err := guardSignals(fmt.Sprintf("kill -9 %d", grandparent.PID))
		if err == nil {
			fmt.Println("ALLOWED")
			os.Exit(4)
		}
		fmt.Println("REFUSED")
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAncestorChainIsProtected$")
	cmd.Env = append(os.Environ(), "W22_TREE_ROLE=A")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chain: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "REFUSED") {
		t.Fatalf("grandparent not protected: %s", out)
	}
}

// treeChild runs the test binary in the given role and waits for it. It is
// only called from helper roles, which exit the process themselves.
func treeChild(role string, attr *syscall.SysProcAttr) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAncestorChainIsProtected$")
	cmd.Env = append(os.Environ(), "W22_TREE_ROLE="+role)
	cmd.SysProcAttr = attr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "role %s: %v\n", role, err)
		os.Exit(5)
	}
}
