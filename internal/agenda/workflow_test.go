package agenda

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// 21.4/21.10: versioned graphs, conditions, durable steps, dry run.

func linearDef(nodes ...Node) *WorkflowDef {
	d := &WorkflowDef{ID: "w", Name: "w", Version: 1, Nodes: nodes}
	for i := 0; i+1 < len(nodes); i++ {
		d.Edges = append(d.Edges, Edge{From: nodes[i].ID, To: nodes[i+1].ID})
	}
	return d
}

type scriptExecutor struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]error
}

func (s *scriptExecutor) ExecuteNode(_ context.Context, nc NodeContext) (NodeOutput, error) {
	s.mu.Lock()
	s.calls = append(s.calls, nc.Node.ID+":"+nc.IdempotencyKey)
	s.mu.Unlock()
	if err, ok := s.fail[nc.Node.ID]; ok {
		return NodeOutput{}, err
	}
	return NodeOutput{Data: map[string]any{"node": nc.Node.ID, "attempt": nc.Attempt}}, nil
}

func instantSleep(_ time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

func TestValidateGraph(t *testing.T) {
	ok := linearDef(
		Node{ID: "a", Type: NodeTool, Tool: "t"},
		Node{ID: "b", Type: NodeAgent, AgentBrief: "brief"},
	)
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	dangling := linearDef(Node{ID: "a", Type: NodeTool, Tool: "t"})
	dangling.Edges = append(dangling.Edges, Edge{From: "a", To: "ghost"})
	if err := dangling.Validate(); err == nil {
		t.Fatal("edge to unknown node must fail")
	}
	loop := &WorkflowDef{ID: "l", Version: 1,
		Nodes: []Node{{ID: "lp", Type: NodeLoop, Body: []string{"w"}, MaxIterations: 0}, {ID: "w", Type: NodeTool, Tool: "t"}},
		Edges: []Edge{{From: "lp", To: "w"}}}
	if err := loop.Validate(); err == nil {
		t.Fatal("unbounded loop must fail")
	}
	if _, err := ParseCondition("a == "); err == nil {
		t.Fatal("bad condition must fail")
	}
}

func TestVersionPin(t *testing.T) {
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	d := linearDef(Node{ID: "a", Type: NodeTool, Tool: "t"})
	in, err := eng.Start(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	d.Version = 2 // publish while running
	if err := eng.Run(context.Background(), d, in); err == nil {
		t.Fatal("running instance must not adopt a new version implicitly")
	}
	if err := in.Migration(d); err != nil {
		t.Fatal(err)
	}
	eng2 := NewEngine(exec)
	in2, err := eng2.Start(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng2.Run(context.Background(), d, in2); err != nil {
		t.Fatal(err)
	}
	if in2.State != InstanceDone {
		t.Fatalf("migrated instance runs: %s", in2.State)
	}
}

func TestResumeSkipsCompleted(t *testing.T) {
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	eng.sleep = instantSleep
	d := linearDef(
		Node{ID: "a", Type: NodeTool, Tool: "t"},
		Node{ID: "b", Type: NodeTool, Tool: "t"},
	)
	in, _ := eng.Start(d, map[string]any{"x": 1})
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("want 2 calls, have %v", exec.calls)
	}
	// Re-run is a resume: no node executes again.
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("resume must not repeat completed nodes: %v", exec.calls)
	}
}

func TestDryRunSkipsUndeclaredEffects(t *testing.T) {
	var calls []string
	exec := &scriptExecutor{}
	rec := &recordingExecutor{inner: exec, calls: &calls}
	eng := NewEngine(rec)
	d := linearDef(
		Node{ID: "safe", Type: NodeTool, Tool: "read"},
		Node{ID: "risky", Type: NodeTool, Tool: "send", Effects: true},
		Node{ID: "declared", Type: NodeTool, Tool: "post", Effects: true,
			DryRun: &DryRunSpec{Validate: []string{"x"}, Simulated: map[string]any{"ok": true}}},
	)
	in, _ := eng.Start(d, DryRunScope(map[string]any{"x": 1}))
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if c == "risky" {
			t.Fatal("effect tool without dry_run must not execute in a test")
		}
	}
	found := false
	for _, r := range in.Records {
		if r.NodeID == "risky" && r.SkippedDryRun {
			found = true
		}
	}
	if !found {
		t.Fatal("skipped node must be recorded as skipped")
	}
}

type recordingExecutor struct {
	inner *scriptExecutor
	mu    sync.Mutex
	calls *[]string
}

func (r *recordingExecutor) ExecuteNode(ctx context.Context, nc NodeContext) (NodeOutput, error) {
	r.mu.Lock()
	*r.calls = append(*r.calls, nc.Node.ID)
	r.mu.Unlock()
	return r.inner.ExecuteNode(ctx, nc)
}

func TestConfirmBlocksWithoutAnswer(t *testing.T) {
	d := linearDef(
		Node{ID: "c", Type: NodeConfirm, ConfirmPrompt: "go?"},
		Node{ID: "s", Type: NodeTool, Tool: "send", Effects: true},
	)
	eng := NewEngine(&scriptExecutor{})
	in, _ := eng.Start(d, nil)
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceWaitingConfirm || in.PendingNode != "c" {
		t.Fatalf("must wait for the user: %+v", in)
	}
	// Simulate the answer arriving later: resume completes.
	in.Confirmations["c"] = true
	mock := &scriptExecutor{}
	eng2 := NewEngine(mock)
	eng2.confirmer = &mockConfirmer{confirm: true}
	if err := eng2.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceDone {
		t.Fatalf("confirmed instance completes: %+v", in)
	}
}

func TestDeclinedConfirmAborts(t *testing.T) {
	d := linearDef(
		Node{ID: "pay", Type: NodeTool, Tool: "charge", Effects: true, Compensation: "refund"},
		Node{ID: "c", Type: NodeConfirm, ConfirmPrompt: "ship?"},
		Node{ID: "s", Type: NodeTool, Tool: "send", Effects: true},
	)
	// Reorder: pay first so a compensation exists when confirm declines.
	exec := &scriptExecutor{}
	eng := NewEngine(exec).WithConfirmer(&mockConfirmer{confirm: false})
	in, _ := eng.Start(d, nil)
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceAborted {
		t.Fatalf("declined confirmation aborts: %+v", in)
	}
	// pay completed before the decline, so its compensation ran.
	found := false
	for _, r := range in.Records {
		if r.NodeID == "compensate_pay" {
			found = true
		}
	}
	if !found {
		t.Fatalf("abort must compensate completed nodes in reverse: %+v", in.Records)
	}
}

func TestAbortCompensatesReverse(t *testing.T) {
	d := linearDef(
		Node{ID: "a", Type: NodeTool, Tool: "t1", Effects: true, Compensation: "undo-a"},
		Node{ID: "b", Type: NodeTool, Tool: "t2", Effects: true, Compensation: "undo-b"},
		Node{ID: "c", Type: NodeTool, Tool: "t3"},
	)
	exec := &scriptExecutor{fail: map[string]error{"c": errors.New("invalid argument: bad input")}}
	eng := NewEngine(exec)
	eng.sleep = instantSleep
	in, _ := eng.Start(d, nil)
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceAborted {
		t.Fatalf("input failure aborts with compensations: %+v", in)
	}
	var order []string
	for _, r := range in.Records {
		if len(r.NodeID) > 11 && r.NodeID[:11] == "compensate_" {
			order = append(order, r.NodeID)
		}
	}
	if len(order) != 2 || order[0] != "compensate_b" || order[1] != "compensate_a" {
		t.Fatalf("reverse compensation order: %v", order)
	}
}

func TestConditionsRoute(t *testing.T) {
	d := &WorkflowDef{ID: "cd", Version: 1,
		Nodes: []Node{
			{ID: "q", Type: NodeCondition, Condition: "ok == true"},
			{ID: "yes", Type: NodeTool, Tool: "t"},
			{ID: "no", Type: NodeTool, Tool: "t"},
		},
		Edges: []Edge{
			{From: "q", To: "yes", When: "ok == true"},
			{From: "q", To: "no", When: "ok != true"},
		},
		Entry: "q",
	}
	for _, tc := range []struct {
		ok   bool
		want string
	}{
		{true, "yes"},
		{false, "no"},
	} {
		exec := &scriptExecutor{}
		eng := NewEngine(exec)
		in, _ := eng.Start(d, map[string]any{"ok": tc.ok})
		if err := eng.Run(context.Background(), d, in); err != nil {
			t.Fatal(err)
		}
		if len(exec.calls) != 1 {
			t.Fatalf("exactly one branch runs: %v", exec.calls)
		}
	}
}

func TestEdgeNoMatchFails(t *testing.T) {
	d := &WorkflowDef{ID: "nm", Version: 1,
		Nodes: []Node{
			{ID: "q", Type: NodeCondition, Condition: "true"},
			{ID: "yes", Type: NodeTool, Tool: "t"},
		},
		Edges: []Edge{{From: "q", To: "yes", When: "ok == true"}},
		Entry: "q",
	}
	eng := NewEngine(&scriptExecutor{})
	in, _ := eng.Start(d, map[string]any{"ok": false})
	if err := eng.Run(context.Background(), d, in); err == nil {
		t.Fatal("no matching edge must fail loudly, not silently end")
	}
}

func TestRunTestsFramework(t *testing.T) {
	d := linearDef(
		Node{ID: "a", Type: NodeTool, Tool: "t"},
		Node{ID: "b", Type: NodeCondition, Condition: "true"},
	)
	d.Tests = []WorkflowTest{
		{Name: "pass", Inputs: map[string]any{}, Mocks: map[string]any{"a": map[string]any{"v": 1}}, Expected: map[string]any{"a.v": "1"}},
		{Name: "caught", Inputs: map[string]any{}, Mocks: map[string]any{"a": errors.New("boom")}, ExpectError: true},
	}
	results := RunTests(d, nil)
	for _, r := range results {
		if !r.Passed {
			t.Fatalf("test %q failed: %s", r.Name, r.Failure)
		}
	}
}
