package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func contextService() *fake.Agent {
	return &fake.Agent{Courses: map[string]classroom.AgentCourse{"c": {Course: classroom.Course{ID: "c", Title: "Algebra", Progress: 49, Units: []classroom.Unit{{Topics: []classroom.Topic{{ID: "t", Title: "Equations", Status: "in_progress", Class: &classroom.Class{Content: "PRIVATE_LESSON"}}}}}}, Quizzes: []classroom.Quiz{{Questions: []classroom.Question{{Prompt: "PRIVATE_QUESTION", Correct: []int{1}}}}}}}}
}
func TestAppContextNoteOnlyInCurrentTurnTail(t *testing.T) {
	b, s := assistantTestWiring(t, false, nil)
	s.ClassroomAgent = contextService()
	a := &assistantTurns{backend: b, services: s}
	_, _, err := b.Master.ReceiveWithContext(context.Background(), "client", "first", "conv", "Explain this", nil, &api.AppContext{App: "classroom", CourseID: "c", TopicID: "t"})
	if err != nil {
		t.Fatal(err)
	}
	awaitAssistant(t, b, "conv", 1)
	before := b.Master.TurnHistory("conv", 0)
	plain := a.snapshot(context.Background(), "conv", master.Normalized{Text: "Explain this"})
	contextual := a.snapshot(context.Background(), "conv", master.Normalized{Text: "Explain this", Context: &api.AppContext{App: "classroom", CourseID: "c", TopicID: "t"}})
	if plain.AgentProfile != contextual.AgentProfile || !reflect.DeepEqual(plain.Task, contextual.Task) {
		t.Fatal("app context changed stable prefix")
	}
	if len(contextual.Activity) != len(plain.Activity)+1 {
		t.Fatal("note not appended")
	}
	note := contextual.Activity[len(contextual.Activity)-1]
	for _, v := range []string{"Algebra", "Equations", "in_progress", "49"} {
		if !strings.Contains(note.Content, v) {
			t.Fatalf("missing %s: %s", v, note.Content)
		}
	}
	if note.Role != providers.RoleSystem || strings.Contains(note.Content, "PRIVATE_") {
		t.Fatalf("bad note: %+v", note)
	}
	if !reflect.DeepEqual(before, b.Master.TurnHistory("conv", 0)) {
		t.Fatal("history rewritten")
	}
	later := a.snapshot(context.Background(), "conv", master.Normalized{Text: "Another question"})
	for _, event := range later.Activity {
		if strings.Contains(event.Content, "Current app view") {
			t.Fatal("context leaked into later turn")
		}
	}
	if got := a.appContextNote(context.Background(), &api.AppContext{App: "future"}); got != "" {
		t.Fatal("unknown app resolved as classroom")
	}
	c := s.ClassroomAgent.(*fake.Agent).Courses["c"]
	c.Title = strings.Repeat("X", 10000)
	c.Units[0].Topics[0].Title = strings.Repeat("Y", 10000)
	s.ClassroomAgent.(*fake.Agent).Courses["c"] = c
	if len(a.appContextNote(context.Background(), &api.AppContext{App: "classroom", CourseID: "c", TopicID: "t"})) > 1200 {
		t.Fatal("unbounded title context")
	}
}
func TestConversationRPCPreservesContextAndAttachments(t *testing.T) {
	var mu sync.Mutex
	notes := []string{}
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		found := ""
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "Current app view") {
				found += m.Content
			}
		}
		mu.Lock()
		notes = append(notes, found)
		mu.Unlock()
		return textReply("Answer"), nil
	})
	s.ClassroomAgent = contextService()
	server := httptest.NewServer(gateway.New(gateway.Config{}, b, gateway.NewMemoryBlobs(), b.Bus))
	defer server.Close()
	call := func(id string, app *api.AppContext) api.Response {
		t.Helper()
		req, err := api.NewRequest(id, api.MethodConversationSend, api.ConversationSendParams{ConversationID: ClassroomConversation, ClientMsgID: id, Text: "Explain this", Attachments: []string{"document"}, Context: app})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(req)
		res, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out api.Response
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := call("one", &api.AppContext{App: "classroom", CourseID: "c", TopicID: "t"}); out.Error != nil {
		t.Fatal(out.Error)
	}
	awaitAssistant(t, b, ClassroomConversation, 1)
	rows := b.Master.TurnHistory(ClassroomConversation, 0)
	if rows[0].Context == nil || rows[0].Context.CourseID != "c" || len(rows[0].Attachments) != 1 {
		t.Fatalf("metadata lost: %+v", rows[0])
	}
	if out := call("two", nil); out.Error != nil {
		t.Fatal(out.Error)
	}
	awaitAssistant(t, b, ClassroomConversation, 2)
	mu.Lock()
	defer mu.Unlock()
	if len(notes) != 2 || !strings.Contains(notes[0], "Algebra") || notes[1] != "" {
		t.Fatalf("model notes: %+v", notes)
	}
	if out := call("invalid", &api.AppContext{App: ""}); out.Error == nil || out.Error.Code != api.CodeInvalidParams {
		t.Fatalf("invalid context accepted: %+v", out)
	}
}
func TestAppContextUnavailableAndClone(t *testing.T) {
	a := &assistantTurns{services: &Services{}}
	if note := a.appContextNote(context.Background(), &api.AppContext{App: "classroom", CourseID: "missing"}); !strings.Contains(note, "unavailable") {
		t.Fatal(note)
	}
	m := master.NewMemStore()
	app := &api.AppContext{App: "classroom", CourseID: "original"}
	_, err := m.SaveMessage(context.Background(), "conv", master.Normalized{Context: app})
	if err != nil {
		t.Fatal(err)
	}
	app.CourseID = "mutated"
	rows := m.History("conv", 0)
	if rows[0].Context.CourseID != "original" {
		t.Fatal("stored context aliased")
	}
	rows[0].Context.CourseID = "again"
	if m.History("conv", 0)[0].Context.CourseID != "original" {
		t.Fatal("history context aliased")
	}
}

func TestAppContextClassroomHome(t *testing.T) {
	a := &assistantTurns{services: &Services{}}
	note := a.appContextNote(context.Background(), &api.AppContext{App: "classroom"})
	if !strings.Contains(note, "classroom home") {
		t.Fatal(note)
	}
}
