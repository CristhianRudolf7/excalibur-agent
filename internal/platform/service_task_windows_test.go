package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Exercise registration against the real scheduler, including escaped Unicode
// paths. The short-lived command is harmless when Install starts the task.
func TestWindowsTaskInstallUninstall(t *testing.T) {
	if _, err := exec.LookPath("schtasks.exe"); err != nil {
		t.Skip("Task Scheduler unavailable")
	}
	dir := filepath.Join(t.TempDir(), "A & B café")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "excalibur.exe")
	source, err := exec.LookPath("whoami.exe")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, raw, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := WindowsServiceManager{ExecPath: executable, TaskName: fmt.Sprintf("Excalibur Test %d %d", os.Getpid(), time.Now().UnixNano())}
	t.Cleanup(func() {
		if err := manager.Uninstall(); err != nil {
			t.Error(err)
		}
	})
	if err := manager.Install(); err != nil {
		t.Fatal(err)
	}
	if status, err := manager.Status(); err != nil || status == "absent" {
		t.Fatalf("installed status %q: %v", status, err)
	}
	if err := manager.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if status, err := manager.Status(); err != nil || status != "absent" {
		t.Fatalf("uninstalled status %q: %v", status, err)
	}
}
