package ctxcompiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestCollapseRepetitiveOutput(t *testing.T) {
	var b strings.Builder
	b.WriteString("start installer\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "Downloading object: %d%% [========>] %d KB/s\n", i%101, i*3)
	}
	b.WriteString("warning: certificate expires soon\nerror: dependency unavailable\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "Installing entry %d\n", i)
	}
	b.WriteString("finished\n")
	raw := b.String()
	got := CollapseRepetitiveOutput(raw)
	if len(got) >= len(raw)/4 {
		t.Fatalf("before %d after %d", len(raw), len(got))
	}
	for _, want := range []string{"start installer", "Downloading object: 0%", "Installing entry 99", "warning: certificate expires soon", "error: dependency unavailable", "finished", "collapsed 296 repetitive lines", "collapsed 96 repetitive lines"} {
		if !strings.Contains(got, want) {
			t.Fatal("missing", want, got)
		}
	}
	if got != CollapseRepetitiveOutput(raw) {
		t.Fatal("nondeterministic")
	}
	activity := []ActivityEvent{{Role: providers.RoleTool, Content: raw, ToolResult: &providers.ToolResult{Name: "exec", Output: raw}}}
	req := providers.ChatRequest{}
	(Compiled{}).AppendActivityMessages(&req, activity)
	if req.Messages[0].ToolResult.Output != got || req.Messages[0].Content != got {
		t.Fatal("model projection did not collapse")
	}
	if activity[0].Content != raw || activity[0].ToolResult.Output != raw {
		t.Fatal("durable history changed")
	}
	t.Setenv("EXCALIBUR_COLLAPSE_PROGRESS", "off")
	if CollapseRepetitiveOutput(raw) != raw {
		t.Fatal("disabled collapse")
	}
	t.Logf("observation bytes before=%d after=%d", len(raw), len(got))
}

func TestCollapseRetainsDiagnosticRunsAndUniqueLines(t *testing.T) {
	raw := strings.Repeat("warning: disk low 20%\n", 60) + strings.Repeat("error: error 1\n", 60)
	if got := CollapseRepetitiveOutput(raw); got != raw {
		t.Fatal("diagnostics collapsed")
	}
	raw = strings.Repeat("short\n", 3)
	if CollapseRepetitiveOutput(raw) != raw {
		t.Fatal("short output changed")
	}
	raw = "begin\n" + strings.Repeat("progress 30%\r", 100) + "end\n"
	got := CollapseRepetitiveOutput(raw)
	if !strings.Contains(got, "collapsed") || !strings.HasSuffix(got, "end\n") {
		t.Fatal(got)
	}
}
