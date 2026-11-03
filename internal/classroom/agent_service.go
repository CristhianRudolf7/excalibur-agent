package classroom

import "context"

// AgentAdapter connects trusted tool calls to the same persistent service used
// by RPC handlers. Only this boundary exposes answer-bearing course history.
type AgentAdapter struct{ *Service }

var _ AgentService = AgentAdapter{}

func (a AgentAdapter) GetCourse(ctx context.Context, id string) (AgentCourse, error) {
	r, err := a.Store.record(ctx, id)
	return AgentCourse{Course: r.Course, Quizzes: r.Quizzes, Attempts: r.Attempts}, err
}

func (a AgentAdapter) CreateQuiz(ctx context.Context, q Quiz) (Quiz, error) {
	return a.Service.CreateQuiz(ctx, q.CourseID, q.TopicID, q.Final, q.Questions)
}

func (a AgentAdapter) ListCourses(ctx context.Context) ([]Course, error) {
	return a.Store.List(ctx)
}
