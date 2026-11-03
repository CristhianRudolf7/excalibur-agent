package classroom

import "context"

// AgentCourse includes answer-bearing quiz history for the trusted agent only.
// It must never be returned by the browser-facing classroom.get method.
type AgentCourse struct {
	Course
	Quizzes  []Quiz    `json:"quizzes"`
	Attempts []Attempt `json:"attempts"`
}

// AgentService is the storage-independent classroom seam. Implementations must
// enforce course ownership, final-exam unlocking, fresh retries and atomic writes.
// GetCourse returns quiz answers to the trusted tool, never to a web client.
type AgentService interface {
	CreateCourse(context.Context, Course) (Course, error)
	GetCourse(context.Context, string) (AgentCourse, error)
	UpdateSyllabus(context.Context, string, []Unit) (Course, error)
	WriteClass(context.Context, string, string, Class) (Class, error)
	CreateQuiz(context.Context, Quiz) (Quiz, error)
	WriteFeedback(context.Context, string, string) (Attempt, error)
	ListCourses(context.Context) ([]Course, error)
}
