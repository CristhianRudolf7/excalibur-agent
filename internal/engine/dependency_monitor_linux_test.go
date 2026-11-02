//go:build linux

package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestDynamicReadDependencyReceipt(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "dynamic.dat")
	if err := os.WriteFile(data, []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor := startDependencyMonitor(dir)
	if monitor == nil {
		t.Fatal("workspace monitor unavailable")
	}
	// The path is assembled by the process, rather than named in tool args.
	cmd := exec.Command("sh", "-c", `cat "$(printf dynamic).dat"`)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	paths, complete := monitor.finish()
	if !complete {
		t.Fatal("incomplete read monitor")
	}
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.Workdir = dir
	tc := toolCall("read", "exec", `{"command":"python3 -c 'print(data)'"}`)
	receipt := e.captureEvidenceDependencies(r, tc, tools.Result{Status: tools.StatusOK, Output: string(output)}, paths...)
	if _, ok := receipt[data]; !ok {
		t.Fatalf("dynamic read absent: %v", paths)
	}
	events := []ctxcompiler.ActivityEvent{{ToolCalls: []providers.ToolCall{tc}}, {Dependencies: receipt, ToolResult: &providers.ToolResult{CallID: "read", Name: "exec", Output: string(output)}}}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.log"), []byte("diagnostic"), 0600); err != nil {
		t.Fatal(err)
	}
	if historyTools(events)[0].DependenciesChanged {
		t.Fatal("unrelated write expired dynamic-read evidence")
	}
	if err := os.WriteFile(data, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if !historyTools(events)[0].DependenciesChanged {
		t.Fatal("changed dynamic input retained evidence")
	}
}
