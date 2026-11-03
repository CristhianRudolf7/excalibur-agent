package classroom

import "github.com/CristhianRudolf7/excalibur-agent/internal/api"

type QuestionView = api.ClassroomQuestionView
type CourseView = api.ClassroomCourseView
type QuizView = api.ClassroomQuizView
type AttemptView = api.ClassroomAttemptView

func (q Question) View() QuestionView {
	return QuestionView{ID: q.ID, Kind: q.Kind, Prompt: q.Prompt, Options: append([]string(nil), q.Options...), Left: append([]string(nil), q.Left...), Right: append([]string(nil), q.Right...)}
}
func (q Quiz) View() QuizView {
	questions := make([]QuestionView, 0, len(q.Questions))
	for _, v := range q.Questions {
		questions = append(questions, v.View())
	}
	return QuizView{ID: q.ID, CourseID: q.CourseID, TopicID: q.TopicID, Final: q.Final, Questions: questions, CreatedAt: q.CreatedAt}
}
func classView(c *Class) *api.ClassroomClass {
	if c == nil {
		return nil
	}
	return &api.ClassroomClass{Format: c.Format, Content: c.Content, WhiteboardID: c.WhiteboardID, CreatedAt: c.CreatedAt}
}
func topicView(t Topic) api.ClassroomTopicView {
	return api.ClassroomTopicView{ID: t.ID, Title: t.Title, Summary: t.Summary, Status: t.Status, Class: classView(t.Class), CompletedAt: t.CompletedAt}
}
func (c Course) View() CourseView {
	units := make([]api.ClassroomUnitView, 0, len(c.Units))
	for _, u := range c.Units {
		topics := make([]api.ClassroomTopicView, 0, len(u.Topics))
		for _, t := range u.Topics {
			topics = append(topics, topicView(t))
		}
		units = append(units, api.ClassroomUnitView{ID: u.ID, Title: u.Title, Topics: topics})
	}
	return CourseView{ID: c.ID, Title: c.Title, Goal: c.Goal, Level: c.Level, Language: c.Language, Units: units, FinalExam: api.ClassroomExamState{Unlocked: c.FinalExam.Unlocked, Passed: c.FinalExam.Passed, PassedAt: c.FinalExam.PassedAt}, Progress: c.Progress, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, CompletedAt: c.CompletedAt}
}
func (a Attempt) View() AttemptView {
	results := make([]api.ClassroomAnswerResult, 0, len(a.Results))
	for _, r := range a.Results {
		results = append(results, api.ClassroomAnswerResult{QuestionID: r.QuestionID, Correct: r.Correct})
	}
	return AttemptView{ID: a.ID, QuizID: a.QuizID, Results: results, Passed: a.Passed, Feedback: a.Feedback, CreatedAt: a.CreatedAt}
}
