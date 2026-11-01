package tools

import (
	"strings"
	"testing"
)

func TestExecHeadLimitNote(t *testing.T) {
	for _, command := range []string{"seq 20 | head -n 3", "seq 20 | head -3", "seq 20 | head -n3", "seq 20 | head", "grep -m 3 x file", "cat file | grep -m3 x"} {
		t.Run(command, func(t *testing.T) {
			n := 3
			if strings.HasSuffix(command, "| head") {
				n = 10
			}
			output := strings.Repeat("x\n", n)
			if got := execHeadLimitNote(command, output); !strings.Contains(got, "there may be more results") {
				t.Fatal(got)
			}
			if got := execHeadLimitNote(command, strings.Repeat("x\n", n-1)); got != "" {
				t.Fatal(got)
			}
			if got := execHeadLimitNote(command, strings.TrimSuffix(output, "\n")); got == "" {
				t.Fatal("unterminated final line")
			}
		})
	}
	for _, command := range []string{"echo 'head -n 3'", "cat file | head -3 | wc -l", "seq 20 | head -c 3", "seq 20 | head -n -3", "grep x file", "echo x; seq 20 | head -3"} {
		if got := execHeadLimitNote(command, "x\nx\nx\n"); got != "" {
			t.Fatalf("%s: %s", command, got)
		}
	}
	t.Setenv("EXCALIBUR_EXEC_HEAD_LIMIT", "off")
	if got := execHeadLimitNote("seq 20 | head -3", "x\nx\nx\n"); got != "" {
		t.Fatal(got)
	}
}

func TestExecHeadLimitRendered(t *testing.T) {
	tool := execTestTool(t)
	defer closeSessions(tool.Sessions)
	res := callTool(t, tool, map[string]any{"command": "printf 'a\\nb\\nc\\n' | head -n 2"})
	if !strings.Contains(res.Output, "note: output stopped at the head limit (2 lines)") {
		t.Fatal(res.Output)
	}
}
