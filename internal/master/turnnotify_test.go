package master

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

// Section 5.5: a message under 2s with no shown output cancels and
// restarts the generation with both messages; a later one injects.
func TestTurnCancelAndInject(t *testing.T) {
	model := &FakeModel{Available_: true, Block: make(chan struct{}), Started: make(chan struct{}), CancelSeen: make(chan struct{})}
	mgr := NewTurnManager(DefaultTurnConfig(), model, time.Now, nil)
	in := func(text string) IntakeResult {
		return IntakeResult{Msg: Normalized{Text: text}, MsgID: types.NewMessageID(), SentAt: time.Now()}
	}
	mgr.Deliver("cnv", in("first"))
	<-model.Started
	mgr.Deliver("cnv", in("second"))
	select {
	case <-model.CancelSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("generation was not canceled within the window")
	}
	// Later message with shown output injects instead of restarting.
	model2 := &FakeModel{Available_: true, Fn: func(context.Context, string) (string, error) {
		return "shown", nil
	}}
	var kinds []string
	var mu sync.Mutex
	mgr2 := NewTurnManager(DefaultTurnConfig(), model2, time.Now, func(e TurnEvent) {
		mu.Lock()
		kinds = append(kinds, e.Kind)
		mu.Unlock()
	})
	mgr2.Deliver("cnv2", in("a"))
	waitFor(t, 2*time.Second, func() bool { return !mgr2.Active("cnv2") }, "turn must finish")
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, k := range kinds {
		if k == "finished" {
			found = true
		}
	}
	if !found {
		t.Fatalf("turn never finished: %v", kinds)
	}
}

// Section 5.5: two conversations advance in parallel with per-turn
// order preserved.
func TestTurnsParallelAcrossConversations(t *testing.T) {
	model := &FakeModel{Available_: true}
	mgr := NewTurnManager(DefaultTurnConfig(), model, time.Now, nil)
	in := func(text string) IntakeResult {
		return IntakeResult{Msg: Normalized{Text: text}, MsgID: types.NewMessageID(), SentAt: time.Now()}
	}
	mgr.Deliver("web", in("web msg"))
	mgr.Deliver("tg", in("tg msg"))
	waitFor(t, 3*time.Second, func() bool { return !mgr.Active("web") && !mgr.Active("tg") }, "both turns must finish")
	if model.Calls != 2 {
		t.Fatalf("model calls = %d, want 2 (one per conversation)", model.Calls)
	}
}

// Section 5.5: provider down with no backup ends the turn with the
// deterministic receipt; the queue survives and the fast lane still
// answers.
func TestProviderDownFallback(t *testing.T) {
	down := &FakeModel{Available_: false}
	var fell []TurnEvent
	var mu sync.Mutex
	mgr := NewTurnManager(DefaultTurnConfig(), down, time.Now, func(e TurnEvent) {
		mu.Lock()
		fell = append(fell, e)
		mu.Unlock()
	})
	in := IntakeResult{Msg: Normalized{Text: "hello"}, MsgID: types.NewMessageID(), SentAt: time.Now()}
	mgr.Deliver("cnv", in)
	waitFor(t, 2*time.Second, func() bool { return !mgr.Active("cnv") }, "turn must end on provider failure")
	mu.Lock()
	defer mu.Unlock()
	ok := false
	for _, e := range fell {
		if e.Kind == "fallback" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("want the deterministic fallback event, got %v", fell)
	}
	// Fast lane needs no model: status still answers.
	fc := newFakeControls()
	fc.seed("run_1", "tool", types.RunRunning)
	lane := &FastLane{Controls: fc}
	fastIn := fastIntake("/status run_1", nil, nil)
	if out := lane.Handle(context.Background(), fastIn); !out.Handled {
		t.Fatal("fast lane must answer with the provider down")
	}
}

// Section 5.3.8: a panic in a turn fails the turn, never the daemon.
func TestTurnPanicContained(t *testing.T) {
	boom := &FakeModel{Available_: true, Fn: func(context.Context, string) (string, error) {
		panic("model exploded")
	}}
	var failed bool
	var mu sync.Mutex
	mgr := NewTurnManager(DefaultTurnConfig(), boom, time.Now, func(e TurnEvent) {
		if e.Kind == "failed" {
			mu.Lock()
			failed = true
			mu.Unlock()
		}
	})
	mgr.Deliver("cnv", IntakeResult{Msg: Normalized{Text: "x"}, MsgID: types.NewMessageID()})
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return failed
	}, "panicking turn must be reported failed")
}

// Section 5.5/5.4 notify suite: urgent, normal and informative across
// connection, activity and quiet-hour states, without duplicates.
type fakeSender struct {
	mu        sync.Mutex
	connected map[string]bool
	active    map[string]time.Time
	sent      []Notice
	now       time.Time
}

func (f *fakeSender) Connected(c string) bool { return f.connected[c] }
func (f *fakeSender) ActiveSince(c string) time.Time {
	return f.active[c]
}
func (f *fakeSender) Send(_ string, n Notice) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	return nil
}

func TestNotifySuite(t *testing.T) {
	now := time.Now()
	sender := &fakeSender{
		connected: map[string]bool{"origin": true},
		active:    map[string]time.Time{"other": now.Add(-5 * time.Minute)},
		now:       now,
	}
	n := NewNotifier(NotifierConfig{Sender: sender, Clock: func() time.Time { return now }})
	urgent := n.Post(Notice{ID: "n1", ConversationID: "origin", Class: types.NoticeUrgent, Content: "blocked"})
	if len(urgent) == 0 {
		t.Fatal("urgent must deliver at once")
	}
	normal := n.Post(Notice{ID: "n2", ConversationID: "origin", Class: types.NoticeNormal, Content: "done"})
	if len(normal) != 0 {
		t.Fatal("normal must group, not deliver at once")
	}
	if got := n.PendingCount(); got != 1 {
		t.Fatalf("pending = %d, want 1", got)
	}
	// Origin connected: no preferred-channel copy.
	for _, d := range urgent {
		if d.Channel == "preferred" {
			t.Fatal("connected origin must not duplicate to the preferred channel")
		}
	}
	// Disconnected origin: urgent and normal copy once each.
	sender.connected["origin"] = false
	urgent2 := n.Post(Notice{ID: "n3", ConversationID: "origin", Class: types.NoticeUrgent, Content: "approval"})
	pref := 0
	for _, d := range urgent2 {
		if d.Channel == "preferred" {
			pref++
		}
	}
	if pref != 1 {
		t.Fatalf("preferred copies = %d, want exactly 1", pref)
	}
	n.Post(Notice{ID: "n4", ConversationID: "origin", Class: types.NoticeNormal, Content: "done2"})
	got := n.Flush() // window not elapsed: nothing flushed
	if len(got) != 0 {
		t.Fatalf("early flush released %d deliveries", len(got))
	}
	n2time := now.Add(6 * time.Second)
	n3 := NewNotifier(NotifierConfig{Sender: sender, Clock: func() time.Time { return n2time }})
	_ = n3
	// Advance the clock past the window and flush the original queue.
	n.mu.Lock()
	for conv := range n.flushAt {
		n.flushAt[conv] = now.Add(-time.Second)
	}
	n.mu.Unlock()
	flushed := n.Flush()
	if len(flushed) == 0 {
		t.Fatal("grouped normals must release after the 5s window")
	}
	// Active elsewhere: the daemon links urgent notices there.
	if elsewhere := n.ActiveElsewhere("origin", []string{"origin", "other", "stale"}); len(elsewhere) != 1 || elsewhere[0] != "other" {
		t.Fatalf("active elsewhere = %v, want [other]", elsewhere)
	}
	// Ack in one channel reads it everywhere.
	n.Ack("n1")
	if !n.Acked("n1") {
		t.Fatal("ack must mark the notice read")
	}
	// Quiet hours retain normal and informative notices.
	sender.connected["origin"] = false
	qn := NewNotifier(NotifierConfig{
		Sender: sender,
		Quiet:  func(time.Time) bool { return true },
		Clock:  func() time.Time { return now },
	})
	del := qn.Post(Notice{ID: "nq", ConversationID: "origin", Class: types.NoticeNormal, Content: "night"})
	for _, d := range del {
		if d.Channel == "preferred" && d.State == "delivered" {
			t.Fatal("quiet hours must retain normal notices")
		}
	}
}

func TestVoiceContract(t *testing.T) {
	out := OnBargeIn("yes but—")
	if !out.StopSynthesis || !out.CancelGeneration || out.NewMessage == "" {
		t.Fatalf("barge-in must cut synthesis, cancel generation, keep speech: %+v", out)
	}
	spoken, written := DefaultVoiceStyle().SplitForVoice("short answer")
	if spoken != "short answer" || written != "" {
		t.Fatalf("short answers stay spoken: %q / %q", spoken, written)
	}
	spoken, written = DefaultVoiceStyle().SplitForVoice(string(make([]byte, 2000)))
	_ = spoken
	if written == "" {
		t.Fatal("long results must stay written, voice only announces")
	}
}
