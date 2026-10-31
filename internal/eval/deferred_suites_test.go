package eval

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func deferredCatalogForEval() *tools.DeferredCatalog {
	reg := tools.NewMapRegistry()
	_ = tools.RegisterBuiltins(reg, nil)
	cat := tools.NewDeferredCatalog(reg)
	exec := tools.NewExecutor(reg)
	tools.WireSearchCall(reg, cat, exec)
	tools.WireWebTools(reg, cat, &tools.WebDeps{Backend: tools.NewMemorySearchBackend(), Sources: &tools.MemorySourceRecorder{}})
	if err := tools.RegisterDeferredMedia(reg, cat, &tools.MediaDeps{}); err != nil {
		panic(err)
	}
	mgr := tools.NewPackageManager(reg, cat, exec)
	tools.WirePackageTool(reg, cat, mgr)
	return cat
}

func TestToolRetrievalScaffold(t *testing.T) {
	dir := t.TempDir()
	hash, err := PrepareToolRetrieval(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("empty hash")
	}
	again, err := PrepareToolRetrieval(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash != again {
		t.Fatal("prepare not deterministic")
	}
	queries, err := LoadToolRetrievalQueries(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) == 0 {
		t.Fatal("no queries")
	}
	cat := deferredCatalogForEval()
	rep := RunToolRetrievalOn(cat, queries)
	if rep.Queries != len(queries) {
		t.Fatalf("queries: %+v", rep)
	}
	// The harness corpus is written to be found: every fixture target
	// resolves in the top 5.
	if rep.PrecisionAt5 != 1 {
		t.Fatalf("precision@5 should be 1 on the pinned corpus: %+v", rep)
	}
	if rep.PrecisionAt1 < 0.5 {
		t.Fatalf("precision@1 too low: %+v", rep)
	}
	if rep.MRR <= 0 {
		t.Fatalf("mrr: %+v", rep)
	}
	// Fixture hash pins the setup (Section 23.8).
	if ToolRetrievalHash(queries) != hash {
		t.Fatal("hash mismatch")
	}
}

func TestTerminalVsToolsScaffold(t *testing.T) {
	dir := t.TempDir()
	hash, err := PrepareTerminalTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("empty hash")
	}
	if TerminalTasksHash(DefaultTerminalTasks()) != hash {
		t.Fatal("hash mismatch")
	}
	trials := []TerminalTrial{
		{Task: "find-todo", Arm: ArmTerminalOnly, Success: true, Tokens: 4000, Millis: 9000, Errors: 2, ToolCalls: 12},
		{Task: "find-todo", Arm: ArmTerminalTools, Success: true, Tokens: 1200, Millis: 3000, Errors: 0, ToolCalls: 3},
		{Task: "convert-doc", Arm: ArmTerminalOnly, Success: true, Tokens: 5000, Millis: 8000, Errors: 1, ToolCalls: 10},
		{Task: "convert-doc", Arm: ArmTerminalTools, Success: true, Tokens: 1500, Millis: 2500, Errors: 0, ToolCalls: 2},
	}
	rep := SimulateTerminalComparison(trials)
	if len(rep.Tasks) != 2 {
		t.Fatalf("tasks: %+v", rep)
	}
	if rep.SuccessOnly != 2 || rep.SuccessTools != 2 {
		t.Fatalf("success counts: %+v", rep)
	}
	if rep.AvgTokenDelta >= 0 {
		t.Fatalf("tools arm should use fewer tokens: %+v", rep)
	}
	if rep.AvgTimeDelta >= 0 {
		t.Fatalf("tools arm should be faster: %+v", rep)
	}
	// Registry entries carry fixture names for the deterministic check.
	toolSuite, err := ByID(SuiteToolRetrieval)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolSuite.Fixtures) == 0 {
		t.Fatal("tool-retrieval needs fixtures")
	}
	termSuite, err := ByID(SuiteTerminalTools)
	if err != nil {
		t.Fatal(err)
	}
	if len(termSuite.Fixtures) == 0 {
		t.Fatal("terminal-vs-tools needs fixtures")
	}
}
