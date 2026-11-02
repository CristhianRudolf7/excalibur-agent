package harbor_test

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Keep the installed adapter's pure timeout and command tests in Go verification.
func TestInstalledAgentTimeout(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-m", "unittest", "excalibur_agent_test.TestRunnerTimeout")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("adapter timeout tests: %v\n%s", err, out)
	}
}
