package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boundaryHarness is the harness log location used by the boundary tests.
var boundaryHarness = []string{"/logs/agent"}

func TestBoundaryRefusesEscapesInExecScope(t *testing.T) {
	requirePOSIXFeature(t, "Linux container boundary paths")
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: boundaryHarness})
	defer restore()
	refused := []string{
		"cat /proc/1/root/etc/passwd",
		"ls /proc/*/root",
		`cat "/proc/2/root/etc/hosts"`,
		"cat /run/containerd/io.containerd.snapshotter.v1/x",
		"ls /var/lib/containerd",
		"du -sh /var/lib/docker",
		"nsenter -t 1 -m -u -i -n -p sh",
		"chroot / sh",
		"chroot /proc/1/root sh",
		"mount --bind / /mnt",
		"mount -o bind /var/lib/docker /mnt",
		"cat /logs/agent/trajectory.json",
		"cat /proc/self/root/logs/agent/trajectory.json",
		"cd /logs/agent && ls",
		"cat < /logs/agent/excalibur/tool_calls.jsonl",
		"cat /tmp/../proc/1/root/x",
	}
	for _, cmd := range refused {
		if err := CheckCommandBoundary(cmd); err == nil {
			t.Errorf("allowed %q", cmd)
		} else if err.Error() != BoundaryRefusal {
			t.Errorf("%q: message %q", cmd, err)
		}
	}
	allowed := []string{
		"cat /app/main.c",
		"gcc -o out main.c && ./out",
		"ls /proc/self/root/app",
		"cat /proc/self/status",
		"chroot /tmp/rootfs sh",
		"mount --bind /srv/a /srv/b",
		"echo /logs",
		"ls /",
		"cat /logs/agentless.txt",
	}
	for _, cmd := range allowed {
		if err := CheckCommandBoundary(cmd); err != nil {
			t.Errorf("refused %q: %v", cmd, err)
		}
	}
}

func TestBoundaryInactiveOutsideExecScopeAndSwitchOff(t *testing.T) {
	const escape = "cat /proc/1/root/etc/passwd"
	if err := CheckCommandBoundary(escape); err != nil {
		t.Fatalf("refused outside exec scope: %v", err)
	}
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: boundaryHarness})
	defer restore()
	t.Setenv(TaskBoundarySwitch, "0")
	if err := CheckCommandBoundary(escape); err != nil {
		t.Fatalf("refused with switch off: %v", err)
	}
	if err := CheckPathBoundary("/logs/agent/x"); err != nil {
		t.Fatalf("read refused with switch off: %v", err)
	}
}

func TestExecScopeRestoresPrevious(t *testing.T) {
	outer := SetExecScope(ExecScope{Active: true, HarnessPaths: []string{"/a"}})
	inner := SetExecScope(ExecScope{Active: true, HarnessPaths: []string{"/b"}})
	if got := CurrentExecScope().HarnessPaths; len(got) != 1 || got[0] != "/b" {
		t.Fatalf("inner scope %v", got)
	}
	inner()
	if got := CurrentExecScope().HarnessPaths; len(got) != 1 || got[0] != "/a" {
		t.Fatalf("restored scope %v", got)
	}
	outer()
	if CurrentExecScope().Active {
		t.Fatal("scope leaked after restore")
	}
}

func TestBoundaryReadsRefusedAndSymlinkResolved(t *testing.T) {
	logs := t.TempDir()
	secret := filepath.Join(logs, "trajectory.json")
	if err := os.WriteFile(secret, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: []string{logs}})
	defer restore()
	if err := CheckPathBoundary(secret); err == nil {
		t.Fatal("harness log readable")
	}
	// A link inside the task that points at the harness logs is still refused.
	link := filepath.Join(t.TempDir(), "notes.json")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckPathBoundary(link); err == nil {
		t.Fatal("symlink to harness log readable")
	}
	if err := CheckPathBoundary(filepath.Join(t.TempDir(), "task.txt")); err != nil {
		t.Fatalf("task path refused: %v", err)
	}
}

func TestReadFileToolRefusesHarnessLog(t *testing.T) {
	logs := t.TempDir()
	secret := filepath.Join(logs, "metrics.json")
	if err := os.WriteFile(secret, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: []string{logs}})
	defer restore()
	args, _ := json.Marshal(map[string]string{"path": secret})
	res, err := (&ReadFileTool{}).Invoke(context.Background(), Invocation{Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusDenied || res.Output != BoundaryRefusal {
		t.Fatalf("read result %+v", res)
	}
}

func TestExecToolRefusesEscapeBeforeRunning(t *testing.T) {
	requirePOSIXFeature(t, "Linux container boundary paths")
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: boundaryHarness})
	defer restore()
	m := NewSessionManager(TerminalDeps{})
	tool := &ExecTool{Sessions: m}
	args, _ := json.Marshal(map[string]string{"command": "cat /proc/1/root/etc/passwd"})
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, BoundaryRefusal) || strings.Contains(res.Output, "root:") {
		t.Fatalf("exec output: %s", res.Output)
	}
}

// A harness root spelled through a symlinked directory still protects the
// files the candidate path resolves to (macOS TMPDIR under /var -> /private/var).
func TestBoundaryHarnessRootThroughSymlink(t *testing.T) {
	real := t.TempDir()
	secret := filepath.Join(real, "trajectory.json")
	if err := os.WriteFile(secret, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "logs")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip(err)
	}
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: []string{alias}})
	defer restore()
	link := filepath.Join(t.TempDir(), "notes.json")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckPathBoundary(link); err == nil {
		t.Fatal("symlink to harness log reached through an aliased root readable")
	}
	if err := CheckPathBoundary(secret); err == nil {
		t.Fatal("resolved harness log readable")
	}
	if err := CheckCommandBoundary("cat " + secret); err == nil {
		t.Fatal("command naming the resolved harness log allowed")
	}
}

func TestBoundaryHarnessRootWithMultipleAliasesAndFuturePaths(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.Symlink(real, other); err != nil {
		t.Fatal(err)
	}
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: []string{filepath.Join(alias, "future", "logs")}})
	defer restore()
	for _, root := range []string{real, alias, other} {
		secret := filepath.Join(root, "future", "logs", "trajectory.json")
		if err := CheckCommandBoundary("cat '" + secret + "'"); err == nil {
			t.Errorf("command naming aliased future harness log allowed: %q", secret)
		}
		if err := CheckPathBoundary(secret); err == nil {
			t.Errorf("aliased future harness log readable: %q", secret)
		}
		if err := CheckCommandBoundary("cat " + filepath.Join(root, "future", "logs-sibling", "report.json")); err != nil {
			t.Errorf("sibling prefix refused: %v", err)
		}
	}
}
