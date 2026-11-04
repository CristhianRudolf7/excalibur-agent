package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsSamplerSelf(t *testing.T) {
	s, err := (WindowsResourceSampler{}).Sample(0)
	if err != nil {
		t.Fatal(err)
	}
	if s.RSSBytes == 0 || s.CPUSeconds < 0 || s.Processes < 1 || s.OOMKills != 0 || s.At.IsZero() || time.Since(s.At) > time.Minute {
		t.Fatalf("bad sample: %+v", s)
	}
	if _, err := (WindowsResourceSampler{}).Sample(1 << 30); err == nil {
		t.Fatal("missing pid accepted")
	}
}

func TestWindowsSamplerProcessTree(t *testing.T) {
	dir := t.TempDir()
	cmd := processTestCommand(dir, "root")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer (WindowsProcessGroup{}).Kill(cmd.Process.Pid)
	if err := AttachJob(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "start"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "leaf")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("grandchild did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sample, err := (WindowsResourceSampler{}).Sample(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Processes != 3 || sample.RSSBytes == 0 {
		t.Fatalf("tree sample: %+v", sample)
	}
}
