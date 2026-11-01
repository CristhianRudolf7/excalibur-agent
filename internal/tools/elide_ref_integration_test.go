package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// The elision marker printed by exec must name a ref and offset that
// get_tool_output accepts, so the cut middle is always recoverable.
func TestExecElisionMarkerIsAcceptedByGetToolOutput(t *testing.T) {
	outs := NewMemoryOutputStore()
	sess := NewSessionManager(TerminalDeps{})
	ex := &ExecTool{Sessions: sess, Outputs: outs}
	args, _ := json.Marshal(map[string]any{"command": "seq 1 20000", "workdir": t.TempDir()})
	res, err := ex.Invoke(context.Background(), Invocation{Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`get_tool_output ref=(\S+) offset=(\d+)\]`).FindStringSubmatch(res.Output)
	if m == nil {
		t.Fatalf("no elision marker with a ref: %q", res.Output[:min(len(res.Output), 400)])
	}
	get := &GetToolOutputTool{Sessions: sess, Outputs: outs}
	gargs, _ := json.Marshal(map[string]any{"ref": m[1], "offset": json.Number(m[2])})
	got, err := get.Invoke(context.Background(), Invocation{Arguments: gargs})
	if err != nil || got.Status != StatusOK || !strings.Contains(got.Output, "bytes ") {
		t.Fatalf("get_tool_output rejected the printed ref %s: %v %v %q", m[1], err, got.Status, got.Output[:min(len(got.Output), 300)])
	}
}
