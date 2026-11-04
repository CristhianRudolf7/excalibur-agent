//go:build darwin

package tools

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDiagramStopsOrphanedRendererChildren(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	dot := diagramFake(t, "dot", `sleep 30 & echo $! > '`+pidPath+`'; exit 0`)
	b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": dot})}
	start := time.Now()
	if _, err := b.Render(context.Background(), "digraph {a->b}", "graphviz", "svg"); err == nil {
		t.Fatal("orphaned output pipe was accepted")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("inherited output pipes blocked cancellation")
	}
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Kill() })
	deadline := time.Now().Add(time.Second)
	for {
		info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err == unix.EIO || err == unix.ESRCH || err == unix.ENOENT {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		// launchd may not have reaped the orphan yet (SZOMB in sys/proc.h).
		if info.Proc.P_stat == 5 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("renderer child %d is still running", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
