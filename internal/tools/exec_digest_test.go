package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestExecDigestIncludesElidedSearchPath(t *testing.T) {
	full := strings.Repeat("src/common.go:12:"+strings.Repeat("x", 100)+"\n", 450) + "src/middle.go:42:unique hit\n" + strings.Repeat("src/common.go:99:"+strings.Repeat("y", 100)+"\n", 450)
	for _, clip := range []func(string) string{
		func(s string) string { out, _ := clipExecOutput(s, nil, "session"); return out },
		func(s string) string { return headTailInline(s, execInlineBudget) },
	} {
		out := clip(full)
		if len(out) > execInlineBudget {
			t.Fatalf("budget exceeded: %d", len(out))
		}
		for _, want := range []string{"901 lines", "src/middle.go: 1", "src/common.go: 900"} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %q", want)
			}
		}
	}
	t.Setenv("EXCALIBUR_EXEC_OUTPUT_DIGEST", "0")
	out, _ := clipExecOutput(full, nil, "session")
	if strings.Contains(out, "Full output digest") {
		t.Fatal("switch ignored")
	}
}

func TestSearchOutputDigestPathsAndLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 65; i++ {
		fmt.Fprintf(&b, "./dir/file%d.txt\n", i)
	}
	digest := execOutputDigest(b.String())
	if !strings.Contains(digest, "+5 more") || strings.Contains(digest, "file60.txt") {
		t.Fatal(digest)
	}
	for _, s := range []string{"dir/file.go:1:hit", "dir/file.go:hit", "./dir/file.go", "/tmp/file"} {
		if searchOutputPath(s) == "" {
			t.Fatalf("missed path %q", s)
		}
	}
}
