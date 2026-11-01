//go:build live

// Container proof for the embedded root CAs (Section 23.2, defect
// w8-bench-fixes/1): the static bench binary runs in a FROM-scratch
// image with no CA certificates and still completes a TLS handshake
// against the real provider endpoint. The dummy key is rejected with
// HTTP 401 after the handshake, which proves TLS verification
// succeeded; without the embedded roots the same run dies with
// x509: certificate signed by unknown authority (the pilot failure).
package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dockerAvailable reports whether the docker CLI can talk to a daemon.
func dockerAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "info")
	cmd.Env = os.Environ()
	return cmd.Run() == nil
}

func TestBenchBinaryCarriesRootCAs(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 for the provider TLS probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if !dockerAvailable(ctx) {
		t.Skip("docker unavailable; cannot run the CA-less container proof")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "excalibur")
	build := exec.CommandContext(ctx, "nice", "-n", "19", "go", "build", "-p", "2", "-o", bin, ".")
	build.Dir = "."
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("static bench build: %v: %s", err, out)
	}
	dockerfile := "FROM scratch\nCOPY excalibur /excalibur\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatal(err)
	}
	image := "excalibur-caless-test"
	buildImg := exec.CommandContext(ctx, "docker", "build", "-t", image, dir)
	if out, err := buildImg.CombinedOutput(); err != nil {
		t.Fatalf("docker build: %v: %s", err, out)
	}
	defer func() {
		rmi := exec.Command("docker", "rmi", "-f", image)
		rmi.Env = os.Environ()
		_ = rmi.Run()
	}()
	runCtx, stop := context.WithTimeout(ctx, 3*time.Minute)
	defer stop()
	run := exec.CommandContext(runCtx, "docker", "run", "--rm",
		"-e", "EXCALIBUR_PROVIDER=openai",
		"-e", "EXCALIBUR_MODEL=gpt-6-luna",
		"-e", "EXCALIBUR_API_KEY=caless-probe-key",
		image, "/excalibur", "exec", "--profile", "eval", "--json",
		"--logs-dir", "/tmp/logs", "--timeout", "120", "say hi")
	out, err := run.CombinedOutput()
	text := strings.ToLower(string(out))
	t.Logf("container exit err=%v output:\n%s", err, out)
	if strings.Contains(text, "x509") ||
		strings.Contains(text, "certificate") ||
		strings.Contains(text, "unknown authority") {
		t.Fatalf("TLS verification failed in the CA-less image (embedded roots missing?):\n%s", out)
	}
	for _, offline := range []string{
		"no such host", "connection refused", "network is unreachable",
		"temporary failure", "i/o timeout", "context deadline exceeded",
	} {
		if strings.Contains(text, offline) {
			t.Skipf("container has no route to the provider endpoint (%s); unrelated to root CAs", offline)
		}
	}
	if !strings.Contains(text, "401") && !strings.Contains(text, "auth") &&
		!strings.Contains(text, "unauthorized") {
		t.Fatalf("expected a post-handshake 401/auth rejection with the dummy key, got:\n%s", out)
	}
}
