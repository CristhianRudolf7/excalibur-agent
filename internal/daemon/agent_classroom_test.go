package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestPersonalClassroomRPCOriginAndFailedRequest(t *testing.T) {
	ctx := context.Background()
	b, r := personalBackend(t, nil)
	first := r.Favorite()
	selected, err := r.Create(ctx, agents.CreateParams{Name: "Tutor"})
	if err != nil {
		t.Fatal(err)
	}
	courseService := classroom.NewService(nil, b.Bus)
	b.Services.Classroom = courseService
	course, err := courseService.CreateCourse(ctx, classroom.Course{Title: "Algebra", Units: []classroom.Unit{{Title: "First", Topics: []classroom.Topic{{Title: "Equations"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	topic := course.Units[0].Topics[0]
	params, _ := json.Marshal(api.ClassroomTopicParams{CourseID: course.ID, TopicID: topic.ID, ConversationID: selected.ConversationID})
	if _, err = b.Passthrough(ctx, api.MethodClassroomOpenTopic, params); err != nil {
		t.Fatal(err)
	}
	for _, ev := range b.Bus.Replay(types.Cursor{}) {
		if err = b.handleClassroomEvent(ctx, ev, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	awaitAssistant(t, b, selected.ConversationID, 1)
	if len(b.Master.History(first.ConversationID, 0)) != 0 {
		t.Fatal("classroom ignored selected chat")
	}
	if len(b.classroomTurns.origins) != 0 {
		t.Fatal("origin not consumed")
	}
	bad, _ := json.Marshal(api.ClassroomTopicParams{CourseID: "missing", TopicID: "missing", ConversationID: selected.ConversationID})
	if _, err = b.Passthrough(ctx, api.MethodClassroomOpenTopic, bad); err == nil {
		t.Fatal("missing course accepted")
	}
	if len(b.classroomTurns.origins) != 0 {
		t.Fatal("failed request left stale origin")
	}
}

func TestPersonalClassroomReceiptSurvivesBusSequenceRestart(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "classroom-receipts.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, _ := personalBackend(t, db)
	event := types.Event{ID: "evt_1", At: time.Now().UTC(), Type: "classroom.class_requested", Payload: json.RawMessage(`{"course_id":"course","topic_id":"first"}`)}
	if err = first.handleClassroomEvent(ctx, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	conv := first.SystemConversation("")
	awaitAssistant(t, first, conv, 1)
	// A new daemon's bus reuses evt_1. Its later timestamp makes this a new
	// request even though the old receipt is still in the database.
	second, _ := personalBackend(t, db)
	event.At = event.At.Add(time.Second)
	event.Payload = json.RawMessage(`{"course_id":"course","topic_id":"second"}`)
	if err = second.handleClassroomEvent(ctx, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	history := awaitAssistant(t, second, conv, 2)
	if len(history) != 4 || !strings.Contains(history[2].Text, "second") {
		t.Fatal(history)
	}
}
