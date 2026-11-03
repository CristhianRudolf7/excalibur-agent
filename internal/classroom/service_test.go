package classroom

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

var testContext = context.Background()

func requireCode(t *testing.T, err error, want int) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("error %v; want code %d", err, want)
	}
}
func courseForTest(t *testing.T, s *Service, n int) Course {
	t.Helper()
	topics := []Topic{}
	for i := 0; i < n; i++ {
		topics = append(topics, Topic{Title: "Topic"})
	}
	c, err := s.CreateCourse(testContext, Course{Title: "Go", Goal: "Build programs", Level: "beginner", Language: "en", Units: []Unit{{Topics: topics}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func questionsForTest(n int) []Question {
	out := []Question{}
	for i := 0; i < n; i++ {
		out = append(out, Question{Kind: KindSingle, Prompt: "Which?", Options: []string{"a", "b"}, Correct: []int{1}, Explanation: "Review the rule"})
	}
	return out
}
func correctAnswers(q Quiz) []Answer {
	out := []Answer{}
	for _, v := range q.Questions {
		out = append(out, Answer{QuestionID: v.ID, Selected: append([]int(nil), v.Correct...), Pairs: append([]int(nil), v.Pairs...)})
	}
	return out
}
func passTopic(t *testing.T, s *Service, c Course, index, n int) Quiz {
	t.Helper()
	q, err := s.CreateQuiz(testContext, c.ID, c.Units[0].Topics[index].ID, false, questionsForTest(n))
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Submit(testContext, q.ID, correctAnswers(q))
	if err != nil || !res.Passed {
		t.Fatalf("pass: %+v %v", res, err)
	}
	return q
}
func TestSyllabusProgressUnlockAndCompletion(t *testing.T) {
	s := NewService(nil, nil)
	c := courseForTest(t, s, 3)
	if c.Progress != 0 || c.FinalExam.Unlocked {
		t.Fatal(c)
	}
	_, err := s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(10))
	requireCode(t, err, api.CodeConflict)
	_, err = s.RequestQuiz(testContext, c.ID, "", true)
	requireCode(t, err, api.CodeConflict)
	q := passTopic(t, s, c, 0, 6)
	c, _ = s.GetCourse(testContext, c.ID)
	if c.Progress != 33 || c.FinalExam.Unlocked {
		t.Fatal(c)
	}
	opened, err := s.OpenTopic(testContext, c.ID, c.Units[0].Topics[0].ID)
	if err != nil || opened.Topic.Status != "completed" {
		t.Fatal(opened, err)
	}
	completedAt := c.Units[0].Topics[0].CompletedAt
	// Retrying another quiz for an already completed topic preserves completion time.
	q2, err := s.CreateQuiz(testContext, c.ID, q.TopicID, false, questionsForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(testContext, q2.ID, correctAnswers(q2)); err != nil {
		t.Fatal(err)
	}
	c, _ = s.GetCourse(testContext, c.ID)
	if !c.Units[0].Topics[0].CompletedAt.Equal(*completedAt) {
		t.Fatal("completion time changed")
	}
	units := owned(c.Units)
	units[0].Topics[0].Title = "Renamed"
	units[0].Topics[1].Status = "completed"
	units[0].Topics[1].Class = &Class{Content: "injected"}
	units[0].Topics[0], units[0].Topics[2] = units[0].Topics[2], units[0].Topics[0]
	edited, err := s.UpdateSyllabus(testContext, c.ID, units)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Units[0].Topics[2].ID != q.TopicID || edited.Units[0].Topics[1].Status != "not_started" || edited.Units[0].Topics[1].Class != nil || edited.Progress != 33 {
		t.Fatal(edited)
	}
	// Removing the completed topic must reduce progress and retain historical attempts.
	units = owned(edited.Units)
	units[0].Topics = units[0].Topics[:2]
	edited, err = s.UpdateSyllabus(testContext, c.ID, units)
	if err != nil || edited.Progress != 0 {
		t.Fatal(edited, err)
	}
	attempts, err := s.Attempts(testContext, c.ID, "")
	if err != nil || len(attempts) != 2 {
		t.Fatal(attempts, err)
	}
	// A newly appended topic receives its own ID and remains incomplete.
	units[0].Topics = append(units[0].Topics, Topic{Title: "New", Status: "completed"})
	edited, err = s.UpdateSyllabus(testContext, c.ID, units)
	if err != nil || edited.Units[0].Topics[2].ID == "" || edited.Units[0].Topics[2].Status != "not_started" {
		t.Fatal(edited, err)
	}
	for i := range edited.Units[0].Topics {
		passTopic(t, s, edited, i, 6)
	}
	c, _ = s.GetCourse(testContext, c.ID)
	if c.Progress != 99 || !c.FinalExam.Unlocked {
		t.Fatal(c)
	}
	_, err = s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(11))
	requireCode(t, err, api.CodeInvalidParams)
	exam, err := s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(12))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Submit(testContext, exam.ID, correctAnswers(exam))
	if err != nil || !result.Passed {
		t.Fatal(result, err)
	}
	c, _ = s.GetCourse(testContext, c.ID)
	if c.Progress != 100 || !c.FinalExam.Passed || c.CompletedAt == nil {
		t.Fatal(c)
	}
	units = owned(c.Units)
	units[0].Topics = append(units[0].Topics, Topic{Title: "Extra"})
	c, err = s.UpdateSyllabus(testContext, c.ID, units)
	if err != nil || c.Progress != 74 || c.FinalExam.Unlocked || c.FinalExam.Passed || c.CompletedAt != nil {
		t.Fatal(c, err)
	}
	_, err = s.Submit(testContext, exam.ID, correctAnswers(exam))
	requireCode(t, err, api.CodeConflict)
	summary, err := s.ListCourses(testContext)
	if err != nil || len(summary) != 1 || summary[0].TotalTopics != 4 || summary[0].CompletedTopics != 3 {
		t.Fatal(summary, err)
	}
	if err = s.DeleteCourse(testContext, c.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetCourse(testContext, c.ID)
	requireCode(t, err, api.CodeUnknownTarget)
}

func TestGradingEveryKindAndQuizReuse(t *testing.T) {
	s := NewService(nil, nil)
	c := courseForTest(t, s, 1)
	id := c.Units[0].Topics[0].ID
	questions := append(questionsForTest(1), Question{Kind: KindMultiple, Prompt: "Which two?", Options: []string{"a", "b", "c"}, Correct: []int{0, 2}}, Question{Kind: KindMatch, Prompt: "Match", Left: []string{"a", "b"}, Right: []string{"B", "A"}, Pairs: []int{1, 0}})
	for _, kind := range []string{"single", "multiple", "match", "missing"} {
		t.Run(kind, func(t *testing.T) {
			request, err := s.RequestQuiz(testContext, c.ID, id, false)
			if err != nil || !request.Requested {
				t.Fatal(request, err)
			}
			q, err := s.CreateQuiz(testContext, c.ID, id, false, questions)
			if err != nil {
				t.Fatal(err)
			}
			request, err = s.RequestQuiz(testContext, c.ID, id, false)
			if err != nil || request.Requested || request.Quiz.ID != q.ID {
				t.Fatal(request, err)
			}
			answers := correctAnswers(q)
			switch kind {
			case "single":
				answers[0].Selected = []int{0}
			case "multiple":
				answers[1].Selected = []int{0}
			case "match":
				answers[2].Pairs = []int{0, 1}
			case "missing":
				answers = answers[:2]
			}
			result, err := s.Submit(testContext, q.ID, answers)
			if err != nil || result.Passed {
				t.Fatal(result, err)
			}
			wrong := 0
			for _, r := range result.Results {
				if !r.Correct {
					wrong++
				}
			}
			if wrong != 1 {
				t.Fatal(result)
			}
			_, err = s.Submit(testContext, q.ID, correctAnswers(q))
			requireCode(t, err, api.CodeConflict)
			if _, err = s.WriteFeedback(testContext, result.Attempt, "Review your mistake"); err != nil {
				t.Fatal(err)
			}
		})
	}
	q, err := s.CreateQuiz(testContext, c.ID, id, false, questions)
	if err != nil {
		t.Fatal(err)
	}
	answers := correctAnswers(q)
	answers[1].Selected = []int{2, 0} // Order does not affect a multiple-choice set.
	result, err := s.Submit(testContext, q.ID, answers)
	if err != nil || !result.Passed {
		t.Fatal(result, err)
	}
	history, err := s.Attempts(testContext, c.ID, id)
	if err != nil || len(history) != 5 || history[0].Feedback == "" {
		t.Fatal(history, err)
	}
	c, _ = s.GetCourse(testContext, c.ID)
	if c.Progress != 99 || !c.FinalExam.Unlocked {
		t.Fatal(c)
	}
}

func TestQuestionValidation(t *testing.T) {
	base := questionsForTest(1)[0]
	cases := map[string]func(*Question){
		"kind": func(q *Question) { q.Kind = "essay" }, "prompt": func(q *Question) { q.Prompt = " " },
		"one option": func(q *Question) { q.Options = []string{"a"} }, "nine options": func(q *Question) { q.Options = make([]string, 9) }, "empty option": func(q *Question) { q.Options[0] = "" },
		"single none": func(q *Question) { q.Correct = nil }, "single two": func(q *Question) { q.Correct = []int{0, 1} }, "negative": func(q *Question) { q.Correct = []int{-1} }, "out of range": func(q *Question) { q.Correct = []int{2} },
		"multiple none": func(q *Question) { q.Kind = KindMultiple; q.Correct = nil }, "multiple duplicate": func(q *Question) { q.Kind = KindMultiple; q.Correct = []int{0, 0} },
		"choice matching fields": func(q *Question) { q.Pairs = []int{0} },
		"match unequal": func(q *Question) {
			*q = Question{Kind: KindMatch, Prompt: "Match", Left: []string{"a", "b"}, Right: []string{"a"}, Pairs: []int{0, 1}}
		},
		"match duplicate": func(q *Question) {
			*q = Question{Kind: KindMatch, Prompt: "Match", Left: []string{"a", "b"}, Right: []string{"a", "b"}, Pairs: []int{0, 0}}
		},
		"match range": func(q *Question) {
			*q = Question{Kind: KindMatch, Prompt: "Match", Left: []string{"a", "b"}, Right: []string{"a", "b"}, Pairs: []int{0, 2}}
		},
		"match missing pair": func(q *Question) {
			*q = Question{Kind: KindMatch, Prompt: "Match", Left: []string{"a", "b"}, Right: []string{"a", "b"}, Pairs: []int{0}}
		},
		"match empty": func(q *Question) {
			*q = Question{Kind: KindMatch, Prompt: "Match", Left: []string{"", "b"}, Right: []string{"a", "b"}, Pairs: []int{0, 1}}
		},
		"match choices": func(q *Question) {
			*q = Question{Kind: KindMatch, Prompt: "Match", Left: []string{"a", "b"}, Right: []string{"a", "b"}, Pairs: []int{0, 1}, Correct: []int{0}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			q := owned(base)
			change(&q)
			requireCode(t, validateQuestions([]Question{q}), api.CodeInvalidParams)
		})
	}
	requireCode(t, validateQuestions(nil), api.CodeInvalidParams)
	base.ID = "same"
	requireCode(t, validateQuestions([]Question{base, base}), api.CodeInvalidParams)
}

func TestSyllabusAndClassValidation(t *testing.T) {
	s := NewService(nil, nil)
	c := courseForTest(t, s, 2)
	id := c.Units[0].Topics[0].ID
	for _, input := range []Course{{Title: ""}, {Title: "Course"}} {
		_, err := s.CreateCourse(testContext, input)
		requireCode(t, err, api.CodeInvalidParams)
	}
	for _, change := range []func([]Unit) []Unit{
		func(u []Unit) []Unit { return nil },
		func(u []Unit) []Unit { u[0].Topics[0].Title = ""; return u }, func(u []Unit) []Unit { u[0].Topics[1].ID = id; return u }, func(u []Unit) []Unit { return append(u, u[0]) },
	} {
		_, err := s.UpdateSyllabus(testContext, c.ID, change(owned(c.Units)))
		requireCode(t, err, api.CodeInvalidParams)
	}
	for _, lesson := range []Class{{Format: "other", Content: "x"}, {Format: "chat"}, {Format: "whiteboard", Content: "x"}} {
		_, err := s.WriteClass(testContext, c.ID, id, lesson)
		requireCode(t, err, api.CodeInvalidParams)
	}
	opened, err := s.OpenTopic(testContext, c.ID, id)
	if err != nil || opened.Topic.Status != "in_progress" || opened.Class != nil {
		t.Fatal(opened, err)
	}
	for _, lesson := range []Class{{Format: "chat", Content: "First"}, {Format: "whiteboard", Content: "Updated", WhiteboardID: "board"}} {
		if _, err = s.WriteClass(testContext, c.ID, id, lesson); err != nil {
			t.Fatal(err)
		}
	}
	opened, err = s.OpenTopic(testContext, c.ID, id)
	if err != nil || opened.Class.Content != "Updated" || opened.Class.WhiteboardID != "board" {
		t.Fatal(opened, err)
	}
	_, err = s.OpenTopic(testContext, c.ID, "missing")
	requireCode(t, err, api.CodeUnknownTarget)
	_, err = s.CreateQuiz(testContext, c.ID, "", false, questionsForTest(1))
	requireCode(t, err, api.CodeInvalidParams)
	_, err = s.CreateQuiz(testContext, c.ID, id, true, questionsForTest(10))
	requireCode(t, err, api.CodeInvalidParams)
	_, err = s.CreateQuiz(testContext, c.ID, "missing", false, questionsForTest(1))
	requireCode(t, err, api.CodeUnknownTarget)
	_, err = s.WriteFeedback(testContext, "missing", "x")
	requireCode(t, err, api.CodeUnknownTarget)
	_, err = s.WriteFeedback(testContext, "missing", "")
	requireCode(t, err, api.CodeInvalidParams)
}

func TestMalformedAnswersDoNotConsumeQuiz(t *testing.T) {
	s := NewService(nil, nil)
	c := courseForTest(t, s, 1)
	q, err := s.CreateQuiz(testContext, c.ID, c.Units[0].Topics[0].ID, false, questionsForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, answers := range [][]Answer{
		{{QuestionID: "foreign", Selected: []int{0}}}, {{QuestionID: q.Questions[0].ID}, {QuestionID: q.Questions[0].ID}},
		{{QuestionID: q.Questions[0].ID, Selected: []int{-1}}}, {{QuestionID: q.Questions[0].ID, Selected: []int{0, 1}}}, {{QuestionID: q.Questions[0].ID, Pairs: []int{0}}},
	} {
		_, err = s.Submit(testContext, q.ID, answers)
		requireCode(t, err, api.CodeInvalidParams)
	}
	attempts, err := s.Attempts(testContext, c.ID, "")
	if err != nil || len(attempts) != 0 {
		t.Fatal(attempts, err)
	}
	got, err := s.GetQuiz(testContext, q.ID)
	if err != nil || !reflect.DeepEqual(got, q) {
		t.Fatal(got, err)
	}
	if result, err := s.Submit(testContext, q.ID, nil); err != nil || result.Passed || result.Results[0].Correct {
		t.Fatal(result, err)
	}
}

func TestUnknownSyllabusIDsBecomeNewEntries(t *testing.T) {
	s := NewService(nil, nil)
	created, err := s.CreateCourse(testContext, Course{Title: "Course", Units: []Unit{{ID: "u-draft", Title: "Unit", Topics: []Topic{{ID: "t-draft-1", Title: "One"}, {ID: "t-draft-2", Title: "Two"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Units[0].ID == "u-draft" || created.Units[0].Topics[0].ID == "t-draft-1" || created.Units[0].Topics[0].Status != "not_started" {
		t.Fatalf("drafted ids kept: %+v", created.Units)
	}
	units := owned(created.Units)
	units[0].Topics = append(units[0].Topics, Topic{ID: "foreign", Title: "Three"})
	updated, err := s.UpdateSyllabus(testContext, created.ID, units)
	if err != nil {
		t.Fatal(err)
	}
	topics := updated.Units[0].Topics
	if len(topics) != 3 || topics[2].ID == "foreign" || topics[0].ID != created.Units[0].Topics[0].ID {
		t.Fatalf("unknown topic id not treated as new: %+v", topics)
	}
}
