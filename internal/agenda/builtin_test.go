package agenda

import (
	"context"
	"sync"
	"testing"
)

// 21.7/21.10: built-ins never send without confirmation, respect round
// caps and cite only read sources.

func TestAllBuiltinsValid(t *testing.T) {
	defs, err := AllBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 6 {
		t.Fatalf("want 6 built-ins, have %d", len(defs))
	}
	for _, d := range defs {
		if bad := BoundedRounds(d); len(bad) != 0 {
			t.Fatalf("%s has unbounded loops: %v", d.ID, bad)
		}
	}
}

func TestJobPipelineConfirmBeforeSend(t *testing.T) {
	d, ok := Builtin(BuiltinJobPipeline)
	if !ok {
		t.Fatal("missing job-application-pipeline")
	}
	if bad := ConfirmBeforeSend(d); len(bad) != 0 {
		t.Fatalf("send without confirmation: %v", bad)
	}
	// A copy without the confirm node must be caught.
	stripped := *d
	stripped.Nodes = []Node{}
	for _, n := range d.Nodes {
		if n.Type == NodeConfirm {
			continue
		}
		stripped.Nodes = append(stripped.Nodes, n)
	}
	stripped.Edges = []Edge{}
	for _, e := range d.Edges {
		if e.From == "confirm_send" || e.To == "confirm_send" {
			continue
		}
		stripped.Edges = append(stripped.Edges, e)
	}
	stripped.Edges = append(stripped.Edges, Edge{From: "prepare", To: "send"})
	if bad := ConfirmBeforeSend(&stripped); len(bad) == 0 {
		t.Fatal("missing confirmation must be detected")
	}
}

func TestJobPipelineDryRunAndDecline(t *testing.T) {
	d, _ := Builtin(BuiltinJobPipeline)
	// Dry run: the executor never runs the send tool.
	var ran []string
	exec := &spyExecutor{calls: &ran}
	eng := NewEngine(exec).WithConfirmer(&mockConfirmer{confirm: true})
	in, err := eng.Start(d, DryRunScope(map[string]any{"cv": "cv.pdf"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), d, in); err != nil {
		t.Fatal(err)
	}
	for _, c := range ran {
		if c == "send" {
			t.Fatal("dry run must not run the send tool")
		}
	}
	// Declined confirmation: aborts before send.
	ran = nil
	eng2 := NewEngine(exec).WithConfirmer(&mockConfirmer{confirm: false})
	in2, err := eng2.Start(d, map[string]any{"cv": "cv.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	// Mocks for effect-free progress: spy returns placeholders, so
	// prepare's agents run; send must never run.
	if err := eng2.Run(context.Background(), d, in2); err != nil {
		t.Fatal(err)
	}
	if in2.State != InstanceAborted {
		t.Fatalf("declined send aborts: %+v", in2)
	}
	for _, c := range ran {
		if c == "send" {
			t.Fatal("declined confirmation must not send")
		}
	}
}

type spyExecutor struct {
	mu    sync.Mutex
	calls *[]string
}

func (s *spyExecutor) ExecuteNode(_ context.Context, nc NodeContext) (NodeOutput, error) {
	s.mu.Lock()
	*s.calls = append(*s.calls, nc.Node.ID)
	s.mu.Unlock()
	return NodeOutput{Data: map[string]any{"mock": nc.Node.ID}}, nil
}

func TestBuiltinTestsPass(t *testing.T) {
	defs, err := AllBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if len(d.Tests) == 0 {
			t.Fatalf("%s ships no tests", d.ID)
		}
		for _, r := range RunTests(d, nil) {
			if !r.Passed {
				t.Fatalf("%s test %q failed: %s", d.ID, r.Name, r.Failure)
			}
		}
	}
}

func TestVerifyCitedSources(t *testing.T) {
	read := map[string]bool{"https://a.example": true}
	if bad := VerifyCitedSources([]string{"https://a.example"}, read); len(bad) != 0 {
		t.Fatalf("read sources pass: %v", bad)
	}
	if bad := VerifyCitedSources([]string{"https://a.example", "https://b.example"}, read); len(bad) != 1 || bad[0] != "https://b.example" {
		t.Fatalf("unread citations flagged: %v", bad)
	}
}

func TestDebateBounded(t *testing.T) {
	d, _ := Builtin(BuiltinDebate)
	var maxRounds int
	for _, n := range d.Nodes {
		if n.ID == "debate" {
			maxRounds = n.MaxIterations
		}
	}
	if maxRounds < 1 || maxRounds > 5 {
		t.Fatalf("debate rounds must be bounded small, have %d", maxRounds)
	}
}
