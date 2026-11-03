package classroom_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func fixture() *fake.Agent {
	return &fake.Agent{Courses: map[string]classroom.AgentCourse{"c": {Course: classroom.Course{ID: "c", Title: "Algebra", FinalExam: classroom.ExamState{Unlocked: true}, Units: []classroom.Unit{{ID: "u", Topics: []classroom.Topic{{ID: "t", Title: "Equations", Status: "completed"}}}}}}}}
}
func question() classroom.Question {
	return classroom.Question{Kind: "single", Prompt: "Which?", Options: []string{"A", "B"}, Correct: []int{0}, Explanation: "Reason"}
}
func invoke(t *testing.T, s *fake.Agent, a any) tools.Result {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	r, err := (&classroom.Tool{Service: s}).Invoke(context.Background(), tools.Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestToolActionRouting(t *testing.T) {
	cases := []struct {
		action string
		args   map[string]any
		calls  string
	}{
		{"create_course", map[string]any{"title": "Math", "goal": "Solve", "level": "beginner", "language": "en", "units": []any{map[string]any{"topics": []any{map[string]any{"title": "Equations", "summary": "Solve equations"}}}}}, "create_course"},
		{"update_syllabus", map[string]any{"course_id": "c", "units": []any{map[string]any{"id": "u", "topics": []any{map[string]any{"id": "t", "title": "Equations", "summary": "Solve"}}}}}, "update_syllabus"},
		{"get_course", map[string]any{"course_id": "c"}, "get_course"},
		{"write_class", map[string]any{"course_id": "c", "topic_id": "t", "format": "chat", "content": "# Lesson"}, "write_class"},
		{"create_quiz", map[string]any{"course_id": "c", "topic_id": "t", "questions": []classroom.Question{question()}}, "get_course,create_quiz"},
		{"write_feedback", map[string]any{"attempt_id": "a", "feedback": "Review the concept."}, "write_feedback"},
		{"list_courses", map[string]any{}, "list_courses"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			s := fixture()
			tc.args["action"] = tc.action
			r := invoke(t, s, tc.args)
			if r.Status != tools.StatusOK {
				t.Fatalf("%+v", r)
			}
			if got := strings.Join(s.SnapshotCalls(), ","); got != tc.calls {
				t.Fatalf("calls %s", got)
			}
			if tc.action == "create_quiz" && strings.Contains(r.Output, "correct") {
				t.Fatal("mutation summary includes answers")
			}
		})
	}
}
func TestToolRejectsInvalidArgumentsBeforeService(t *testing.T) {
	cases := []string{
		`null`, `{}`, `{"action":"unknown"}`, `{"action":"list_courses","extra":1}`,
		`{"action":"create_course","title":"Math"}`, `{"action":"get_course","course_id":" "}`,
		`{"action":"update_syllabus","course_id":"c","units":[]}`,
		`{"action":"update_syllabus","course_id":"c","units":[{"topics":[]}]}`,
		`{"action":"update_syllabus","course_id":"c","units":[{"topics":[{"title":"T","summary":"S","status":"completed"}]}]}`,
		`{"action":"update_syllabus","course_id":"c","units":[{"topics":[{"id":"x","title":"T","summary":"S"},{"id":"x","title":"T","summary":"S"}]}]}`,
		`{"action":"write_class","course_id":"c","topic_id":"t","format":"video","content":"lesson"}`,
		`{"action":"write_class","course_id":"c","topic_id":"t","format":"whiteboard","content":"lesson"}`,
		`{"action":"write_class","course_id":"c","topic_id":"t","format":"chat","content":" "}`,
		`{"action":"write_feedback","attempt_id":"a","feedback":" "}`,
		`{"action":"create_quiz","course_id":"c","final":true,"topic_id":"t","questions":[]}`,
		`{"action":"create_quiz","course_id":"c","questions":[]}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			s := fixture()
			r, err := (&classroom.Tool{Service: s}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(raw)})
			if err != nil || r.Status != tools.StatusInputError {
				t.Fatalf("%+v %v", r, err)
			}
			if len(s.SnapshotCalls()) != 0 {
				t.Fatal("invalid input reached service")
			}
		})
	}
}
func TestQuestionValidation(t *testing.T) {
	cases := []struct {
		name string
		edit func(*classroom.Question)
		ok   bool
	}{
		{"single", func(q *classroom.Question) {}, true},
		{"multiple", func(q *classroom.Question) { q.Kind = "multiple"; q.Correct = []int{0, 1} }, true},
		{"match", func(q *classroom.Question) {
			*q = classroom.Question{Kind: "match", Prompt: "Pair", Explanation: "Reason", Left: []string{"A", "B"}, Right: []string{"C", "D"}, Pairs: []int{1, 0}}
		}, true},
		{"open", func(q *classroom.Question) { q.Kind = "text" }, false},
		{"no explanation", func(q *classroom.Question) { q.Explanation = "" }, false},
		{"few options", func(q *classroom.Question) { q.Options = q.Options[:1] }, false},
		{"empty option", func(q *classroom.Question) { q.Options[0] = " " }, false},
		{"no answers", func(q *classroom.Question) { q.Correct = nil }, false},
		{"negative", func(q *classroom.Question) { q.Correct = []int{-1} }, false},
		{"out of range", func(q *classroom.Question) { q.Correct = []int{2} }, false},
		{"single many", func(q *classroom.Question) { q.Correct = []int{0, 1} }, false},
		{"duplicate", func(q *classroom.Question) { q.Kind = "multiple"; q.Correct = []int{0, 0} }, false},
		{"mixed fields", func(q *classroom.Question) { q.Left = []string{"A"} }, false},
		{"bad permutation", func(q *classroom.Question) {
			*q = classroom.Question{Kind: "match", Prompt: "Pair", Explanation: "Reason", Left: []string{"A", "B"}, Right: []string{"C", "D"}, Pairs: []int{0, 0}}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := question()
			tc.edit(&q)
			s := fixture()
			r := invoke(t, s, map[string]any{"action": "create_quiz", "course_id": "c", "topic_id": "t", "questions": []classroom.Question{q}})
			if (r.Status == tools.StatusOK) != tc.ok {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestQuizFinalSizeAndTopicValidation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		n, largest    int
		locked, topic bool
		ok            bool
	}{
		{"minimum", 10, 0, false, false, true}, {"too short", 9, 0, false, false, false}, {"double largest", 14, 7, false, false, true}, {"less than double", 13, 7, false, false, false}, {"locked", 10, 0, true, false, false}, {"unknown topic", 1, 0, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			c := s.Courses["c"]
			c.FinalExam.Unlocked = !tc.locked
			c.Quizzes = []classroom.Quiz{{Questions: make([]classroom.Question, tc.largest)}}
			s.Courses["c"] = c
			qs := make([]classroom.Question, tc.n)
			for i := range qs {
				qs[i] = question()
			}
			a := map[string]any{"action": "create_quiz", "course_id": "c", "final": !tc.topic, "questions": qs}
			if tc.topic {
				a["topic_id"] = "missing"
			}
			r := invoke(t, s, a)
			if (r.Status == tools.StatusOK) != tc.ok {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestToolFailuresAndCancellation(t *testing.T) {
	s := fixture()
	s.Err = errors.New("storage failed")
	r := invoke(t, s, map[string]any{"action": "list_courses"})
	if r.Status != tools.StatusToolError {
		t.Fatal(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := (&classroom.Tool{Service: s}).Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"action":"list_courses"}`)})
	if err != nil || r.Status != tools.StatusCanceled {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = (&classroom.Tool{}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"list_courses"}`)})
	if err != nil || r.Status != tools.StatusToolError {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestToolPreservesActionPayloads(t *testing.T) {
	s := fixture()
	c := s.Courses["c"]
	c.Attempts = []classroom.Attempt{{ID: "a"}}
	s.Courses["c"] = c
	for _, format := range []string{"chat", "whiteboard"} {
		args := map[string]any{"action": "write_class", "course_id": "c", "topic_id": "t", "format": format, "content": "# Full lesson"}
		if format == "whiteboard" {
			args["whiteboard_id"] = "board"
		}
		if r := invoke(t, s, args); r.Status != tools.StatusOK {
			t.Fatal(r)
		}
		got, err := s.GetCourse(context.Background(), "c")
		if err != nil {
			t.Fatal(err)
		}
		lesson := got.Units[0].Topics[0].Class
		if lesson == nil || lesson.Content != "# Full lesson" || lesson.Format != format || format == "whiteboard" && lesson.WhiteboardID != "board" {
			t.Fatalf("wrong lesson: %+v", lesson)
		}
	}
	if r := invoke(t, s, map[string]any{"action": "create_quiz", "course_id": "c", "topic_id": "t", "questions": []classroom.Question{question()}}); r.Status != tools.StatusOK {
		t.Fatal(r)
	}
	got, _ := s.GetCourse(context.Background(), "c")
	q := got.Quizzes[0]
	if q.CourseID != "c" || q.TopicID != "t" || q.Questions[0].Correct[0] != 0 || q.Questions[0].Explanation != "Reason" {
		t.Fatalf("wrong quiz: %+v", q)
	}
	if r := invoke(t, s, map[string]any{"action": "write_feedback", "attempt_id": "a", "feedback": "Targeted hint"}); r.Status != tools.StatusOK {
		t.Fatal(r)
	}
	got, _ = s.GetCourse(context.Background(), "c")
	if got.Attempts[0].Feedback != "Targeted hint" {
		t.Fatal("feedback lost")
	}
	// The trusted read includes the private quiz history needed for fresh retries.
	r := invoke(t, s, map[string]any{"action": "get_course", "course_id": "c"})
	if !strings.Contains(r.Output, `"correct":[0]`) {
		t.Fatal("trusted answers missing")
	}
}
