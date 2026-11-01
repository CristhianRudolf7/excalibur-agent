package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Opt-in infrastructure check; ordinary tests never install packages.
func TestProvisionMinimalDebian(t *testing.T) {
	if os.Getenv("EXCALIBUR_PROVISION_CONTAINER") != "1" {
		t.Skip("set EXCALIBUR_PROVISION_CONTAINER=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", "debian:bookworm-slim", "sleep", "300").Output()
	if err != nil {
		t.Fatalf("container: %v %s", err, raw)
	}
	id := strings.TrimSpace(string(raw))
	defer exec.Command("docker", "rm", "-f", id).Run()
	run := func(ctx context.Context, cmd, dir string) (string, int) {
		args := []string{"exec"}
		if dir != "" {
			args = append(args, "-w", dir)
		}
		args = append(args, id, "sh", "-c", cmd)
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			return string(out), 1
		}
		return string(out), 0
	}
	out, code := run(ctx, "python3 -c 'print(42)'", "")
	if code == 0 {
		t.Fatal("container unexpectedly contains python3")
	}
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Run: run}
	report := provisionText(ctx, p, "python3 -c 'print(42)'", out, "")
	t.Log(report)
	if !strings.Contains(report, "verified working command") {
		t.Fatalf("diagnostic=%q report=%s", out, report)
	}
	out, code = run(ctx, "python3 -c 'print(42)'", "")
	if code != 0 || strings.TrimSpace(out) != "42" {
		t.Fatalf("%d: %s", code, out)
	}
}
