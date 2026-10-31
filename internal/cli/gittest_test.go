package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func execLookPath(name string) (string, error) { return exec.LookPath(name) }

func osFindProcess(pid int) (*os.Process, error) { return os.FindProcess(pid) }

func procKill(p *os.Process) error { return p.Kill() }

// testSleepScript writes a helper binary stand-in: a shell script that
// sleeps, standing in for a long background child.
func testSleepScript(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sleeper.sh"
	body := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func lookPathGit() (string, error) { return exec.LookPath("git") }

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return &codedErr{string(out), err}
	}
	return nil
}

type codedErr struct {
	out string
	err error
}

func (e *codedErr) Error() string { return e.out + ": " + e.err.Error() }

func gitClean(dir string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	// The task store (<project>/.excalibur) is harness state, not
	// delegating work: users gitignore it. Only tracked or
	// non-harness changes count as touching the delegating tree.
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		name := fields[len(fields)-1]
		if strings.HasPrefix(name, ".excalibur/") || name == ".excalibur" {
			continue
		}
		return false, nil
	}
	return true, nil
}
