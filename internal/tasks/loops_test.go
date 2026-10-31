// Loop detector tests (Section 18.3; verification row 18.5.3):
// planted trajectories for R1-R6 fire with the staged response
// (note, expert, blocked), legitimately repetitive trajectories
// using wait_for do not stop, the judge only fires the note, and
// the false-positive rate on benign traffic stays measured.
package tasks

import (
	"strings"
	"testing"
)

func feed(d *LoopDetector, ev ToolEvent, n int) []Detection {
	var out []Detection
	for i := 0; i < n; i++ {
		out = d.ObserveTool(ev)
	}
	return out
}

func lastAction(ds []Detection) LoopAction {
	if len(ds) == 0 {
		return ActionNone
	}
	return ds[len(ds)-1].Action
}

func TestR1IdenticalRepeat(t *testing.T) {
	d := NewLoopDetector()
	ev := ToolEvent{Tool: "exec", Args: "cargo test", Result: "ok 3 passed"}
	if lastAction(feed(d, ev, 2)) != ActionNone {
		t.Fatal("two identical results must not fire")
	}
	ds := feed(d, ev, 1)
	if lastAction(ds) != ActionNote || ds[len(ds)-1].Rule != RuleIdenticalRepeat {
		t.Fatalf("third identical result must note: %+v", ds)
	}
}

func TestR2SameError(t *testing.T) {
	d := NewLoopDetector()
	ev := ToolEvent{Tool: "exec", Args: "cargo test", Result: "", Error: "E0432 unresolved import"}
	feed(d, ev, 2)
	ds := feed(d, ev, 1)
	if lastAction(ds) != ActionNote || ds[len(ds)-1].Rule != RuleSameError {
		t.Fatalf("third same error must note: %+v", ds)
	}
	if !strings.Contains(ds[len(ds)-1].Note, "E0432") {
		t.Fatalf("note must name the pattern: %q", ds[len(ds)-1].Note)
	}
}

func TestR3Cycle(t *testing.T) {
	d := NewLoopDetector()
	seq := []ToolEvent{
		{Tool: "read", Args: "a.go", Result: "x"},
		{Tool: "edit", Args: "a.go", Result: "y"},
	}
	var ds []Detection
	for i := 0; i < 3; i++ {
		for _, ev := range seq {
			ds = d.ObserveTool(ev)
		}
	}
	if len(ds) == 0 || ds[0].Rule != RuleCycle {
		t.Fatalf("2-call cycle x3 must fire R3: %+v", ds)
	}
}

func TestR4NoProgress(t *testing.T) {
	d := NewLoopDetector()
	st := ProgressState{Plans: "p1", Files: "f1", Proof: "e1", Info: "i1"}
	var ds []Detection
	for i := 0; i < LoopNoProgressSteps; i++ {
		ds = d.ObserveProgress(st)
	}
	if len(ds) == 0 || ds[0].Rule != RuleNoProgress {
		t.Fatalf("8 frozen steps must fire R4: %+v", ds)
	}
	// A state change resets the window.
	d2 := NewLoopDetector()
	for i := 0; i < LoopNoProgressSteps-1; i++ {
		d2.ObserveProgress(st)
	}
	d2.ObserveProgress(ProgressState{Plans: "p2", Files: "f1", Proof: "e1", Info: "i1"})
	if ds := d2.ObserveProgress(st); len(ds) != 0 {
		t.Fatalf("changed state must reset R4: %+v", ds)
	}
}

func TestR5RepeatedText(t *testing.T) {
	d := NewLoopDetector()
	frag := strings.Repeat("x", LoopTextFragment)
	chunk := frag + "tail"
	var ds []Detection
	for i := 0; i < LoopTextRepeats; i++ {
		ds = d.ObserveText(chunk)
	}
	if len(ds) == 0 || ds[0].Rule != RuleRepeatedText {
		t.Fatalf("50-char fragment x10 must fire R5: %+v", ds)
	}
}

func TestR6EditMismatch(t *testing.T) {
	d := NewLoopDetector()
	var ds []Detection
	for i := 0; i < LoopEditMismatches; i++ {
		ds = d.ObserveEditMismatch("a.go", true)
	}
	if len(ds) == 0 || ds[len(ds)-1].Rule != RuleEditMismatch {
		t.Fatalf("3 mismatches must fire R6: %+v", ds)
	}
	d.ObserveEditMismatch("a.go", false)
	if ds := d.ObserveEditMismatch("a.go", true); len(ds) != 0 {
		t.Fatalf("a match must reset R6: %+v", ds)
	}
}

func TestStagedResponseNoteEscalateBlock(t *testing.T) {
	d := NewLoopDetector()
	ev := ToolEvent{Tool: "exec", Args: "make test", Result: "same fail"}
	var actions []LoopAction
	for i := 0; i < 3; i++ {
		feed(d, ev, LoopRepeatIdentical)
		got := d.Counts()[RuleIdenticalRepeat]
		actions = append(actions, StageAction(got))
		// Fresh identical window each round: vary then repeat.
		d.h2Window = nil
	}
	if actions[0] != ActionNote || actions[1] != ActionEscalate || actions[2] != ActionBlock {
		t.Fatalf("stages must be note, escalate, blocked: %v", actions)
	}
}

func TestLegitimateWaitForNotStopped(t *testing.T) {
	// wait_for polling with an identical result is legitimately
	// repetitive and exempt: it must never stop the run.
	d := NewLoopDetector()
	for i := 0; i < 10; i++ {
		if ds := d.ObserveTool(ToolEvent{Tool: "wait_for", Args: "port 8080", Result: "not ready, retry"}); len(ds) != 0 {
			t.Fatalf("exempt wait_for polling must not fire: %+v", ds)
		}
	}
	// The exemption is configurable: without it, identical
	// results trip R1 like any other tool.
	d2 := NewLoopDetector()
	d2.LongPoll = map[string]bool{}
	ev := ToolEvent{Tool: "wait_for", Args: "port 8080", Result: "not ready, retry"}
	feed(d2, ev, 2)
	if ds := feed(d2, ev, 1); len(ds) == 0 {
		t.Fatal("non-exempt repetition must still fire R1")
	}
	// Varying results on a normal tool never fire either.
	d3 := NewLoopDetector()
	for i := 0; i < 12; i++ {
		ds := d3.ObserveTool(ToolEvent{
			Tool: "exec", Args: "poll",
			Result: strings.Repeat("x", i) + "tick",
		})
		if len(ds) != 0 {
			t.Fatalf("changing results must not fire: %+v", ds)
		}
	}
}

func TestJudgeOnlyFiresNote(t *testing.T) {
	d := NewLoopDetector()
	st := ProgressState{Plans: "p", Files: "f", Proof: "e", Info: "i"}
	for i := 0; i < StuckJudgeFromStep; i++ {
		d.ObserveProgress(st)
	}
	before := d.Counts()[RuleNoProgress]
	ds := d.MaybeJudge(func(step int) (bool, float64) { return true, 0.95 })
	if len(ds) != 1 || ds[0].Action != ActionNote {
		t.Fatalf("judge must only fire the stage-1 note: %+v", ds)
	}
	if d.Counts()[RuleNoProgress] != before {
		t.Fatal("judge must not advance the escalation counters")
	}
	// Below threshold the judge stays silent.
	d2 := NewLoopDetector()
	for i := 0; i < StuckJudgeFromStep; i++ {
		d2.ObserveProgress(ProgressState{Plans: "p"})
	}
	if ds := d2.MaybeJudge(func(step int) (bool, float64) { return true, 0.5 }); len(ds) != 0 {
		t.Fatalf("p < 0.9 must not count: %+v", ds)
	}
}

func TestFalsePositiveRateOnBenignTraffic(t *testing.T) {
	// Benign traffic: varied tools, changing results, progressing
	// state. Measure the false-positive rate; it must be zero.
	d := NewLoopDetector()
	fp := 0
	total := 0
	tools := []string{"read", "exec", "edit", "search", "exec", "read"}
	for i := 0; i < 60; i++ {
		ev := ToolEvent{Tool: tools[i%len(tools)], Args: "arg", Result: "result"}
		ev.Args += strings.Repeat("x", i%7)
		ev.Result += strings.Repeat("y", i%11)
		if ds := d.ObserveTool(ev); len(ds) != 0 {
			fp++
		}
		total++
		st := ProgressState{Plans: "p", Info: strings.Repeat("z", i)}
		if ds := d.ObserveProgress(st); len(ds) != 0 {
			fp++
		}
		total++
	}
	t.Logf("false-positive rate: %d/%d", fp, total)
	if fp != 0 {
		t.Fatalf("false positives on benign traffic: %d/%d", fp, total)
	}
}
