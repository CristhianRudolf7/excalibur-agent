package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func classroomEvent(id string, kind types.EventType, topic string, final bool) types.Event {
	raw, _ := json.Marshal(classroomTurnEvent{CourseID: "c", TopicID: topic, Final: final, AttemptID: func() string {
		if strings.Contains(string(kind), "graded") || strings.Contains(string(kind), "feedback") {
			return "a"
		}
		return ""
	}()})
	return types.Event{ID: id, Type: kind, Payload: raw}
}
func systemRows(b *DaemonBackend) []master.HistoryMessage {
	rows := []master.HistoryMessage{}
	for _, r := range b.Master.History(ClassroomConversation, 0) {
		if r.Role == "system" {
			rows = append(rows, r)
		}
	}
	return rows
}
func TestClassroomEventsStartOneSystemTurn(t *testing.T) {
	for _, tc := range []struct {
		kind          types.EventType
		topic, action string
		final         bool
	}{
		{"classroom.class_requested", "t", "write_class", false},
		{"classroom.quiz_requested", "t", "create_quiz", false},
		{"classroom.quiz_requested", "", "create_quiz", true},
		{"classroom.attempt_graded", "t", "write_feedback", false},
	} {
		t.Run(string(tc.kind)+tc.topic, func(t *testing.T) {
			b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, nil)
			t.Cleanup(func() { _ = b.Master.Turns().Stop(context.Background()) })
			ev := classroomEvent("one", tc.kind, tc.topic, tc.final)
			now := time.Now()
			for i := 0; i < 3; i++ {
				if err := b.handleClassroomEvent(context.Background(), ev, now); err != nil {
					t.Fatal(err)
				}
			}
			// A second backend emission while the same operation is pending coalesces.
			ev.ID = "two"
			if err := b.handleClassroomEvent(context.Background(), ev, now); err != nil {
				t.Fatal(err)
			}
			rows := systemRows(b)
			if len(rows) != 1 || !strings.Contains(rows[0].Text, tc.action) {
				t.Fatalf("system turns: %+v", rows)
			}
			if rows[0].ConversationID != ClassroomConversation {
				t.Fatal("separate app conversation")
			}
		})
	}
}
func TestClassroomReadyAllowsFreshRetry(t *testing.T) {
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, nil)
	defer b.Master.Turns().Stop(context.Background())
	now := time.Now()
	first := classroomEvent("request-1", "classroom.quiz_requested", "t", false)
	if err := b.handleClassroomEvent(context.Background(), first, now); err != nil {
		t.Fatal(err)
	}
	_ = b.handleClassroomEvent(context.Background(), classroomEvent("coalesced", "classroom.quiz_requested", "t", false), now)
	if err := b.handleClassroomEvent(context.Background(), classroomEvent("ready", "classroom.quiz_ready", "t", false), now); err != nil {
		t.Fatal(err)
	}
	// Original and coalesced identities stay deduplicated after ready.
	_ = b.handleClassroomEvent(context.Background(), classroomEvent("coalesced", "classroom.quiz_requested", "t", false), now)
	_ = b.handleClassroomEvent(context.Background(), first, now)
	_ = b.handleClassroomEvent(context.Background(), classroomEvent("request-2", "classroom.quiz_requested", "t", false), now)
	if len(systemRows(b)) != 2 {
		t.Fatalf("turns: %+v", systemRows(b))
	}
	if len(b.classroomTurns.pending) != 1 || len(b.classroomTurns.order) != 4 {
		t.Fatal("duplicate accounting")
	}
}
func TestClassroomTriggerBoundsAndExpiry(t *testing.T) {
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, nil)
	defer b.Master.Turns().Stop(context.Background())
	now := time.Now()
	for i := 0; i < 128; i++ {
		if err := b.handleClassroomEvent(context.Background(), classroomEvent(fmt.Sprint(i), "classroom.class_requested", fmt.Sprint(i), false), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.handleClassroomEvent(context.Background(), classroomEvent("overflow", "classroom.class_requested", "more", false), now); err == nil {
		t.Fatal("unbounded pending requests")
	}
	if err := b.handleClassroomEvent(context.Background(), classroomEvent("expired-retry", "classroom.class_requested", "0", false), now.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(b.classroomTurns.pending) != 1 {
		t.Fatal("expired requests retained")
	}
	for i := 128; i < 650; i++ {
		e := classroomEvent(fmt.Sprint(i), "classroom.class_requested", "t", false)
		_ = b.handleClassroomEvent(context.Background(), e, now.Add(11*time.Minute))
		_ = b.handleClassroomEvent(context.Background(), classroomEvent(fmt.Sprintf("ready-%d", i), "classroom.class_ready", "t", false), now.Add(11*time.Minute))
	}
	if len(b.classroomTurns.order) > 512 || len(b.classroomTurns.seen) > 512 {
		t.Fatal("unbounded deduplication")
	}
}
func TestClassroomLiveSubscriptionAndStop(t *testing.T) {
	bus := telemetry.NewBus(nil)
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, bus)
	defer b.Master.Turns().Stop(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.StartClassroomTurns(ctx)
	b.StartClassroomTurns(ctx)
	_, err := bus.Publish("classroom.class_requested", nil, nil, classroomTurnEvent{CourseID: "c", TopicID: "t"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(systemRows(b)) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(systemRows(b)) != 1 {
		t.Fatal("event never queued")
	}
	cancel()
	// Canceled handling cannot append another instruction.
	e := classroomEvent("stopped", "classroom.class_requested", "another", false)
	if err := b.handleClassroomEvent(ctx, e, time.Now()); err == nil {
		t.Fatal("cancellation ignored")
	}
	if len(systemRows(b)) != 1 {
		t.Fatal("turn after cancellation")
	}
}

func TestClassroomFullQueueStillConsumesReadyEvents(t *testing.T) {
	bus := telemetry.NewBus(nil)
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, bus)
	defer b.Master.Turns().Stop(context.Background())
	b.classroomTurns.pending = map[string]time.Time{}
	b.classroomTurns.seen = map[string]bool{}
	for i := 0; i < 128; i++ {
		key := classroomRequestKey(classroomTurnEvent{CourseID: "c", TopicID: fmt.Sprint(i)}, "class")
		b.classroomTurns.pending[key] = time.Now().Add(time.Hour)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.StartClassroomTurns(ctx)
	for _, ev := range []struct {
		kind  types.EventType
		topic string
	}{
		{"classroom.class_requested", "overflow"}, {"classroom.class_ready", "0"}, {"classroom.class_requested", "overflow"},
	} {
		if _, err := bus.Publish(ev.kind, nil, nil, classroomTurnEvent{CourseID: "c", TopicID: ev.topic}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(systemRows(b)) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(systemRows(b)) != 1 {
		t.Fatal("queue saturation blocked the later ready event")
	}
	found := false
	for _, e := range bus.Replay(types.Cursor{}) {
		if e.Type == types.EventNoticePosted && strings.Contains(string(e.Payload), "could not be queued") {
			found = true
		}
	}
	if !found {
		t.Fatal("queue saturation was silent")
	}
}

func TestClassroomInvalidEventsDoNotStartTurns(t *testing.T) {
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, nil)
	defer b.Master.Turns().Stop(context.Background())
	for _, ev := range []types.Event{
		{ID: "invalid", Type: "classroom.class_requested", Payload: json.RawMessage(`{`)},
		{ID: "missing", Type: "classroom.attempt_graded", Payload: json.RawMessage(`{"attempt_id":"a"}`)},
		{ID: "both", Type: "classroom.quiz_requested", Payload: json.RawMessage(`{"course_id":"c","topic_id":"t","final":true}`)},
		{ID: "neither", Type: "classroom.quiz_requested", Payload: json.RawMessage(`{"course_id":"c"}`)},
		classroomEvent("oversized", "classroom.class_requested", strings.Repeat("x", 201), false),
	} {
		if err := b.handleClassroomEvent(context.Background(), ev, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if len(systemRows(b)) != 0 {
		t.Fatal("invalid event started a turn")
	}
}

func TestClassroomFeedbackReadyNeedsOnlyAttemptIdentity(t *testing.T) {
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, nil)
	defer b.Master.Turns().Stop(context.Background())
	now := time.Now()
	e := classroomEvent("graded", "classroom.attempt_graded", "t", false)
	if err := b.handleClassroomEvent(context.Background(), e, now); err != nil {
		t.Fatal(err)
	}
	if len(b.classroomTurns.pending) != 1 {
		t.Fatal("feedback not pending")
	}
	ready := types.Event{ID: "ready", Type: "classroom.feedback_ready", Payload: json.RawMessage(`{"attempt_id":"a"}`)}
	if err := b.handleClassroomEvent(context.Background(), ready, now); err != nil {
		t.Fatal(err)
	}
	if len(b.classroomTurns.pending) != 0 {
		t.Fatal("feedback readiness depended on optional course/topic fields")
	}
}
