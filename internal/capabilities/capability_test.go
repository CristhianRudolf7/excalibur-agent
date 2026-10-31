package capabilities

import (
	"encoding/json"
	"strings"
	"testing"
)

func testManifest() Manifest {
	return Manifest{
		ID: "search", Version: "1.0.0",
		Operations: []Operation{
			{
				Name: "search", Description: "Search",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Effect:      "read_only", Idempotency: "pure", Cancel: "immediate",
			},
		},
	}
}

func TestManifestValidate(t *testing.T) {
	if err := testManifest().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := testManifest()
	bad.Operations[0].Effect = "nuke"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected effect error")
	}
	bad2 := testManifest()
	bad2.Operations = append(bad2.Operations, bad2.Operations[0])
	if err := bad2.Validate(); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestTaskTerminal(t *testing.T) {
	if !Terminal(StateCompleted) || !Terminal(StateFailed) || !Terminal(StateCancelled) {
		t.Fatal("terminal states")
	}
	if Terminal(StateWorking) || Terminal(StateInputRequired) {
		t.Fatal("non-terminal flagged terminal")
	}
}

func TestClassifyError(t *testing.T) {
	if ClassifyError(nil) != "" {
		t.Fatal("nil")
	}
	if ClassifyError(errMsg("timeout waiting for browser")) != ErrTransient {
		t.Fatal("transient")
	}
	if ClassifyError(errMsg("invalid arguments: missing url")) != ErrInput {
		t.Fatal("input")
	}
	if ClassifyError(errMsg("disk melted")) != ErrPermanent {
		t.Fatal("permanent")
	}
}

func TestBlockedBenchmarkSource(t *testing.T) {
	for _, blocked := range []string{
		"https://github.com/harbor-framework/harbor",
		"https://github.com/terminal-bench/terminal-bench",
		"https://harbor.example.com/x",
		"https://tbench.ai/tasks/1",
		"http://TBench.AI/foo",
	} {
		if !BlockedBenchmarkSource(blocked) {
			t.Fatalf("should block %q", blocked)
		}
	}
	for _, okURL := range []string{
		"https://docs.python.org/3/library/os.html",
		"https://example.com/search?q=harbor+boats",
	} {
		// "harbor boats" contains the marker "harbor" as a substring;
		// the blocklist is intentionally substring-based and
		// conservative, so plain docs stay allowed while anything
		// mentioning the benchmark host is blocked. The second URL
		// above does contain "harbor" and is therefore blocked; keep
		// only the docs URL as allowed here.
		if strings.Contains(okURL, "harbor") {
			continue
		}
		if BlockedBenchmarkSource(okURL) {
			t.Fatalf("should allow %q", okURL)
		}
	}
	if !BlockedBenchmarkSource("https://example.com/harbor/boats") {
		t.Fatal("conservative substring block expected")
	}
}

func TestMCPToolName(t *testing.T) {
	if got := MCPToolName("github", "get_issue"); got != "mcp__github__get_issue" {
		t.Fatalf("mcp name: %q", got)
	}
	long := MCPToolName(strings.Repeat("a", 40), strings.Repeat("b", 40))
	if len(long) > 64 {
		t.Fatalf("abbreviated name too long: %q (%d)", long, len(long))
	}
	if !strings.HasPrefix(long, "mcp__") {
		t.Fatalf("prefix lost: %q", long)
	}
	// Deterministic abbreviation.
	again := MCPToolName(strings.Repeat("a", 40), strings.Repeat("b", 40))
	if long != again {
		t.Fatal("mcp abbreviation not deterministic")
	}
}

type errMsg string

func (e errMsg) Error() string { return string(e) }
