package agenda

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestParallelBranchesMerge(t *testing.T) {
	d := &WorkflowDef{ID: "par", Version: 1,
		Nodes: []Node{
			{ID: "fan", Type: NodeParallel, Branches: []string{"left", "right"}},
			{ID: "left", Type: NodeAgent, AgentBrief: "left work"},
			{ID: "right", Type: NodeAgent, AgentBrief: "right work"},
			{ID: "join", Type: NodeTool, Tool: "merge"},
		},
		Edges: []Edge{
			{From: "fan", To: "join"},
		},
		Entry: "fan",
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	// checkReachable passes (branches referenced); entry fan has no incoming.
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	in, _ := eng.Start(d, map[string]any{"in": 1})
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceDone {
		t.Fatalf("parallel run completes: %+v", in)
	}
	// join ran once, after both branches recorded.
	seen := map[string]int{}
	for _, r := range in.Records {
		seen[r.NodeID]++
	}
	if seen["left"] != 1 || seen["right"] != 1 || seen["fan"] != 1 || seen["join"] != 1 {
		t.Fatalf("each node recorded once: %v", seen)
	}
}

func TestParallelRace(t *testing.T) {
	d := &WorkflowDef{ID: "prace", Version: 1,
		Nodes: []Node{
			{ID: "fan", Type: NodeParallel, Branches: []string{"a1", "a2", "a3", "a4"}},
			{ID: "a1", Type: NodeAgent, AgentBrief: "w"},
			{ID: "a2", Type: NodeAgent, AgentBrief: "w"},
			{ID: "a3", Type: NodeAgent, AgentBrief: "w"},
			{ID: "a4", Type: NodeAgent, AgentBrief: "w"},
		},
		Edges: []Edge{{From: "fan", To: "a1"}},
		Entry: "fan",
	}
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	for i := 0; i < 5; i++ {
		in, _ := eng.Start(d, nil)
		if err := eng.Run(context.Background(), d, in); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoopBound(t *testing.T) {
	d := &WorkflowDef{ID: "lp", Version: 1,
		Nodes: []Node{
			{ID: "loop", Type: NodeLoop, Body: []string{"step"}, MaxIterations: 3, Condition: "go == true"},
			{ID: "step", Type: NodeAgent, AgentBrief: "w"},
			{ID: "end", Type: NodeTool, Tool: "t"},
		},
		Edges: []Edge{{From: "loop", To: "end"}},
		Entry: "loop",
	}
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	in, _ := eng.Start(d, map[string]any{"go": true})
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range in.Records {
		if r.NodeID == "step" && r.Error == "" {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("loop bounded at 3, step ran %d times", count)
	}
}

func TestLoopExitsOnCondition(t *testing.T) {
	// Condition false from the start: body runs once, then exits.
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	d2 := &WorkflowDef{ID: "lp2", Version: 1,
		Nodes: []Node{
			{ID: "loop", Type: NodeLoop, Body: []string{"step"}, MaxIterations: 10, Condition: "go == true"},
			{ID: "step", Type: NodeAgent, AgentBrief: "w"},
		},
		Entry: "loop",
	}
	in, _ := eng.Start(d2, map[string]any{"go": false})
	if err := eng.Run(context.Background(), d2, in); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range in.Records {
		if r.NodeID == "step" && r.Error == "" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("body runs once then exits, ran %d", count)
	}
}

func TestSubflowPinned(t *testing.T) {
	sub := &WorkflowDef{ID: "child", Version: 2,
		Nodes: []Node{{ID: "s1", Type: NodeTool, Tool: "t"}}, Entry: "s1"}
	main := &WorkflowDef{ID: "main", Version: 1,
		Nodes: []Node{
			{ID: "call", Type: NodeSubflow, Subflow: "child", SubflowVersion: 2},
		},
		Entry: "call",
	}
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	if err := eng.RegisterSubflow(sub); err != nil {
		t.Fatal(err)
	}
	in, _ := eng.Start(main, nil)
	if err := eng.Run(context.Background(), main, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceDone {
		t.Fatalf("subflow completes: %+v", in)
	}
	bad := &WorkflowDef{ID: "bad", Version: 1,
		Nodes: []Node{{ID: "call", Type: NodeSubflow, Subflow: "child", SubflowVersion: 99}},
		Entry: "call",
	}
	in2, _ := eng.Start(bad, nil)
	if err := eng.Run(context.Background(), bad, in2); err == nil {
		t.Fatal("wrong subflow version must fail")
	}
}

func TestTransientRetrySameKey(t *testing.T) {
	d := linearDef(Node{ID: "flaky", Type: NodeTool, Tool: "t"})
	var mu sync.Mutex
	attempts := 0
	var keys []string
	exec := &flakyExecutor{failTimes: 2, onCall: func(nc NodeContext) {
		mu.Lock()
		attempts++
		keys = append(keys, nc.IdempotencyKey)
		mu.Unlock()
	}}
	eng := NewEngine(exec)
	eng.sleep = instantSleep
	in, _ := eng.Start(d, nil)
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("two transient failures then success: %d attempts", attempts)
	}
	for _, k := range keys {
		if k != keys[0] {
			t.Fatal("retries reuse the same idempotency key")
		}
	}
}

type flakyExecutor struct {
	mu        sync.Mutex
	failTimes int
	count     int
	onCall    func(NodeContext)
}

func (f *flakyExecutor) ExecuteNode(_ context.Context, nc NodeContext) (NodeOutput, error) {
	f.mu.Lock()
	f.onCall(nc)
	f.count++
	n := f.count
	f.mu.Unlock()
	if n <= f.failTimes {
		return NodeOutput{}, errors.New("provider 503 overloaded")
	}
	return NodeOutput{Data: map[string]any{"ok": true}}, nil
}

func TestLogicFailureNoBlindRetry(t *testing.T) {
	d := linearDef(Node{ID: "n", Type: NodeTool, Tool: "t"})
	exec := &scriptExecutor{fail: map[string]error{"n": errors.New("tests failed")}}
	eng := NewEngine(exec)
	eng.sleep = instantSleep
	in, _ := eng.Start(d, nil)
	if err := eng.Run(context.Background(), d, in); err == nil {
		t.Fatal("logic failure must fail, not loop")
	}
	if len(exec.calls) != 1 {
		t.Fatalf("no blind retry on logic failure: %v", exec.calls)
	}
}

func TestScriptNodeRuns(t *testing.T) {
	d := linearDef(Node{ID: "s", Type: NodeScript, Script: "echo hi", ScriptLang: "sh"})
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	in, _ := eng.Start(d, nil)
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceDone {
		t.Fatalf("script node completes: %+v", in)
	}
}

func TestWaitTimeNode(t *testing.T) {
	d := linearDef(
		Node{ID: "w", Type: NodeWait, WaitKind: WaitUntilTime, WaitFor: "1ms"},
		Node{ID: "n", Type: NodeTool, Tool: "t"},
	)
	exec := &scriptExecutor{}
	eng := NewEngine(exec)
	in, _ := eng.Start(d, nil)
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	if in.State != InstanceDone {
		t.Fatalf("wait continues along edges: %+v", in)
	}
}
