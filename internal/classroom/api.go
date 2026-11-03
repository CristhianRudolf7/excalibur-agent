package classroom

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// Op is the only client boundary. It returns explicit views and never Quiz or
// Attempt server models, even when an operation also emits a bus event.
func (s *Service) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if s == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "classroom unavailable"}
	}
	decode := func(v any) error {
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		if len(raw) > 2<<20 {
			return invalid("classroom input exceeds 2 MiB")
		}
		trimmed := bytes.TrimSpace(raw)
		if !json.Valid(trimmed) || len(trimmed) == 0 || trimmed[0] != '{' {
			return invalid("params must be an object")
		}
		d := json.NewDecoder(bytes.NewReader(trimmed))
		d.DisallowUnknownFields()
		if err := d.Decode(v); err != nil {
			return invalid(err.Error())
		}
		return nil
	}
	required := func(v, name string) error {
		if strings.TrimSpace(v) == "" {
			return invalid(name + " required")
		}
		return nil
	}
	switch method {
	case api.MethodClassroomList:
		if err := decode(&struct{}{}); err != nil {
			return nil, err
		}
		courses, err := s.ListCourses(ctx)
		out := api.ClassroomListResult{Courses: []api.ClassroomCourseSummary{}}
		for _, c := range courses {
			out.Courses = append(out.Courses, api.ClassroomCourseSummary(c))
		}
		return out, err
	case api.MethodClassroomGet, api.MethodClassroomDelete:
		var p api.ClassroomCourseParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := required(p.CourseID, "course_id"); err != nil {
			return nil, err
		}
		if method == api.MethodClassroomDelete {
			err := s.DeleteCourse(ctx, p.CourseID)
			return map[string]bool{"ok": err == nil}, err
		}
		c, err := s.GetCourse(ctx, p.CourseID)
		return c.View(), err
	case api.MethodClassroomUpdateSyllabus:
		var p api.ClassroomUpdateSyllabusParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := required(p.CourseID, "course_id"); err != nil {
			return nil, err
		}
		units := make([]Unit, 0, len(p.Units))
		for _, u := range p.Units {
			topics := make([]Topic, 0, len(u.Topics))
			for _, t := range u.Topics {
				topics = append(topics, Topic{ID: t.ID, Title: t.Title, Summary: t.Summary})
			}
			units = append(units, Unit{ID: u.ID, Title: u.Title, Topics: topics})
		}
		c, err := s.UpdateSyllabus(ctx, p.CourseID, units)
		return c.View(), err
	case api.MethodClassroomOpenTopic:
		var p api.ClassroomTopicParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := required(p.CourseID, "course_id"); err != nil {
			return nil, err
		}
		if err := required(p.TopicID, "topic_id"); err != nil {
			return nil, err
		}
		out, err := s.OpenTopic(ctx, p.CourseID, p.TopicID)
		return api.ClassroomOpenTopicResult{Topic: topicView(out.Topic), Class: classView(out.Class)}, err
	case api.MethodClassroomRequestQuiz:
		var p api.ClassroomRequestQuizParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := required(p.CourseID, "course_id"); err != nil {
			return nil, err
		}
		result, err := s.RequestQuiz(ctx, p.CourseID, p.TopicID, p.Final)
		out := api.ClassroomRequestQuizResult{Requested: result.Requested}
		if result.Quiz != nil {
			v := result.Quiz.View()
			out.Quiz = &v
		}
		return out, err
	case api.MethodClassroomGetQuiz:
		var p api.ClassroomGetQuizParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := required(p.QuizID, "quiz_id"); err != nil {
			return nil, err
		}
		q, err := s.GetQuiz(ctx, p.QuizID)
		return q.View(), err
	case api.MethodClassroomSubmit:
		var p api.ClassroomSubmitParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := required(p.QuizID, "quiz_id"); err != nil {
			return nil, err
		}
		answers := make([]Answer, 0, len(p.Answers))
		for _, a := range p.Answers {
			answers = append(answers, Answer(a))
		}
		result, err := s.Submit(ctx, p.QuizID, answers)
		out := api.ClassroomSubmitResult{Attempt: result.Attempt, Passed: result.Passed, Results: []api.ClassroomAnswerResult{}}
		for _, r := range result.Results {
			out.Results = append(out.Results, api.ClassroomAnswerResult{QuestionID: r.QuestionID, Correct: r.Correct})
		}
		return out, err
	case api.MethodClassroomAttempts:
		var p api.ClassroomAttemptsParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := required(p.CourseID, "course_id"); err != nil {
			return nil, err
		}
		history, err := s.Attempts(ctx, p.CourseID, p.TopicID)
		out := api.ClassroomAttemptsResult{Attempts: []AttemptView{}}
		for _, a := range history {
			out.Attempts = append(out.Attempts, a.View())
		}
		return out, err
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown classroom method"}
	}
}
