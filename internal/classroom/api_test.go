package classroom

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestAPIAndEvents(t *testing.T) {
	bus := telemetry.NewBus(nil)
	s := NewService(nil, bus)
	c := courseForTest(t, s, 1)
	id := c.Units[0].Topics[0].ID
	call := func(method string, p any) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		result, err := s.Op(testContext, method, raw)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		out, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	list := call(api.MethodClassroomList, struct{}{})
	if !strings.Contains(string(list), c.ID) || !strings.Contains(string(list), `"total_topics":1`) {
		t.Fatal(string(list))
	}
	call(api.MethodClassroomGet, api.ClassroomCourseParams{CourseID: c.ID})
	opened := call(api.MethodClassroomOpenTopic, api.ClassroomTopicParams{CourseID: c.ID, TopicID: id})
	if !strings.Contains(string(opened), "in_progress") {
		t.Fatal(string(opened))
	}
	if _, err := s.WriteClass(testContext, c.ID, id, Class{Format: "chat", Content: "Lesson"}); err != nil {
		t.Fatal(err)
	}
	before := bus.Len()
	call(api.MethodClassroomOpenTopic, api.ClassroomTopicParams{CourseID: c.ID, TopicID: id})
	if bus.Len() != before {
		t.Fatal("existing lesson requested again")
	}
	request := api.ClassroomRequestQuizParams{CourseID: c.ID, TopicID: id}
	call(api.MethodClassroomRequestQuiz, request)
	q, err := s.CreateQuiz(testContext, c.ID, id, false, questionsForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	before = bus.Len()
	reused := call(api.MethodClassroomRequestQuiz, request)
	if bus.Len() != before || !strings.Contains(string(reused), q.ID) {
		t.Fatal(string(reused))
	}
	quiz := call(api.MethodClassroomGetQuiz, api.ClassroomGetQuizParams{QuizID: q.ID})
	for _, field := range []string{`"correct"`, `"pairs"`, `"explanation"`} {
		if strings.Contains(string(quiz), field) || strings.Contains(string(reused), field) {
			t.Fatal("answer key leaked", string(quiz), string(reused))
		}
	}
	answers := correctAnswers(q)
	clientAnswers := []api.ClassroomAnswer{}
	for _, a := range answers {
		clientAnswers = append(clientAnswers, api.ClassroomAnswer(a))
	}
	submit := call(api.MethodClassroomSubmit, api.ClassroomSubmitParams{QuizID: q.ID, Answers: clientAnswers})
	var result api.ClassroomSubmitResult
	if err = json.Unmarshal(submit, &result); err != nil || !result.Passed {
		t.Fatal(string(submit), err)
	}
	if _, err = s.WriteFeedback(testContext, result.Attempt, "Well done"); err != nil {
		t.Fatal(err)
	}
	history := call(api.MethodClassroomAttempts, api.ClassroomAttemptsParams{CourseID: c.ID, TopicID: id})
	if !strings.Contains(string(history), "Well done") || strings.Contains(string(history), `"answers"`) {
		t.Fatal(string(history))
	}
	call(api.MethodClassroomUpdateSyllabus, api.ClassroomUpdateSyllabusParams{CourseID: c.ID, Units: []api.ClassroomSyllabusUnit{{ID: c.Units[0].ID, Title: "Renamed", Topics: []api.ClassroomSyllabusTopic{{ID: id, Title: "Renamed topic"}}}}})
	call(api.MethodClassroomRequestQuiz, api.ClassroomRequestQuizParams{CourseID: c.ID, Final: true})
	exam, err := s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(10))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(testContext, exam.ID, correctAnswers(exam)); err != nil {
		t.Fatal(err)
	}
	call(api.MethodClassroomDelete, api.ClassroomCourseParams{CourseID: c.ID})
	seen := map[types.EventType]int{}
	for _, event := range bus.Replay(types.Cursor{}) {
		seen[event.Type]++
		var p map[string]any
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p["course_id"] != c.ID {
			t.Fatal(string(event.Payload))
		}
		if strings.Contains(string(event.Payload), `"pairs"`) || strings.Contains(string(event.Payload), `"answers"`) {
			t.Fatal("answer fields in event", string(event.Payload))
		}
		if event.Type == "classroom.quiz_ready" && (strings.Contains(string(event.Payload), `"correct"`) || strings.Contains(string(event.Payload), `"explanation"`)) {
			t.Fatal("answer key in quiz event", string(event.Payload))
		}
	}
	for _, name := range []string{"course_created", "course_updated", "class_requested", "class_ready", "quiz_requested", "quiz_ready", "attempt_graded", "feedback_ready", "topic_completed", "course_completed"} {
		if seen[types.EventType("classroom."+name)] == 0 {
			t.Error("missing event", name)
		}
	}
	if seen["classroom.topic_completed"] != 1 || seen["classroom.course_completed"] != 1 {
		t.Fatal(seen)
	}
}

func TestAPIValidationAndUnavailable(t *testing.T) {
	s := NewService(nil, nil)
	for _, raw := range []string{`null`, `[]`, `1`, `{"course_id":`, `{"extra":true}`} {
		_, err := s.Op(testContext, api.MethodClassroomGet, json.RawMessage(raw))
		requireCode(t, err, api.CodeInvalidParams)
	}
	for _, method := range []string{api.MethodClassroomGet, api.MethodClassroomDelete, api.MethodClassroomUpdateSyllabus, api.MethodClassroomOpenTopic, api.MethodClassroomRequestQuiz, api.MethodClassroomGetQuiz, api.MethodClassroomSubmit, api.MethodClassroomAttempts} {
		_, err := s.Op(testContext, method, json.RawMessage(`{}`))
		requireCode(t, err, api.CodeInvalidParams)
	}
	_, err := s.Op(testContext, "classroom.unknown", nil)
	requireCode(t, err, api.CodeMethodNotFound)
	_, err = (*Service)(nil).Op(testContext, api.MethodClassroomList, nil)
	requireCode(t, err, api.CodeInternal)
	_, err = s.Op(testContext, api.MethodClassroomGet, json.RawMessage(`{"course_id":"missing"}`))
	requireCode(t, err, api.CodeUnknownTarget)
	_, err = s.Op(testContext, api.MethodClassroomUpdateSyllabus, json.RawMessage(`{"course_id":"missing","units":[{"title":"Unit","topics":[{"title":"Topic","status":"completed"}]}]}`))
	requireCode(t, err, api.CodeInvalidParams)
	_, err = s.Op(testContext, api.MethodClassroomList, json.RawMessage(`{"data":"`+strings.Repeat("x", 2<<20)+`"}`))
	requireCode(t, err, api.CodeInvalidParams)
}
