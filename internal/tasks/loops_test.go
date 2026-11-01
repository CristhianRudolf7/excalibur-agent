// Loop detector tests (Section 18.3; verification row 18.5.3):
// planted trajectories for R1-R6 fire with the staged response
// (note, expert, blocked), legitimately repetitive trajectories
// using wait_for do not stop, the judge only fires the note, and
// the false-positive rate on benign traffic stays measured.
package tasks

import (
	"strconv"
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
	// The same command polled with varying results fires the
	// near-repeat note (agent-quality loop breaker, TestNearRepeat):
	// the outputs change but the approach does not. The run is not
	// stopped here: wait_for stays exempt and short polls stay
	// silent, so only the new rule's note fires below.
	d3 := NewLoopDetector()
	for i := 0; i < 12; i++ {
		ds := d3.ObserveTool(ToolEvent{
			Tool: "exec", Args: "poll",
			Result: strings.Repeat("x", i) + "tick",
		})
		if i+1 < LoopNearRepeat && len(ds) != 0 {
			t.Fatalf("short polls must stay silent: %+v", ds)
		}
	}
	if got := d3.Counts()[RuleNearRepeat]; got == 0 {
		t.Fatal("same-command polls must fire near_repeat (see TestNearRepeat)")
	}
}

// TestNearRepeat fires on the same command polled with varying
// results (agent-quality loop breaker): R1/R2 stay silent because
// no h1/h2 hash repeats, but the approach does not change. Short
// polls stay silent and the first detection is a note, so the run
// is never stopped by a couple of status checks.
func TestNearRepeat(t *testing.T) {
	d := NewLoopDetector()
	mk := func(i int) ToolEvent {
		return ToolEvent{Tool: "exec", Args: "poll", Result: "tick " + strings.Repeat("x", i)}
	}
	for i := 0; i < LoopNearRepeat-1; i++ {
		if ds := d.ObserveTool(mk(i)); len(ds) != 0 {
			t.Fatalf("short polls must stay silent: %+v", ds)
		}
	}
	ds := d.ObserveTool(mk(LoopNearRepeat - 1))
	if len(ds) != 1 || ds[0].Rule != RuleNearRepeat || ds[0].Action != ActionNote {
		t.Fatalf("third same-command poll must note near_repeat: %+v", ds)
	}
	// Cosmetic argument tweaks count as near-identical too.
	d2 := NewLoopDetector()
	for _, args := range []string{"ls /app", "ls  /app", "ls /app/"} {
		d2.ObserveTool(ToolEvent{Tool: "exec", Args: args, Result: "r"})
	}
	if last := d2.Counts()[RuleNearRepeat]; last != 1 {
		t.Fatalf("cosmetic tweaks must fire near_repeat once, counts=%v", d2.Counts())
	}
	// A different tool or a genuinely different command restarts the run.
	d3 := NewLoopDetector()
	d3.ObserveTool(ToolEvent{Tool: "exec", Args: "poll a", Result: "1"})
	d3.ObserveTool(ToolEvent{Tool: "exec", Args: "poll a", Result: "2"})
	d3.ObserveTool(ToolEvent{Tool: "read_file", Args: "poll a", Result: "3"})
	if ds := d3.ObserveTool(ToolEvent{Tool: "exec", Args: "poll a", Result: "4"}); len(ds) != 0 {
		t.Fatalf("a different tool in between must restart the run: %+v", ds)
	}
	d4 := NewLoopDetector()
	d4.ObserveTool(ToolEvent{Tool: "exec", Args: "make build", Result: "1"})
	d4.ObserveTool(ToolEvent{Tool: "exec", Args: "make build", Result: "2"})
	d4.ObserveTool(ToolEvent{Tool: "exec", Args: "make test", Result: "3"})
	if ds := d4.ObserveTool(ToolEvent{Tool: "exec", Args: "make test", Result: "4"}); len(ds) != 0 {
		t.Fatalf("a different command must restart the run: %+v", ds)
	}
	// Persisting past the first note escalates through the standard
	// stages: note, escalate, blocked. Results vary so the
	// exact-match R1 owns nothing and the near rule stages alone.
	d5 := NewLoopDetector()
	var last []Detection
	for i := 0; i < LoopNearRepeat*3; i++ {
		last = d5.ObserveTool(ToolEvent{Tool: "exec", Args: "poll", Result: "tick " + strings.Repeat("y", i)})
	}
	if len(last) == 0 || last[0].Action != ActionBlock {
		t.Fatalf("persisting near-repeat must reach blocked: %+v", last)
	}
	if got := d5.Counts()[RuleNearRepeat]; got != 3 {
		t.Fatalf("near_repeat must stage 3 times, got %d", got)
	}
}

// TestNearRepeatSimilarity pins the argument similarity: identical
// and cosmetic variants match, unrelated commands do not.
func TestNearRepeatSimilarity(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"ls /app", "ls /app", true},
		{"ls  /app", "ls /app", true},
		{"ls /app", "ls /app/", true},
		{"ps -p 71 -o pid,stat,etime,cmd", "ps -p 71 -o pid,stat,etime,cmd", true},
		{"make build", "make test", false},
		{"", "ls", false},
		{"", "", true},
		{"python3 -c 'print(1)'", "rm -rf /tmp/work && git clone https://example.com/r.git", false},
	} {
		if got := nearArgs(collapseSpaces(tc.a), collapseSpaces(tc.b)); got != tc.want {
			t.Errorf("nearArgs(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestObserveNoInfo fires every LoopNoInfoSteps dull steps with the
// standard staging, and resets on informative steps.
func TestObserveNoInfo(t *testing.T) {
	d := NewLoopDetector()
	for i := 0; i < LoopNoInfoSteps-1; i++ {
		if ds := d.ObserveNoInfo(false); len(ds) != 0 {
			t.Fatalf("dull step %d must stay silent: %+v", i, ds)
		}
	}
	ds := d.ObserveNoInfo(false)
	if len(ds) != 1 || ds[0].Rule != RuleNoInfo || ds[0].Action != ActionNote {
		t.Fatalf("K dull steps must note no_info: %+v", ds)
	}
	if ds := d.ObserveNoInfo(true); len(ds) != 0 {
		t.Fatalf("informative step must reset silently: %+v", ds)
	}
	for i := 0; i < LoopNoInfoSteps-1; i++ {
		d.ObserveNoInfo(false)
	}
	ds = d.ObserveNoInfo(false)
	if len(ds) != 1 || ds[0].Action != ActionEscalate {
		t.Fatalf("second K dull steps must escalate: %+v", ds)
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

// Item 75: the near-repeat detector escalates on its second firing,
// so a persisting poll loop gets an expert-level response, not only a
// second note.
func TestNearRepeatEscalatesOnSecondHit(t *testing.T) {
	d := NewLoopDetector()
	var actions []LoopAction
	for i := 0; i < 2*LoopNearRepeat; i++ {
		for _, det := range d.ObserveTool(ToolEvent{Tool: "exec", Args: "poll", Result: "tick " + strings.Repeat("x", i)}) {
			if det.Rule == RuleNearRepeat {
				actions = append(actions, det.Action)
			}
		}
	}
	if len(actions) != 2 || actions[0] != ActionNote || actions[1] != ActionEscalate {
		t.Fatalf("near_repeat actions = %v, want [note escalate]", actions)
	}
}

// Item 15: the exact-call guard notes the third identical result and
// refuses the exact call for the next five turns at the sixth.
func TestExactCallGuardNoteAndRefusal(t *testing.T) {
	g := NewExactCallGuard()
	count := ExactCountKey("read_file", `{"path":"a"}`, ResultFingerprint("same"))
	call := ExactCallKey("read_file", `{"path":"a"}`)
	var notes []string
	for turn := 1; turn <= ExactCallBanAt; turn++ {
		note, banned := g.Observe(turn, count, call)
		if note != "" {
			notes = append(notes, note)
		}
		if banned != (turn == ExactCallBanAt) {
			t.Fatalf("turn %d banned=%v", turn, banned)
		}
	}
	if len(notes) != 2 || notes[0] != ExactCallNote(3) || !strings.Contains(notes[1], "refused for the next 5 turns") {
		t.Fatalf("notes = %q", notes)
	}
	// The window covers the five turns after the sixth occurrence.
	for turn := ExactCallBanAt + 1; turn <= ExactCallBanAt+ExactCallBanTurns; turn++ {
		msg, refused := g.Refusal(turn, call)
		if !refused {
			t.Fatalf("turn %d must be refused", turn)
		}
		if want := ExactCallBanAt + ExactCallBanTurns - turn + 1; !strings.Contains(msg, "refused for "+strconv.Itoa(want)+" more turns") {
			t.Fatalf("turn %d refusal = %q", turn, msg)
		}
	}
	if _, refused := g.Refusal(ExactCallBanAt+ExactCallBanTurns+1, call); refused {
		t.Fatal("refusal must lapse after the window")
	}
}

// Item 15: a workspace change restarts the count, because the same call
// may legitimately return the same result afterwards.
func TestExactCallGuardStateChangeResetsCounts(t *testing.T) {
	g := NewExactCallGuard()
	count := ExactCountKey("exec", "pwd", ResultFingerprint("/app"))
	call := ExactCallKey("exec", `{"command":"pwd"}`)
	for turn := 1; turn <= 2; turn++ {
		g.Observe(turn, count, call)
	}
	g.MarkStateChange()
	// The first identical result after the change is occurrence one of a
	// new count, so the note waits for the third of them, at turn 5.
	if note, _ := g.Observe(3, count, call); note != "" {
		t.Fatalf("first identical result after a change must not note: %q", note)
	}
	if note, _ := g.Observe(4, count, call); note != "" {
		t.Fatalf("second identical result after a change must not note: %q", note)
	}
	if note, _ := g.Observe(5, count, call); note != ExactCallNote(3) {
		t.Fatalf("third identical result after a change must note, got %q", note)
	}
}

// Item 15 / 79: a relaunch that produces the same observation is the same
// call. Counts are per observation: distinct results never add up, while
// alternating results each reach the third occurrence on their own.
func TestExactCallGuardKeysByResult(t *testing.T) {
	g := NewExactCallGuard()
	call := ExactCallKey("exec", `{"command":"qemu"}`)
	for turn, out := range []string{"a", "b", "c", "d", "e", "f"} {
		count := ExactCountKey("exec", "", ResultFingerprint(out))
		if note, banned := g.Observe(turn+1, count, call); note != "" || banned {
			t.Fatalf("distinct results must never count: %q %v", note, banned)
		}
	}
	var notes []int
	for turn, out := range []string{"up", "down", "up", "down", "up", "down"} {
		count := ExactCountKey("exec", "", ResultFingerprint(out))
		if note, _ := g.Observe(turn+10, count, call); note != "" {
			notes = append(notes, turn+10)
		}
	}
	if len(notes) != 2 || notes[0] != 14 || notes[1] != 15 {
		t.Fatalf("alternating results note at the third occurrence of each: %v", notes)
	}
}

// Volatile exec header tokens (duration, session id, pid, clock times)
// do not make two identical observations differ.
func TestResultFingerprintIgnoresVolatileTokens(t *testing.T) {
	a := "exit: 0  duration: 1.2s  session: sess-7  alive: false\nout 10:11:12\n"
	b := "exit: 0  duration: 9.8s  session: sess-8  alive: false\nout 22:01:59\n"
	if ResultFingerprint(a) != ResultFingerprint(b) {
		t.Fatal("volatile tokens must not change the fingerprint")
	}
	if ResultFingerprint(a) == ResultFingerprint("exit: 1  duration: 1.2s  session: sess-7  alive: false\nout 10:11:12\n") {
		t.Fatal("a different exit code must change the fingerprint")
	}
}

func TestNormalizeCallArgs(t *testing.T) {
	if NormalizeCallArgs(`{"b": 2, "a":"x"}`) != NormalizeCallArgs(`{"a":"x","b":2}`) {
		t.Fatal("argument key order must not matter")
	}
	if NormalizeCallArgs(`{"n": 1.0}`) != `{"n":1.0}` {
		t.Fatalf("numbers must be kept as written: %s", NormalizeCallArgs(`{"n": 1.0}`))
	}
	if NormalizeCallArgs("ls   /app") != "ls /app" {
		t.Fatal("non-JSON arguments collapse whitespace")
	}
}

// Pure echo, printf, true and sleep commands with a constant output
// count as identical; anything that may write does not.
func TestPureAndWritingCommands(t *testing.T) {
	for _, cmd := range []string{"echo hi", "printf 'x\\n'", "true", "sleep 30"} {
		if !IsPureCommand(cmd) {
			t.Fatalf("%q must be pure", cmd)
		}
	}
	for _, cmd := range []string{"echo hi > out.txt", "printf x >> log", "sleep 1 && tee f", "echo x | tee a", "cat a"} {
		if IsPureCommand(cmd) {
			t.Fatalf("%q must not be pure", cmd)
		}
	}
	for _, cmd := range []string{"cp a b", "mv a b", "rm -f x", "mkdir -p d", "sed -i s/a/b/ f", "tar -xf a.tar", "make > build.log"} {
		if !MayWriteCommand(cmd) {
			t.Fatalf("%q must count as a possible write", cmd)
		}
	}
	for _, cmd := range []string{"ls -la", "test -s a.txt && ls -l a.txt", "cat a 2>&1", "cmd > /dev/null", "echo hi 2>&1"} {
		if MayWriteCommand(cmd) {
			t.Fatalf("%q must not count as a write", cmd)
		}
	}
}

// Item 106: a session that is still running is progress, not a repeat.
func TestLiveSessionOutput(t *testing.T) {
	if !LiveSessionOutput("exit: running  duration: 30.0s  session: sess-1  alive: true  next_offset: 4\n") {
		t.Fatal("a running exec header is live")
	}
	if !LiveSessionOutput("exited: false  next_offset: 9\nbuilding\n") {
		t.Fatal("an unfinished exec_io read is live")
	}
	if LiveSessionOutput("exit: 0  duration: 1.0s  session: sess-1  alive: false\n") {
		t.Fatal("a finished session is not live")
	}
}

// Finish repeats share the tally: notes at the third and sixth
// occurrence, and no refusal window (the finish protocol owns refusals).
func TestExactCallGuardTallyNoWindow(t *testing.T) {
	g := NewExactCallGuard()
	count := ExactCountKey("finish", "", ResultFingerprint("gate pending"))
	var notes []string
	for i := 0; i < 7; i++ {
		if n := g.Tally(count); n != "" {
			notes = append(notes, n)
		}
	}
	if len(notes) != 2 || notes[0] != ExactCallNote(3) || notes[1] != ExactCallNote(6) {
		t.Fatalf("tally notes = %q", notes)
	}
	if _, refused := g.Refusal(8, ExactCallKey("finish", "")); refused {
		t.Fatal("tally must never open a refusal window")
	}
}
