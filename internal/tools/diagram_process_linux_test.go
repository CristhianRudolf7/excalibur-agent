//go:build linux

package tools

import (
	"context"
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
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		// An orphan may briefly remain as a zombie until the host init reaps it.
		if strings.Contains(string(stat), ") Z ") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("renderer child %d is still running", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
