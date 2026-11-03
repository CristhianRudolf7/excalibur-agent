package api

import "time"

// Classroom client models deliberately have no answer-key fields. Explanations
// are exposed only in graded results, never alongside an unanswered question.
type ClassroomQuestionView struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options,omitempty"`
	Left    []string `json:"left,omitempty"`
	Right   []string `json:"right,omitempty"`
}
type ClassroomQuizView struct {
	ID        string                  `json:"id"`
	CourseID  string                  `json:"course_id"`
	TopicID   string                  `json:"topic_id,omitempty"`
	Final     bool                    `json:"final"`
	Questions []ClassroomQuestionView `json:"questions"`
	CreatedAt time.Time               `json:"created_at"`
}
type ClassroomClass struct {
	Format       string    `json:"format"`
	Content      string    `json:"content"`
	WhiteboardID string    `json:"whiteboard_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
type ClassroomTopicView struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Status      string          `json:"status"`
	Class       *ClassroomClass `json:"class,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}
type ClassroomUnitView struct {
	ID     string               `json:"id"`
	Title  string               `json:"title"`
	Topics []ClassroomTopicView `json:"topics"`
}
type ClassroomExamState struct {
	Unlocked bool       `json:"unlocked"`
	Passed   bool       `json:"passed"`
	PassedAt *time.Time `json:"passed_at,omitempty"`
}
type ClassroomCourseView struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Goal        string              `json:"goal"`
	Level       string              `json:"level"`
	Language    string              `json:"language"`
	Units       []ClassroomUnitView `json:"units"`
	FinalExam   ClassroomExamState  `json:"final_exam"`
	Progress    int                 `json:"progress"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	CompletedAt *time.Time          `json:"completed_at,omitempty"`
}
type ClassroomCourseSummary struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Progress        int       `json:"progress"`
	UpdatedAt       time.Time `json:"updated_at"`
	TotalTopics     int       `json:"total_topics"`
	CompletedTopics int       `json:"completed_topics"`
}
type ClassroomAnswer struct {
	QuestionID string `json:"question_id"`
	Selected   []int  `json:"selected,omitempty"`
	Pairs      []int  `json:"pairs,omitempty"`
}
type ClassroomAnswerResult struct {
	QuestionID  string `json:"question_id"`
	Correct     bool   `json:"correct"`
	Explanation string `json:"explanation,omitempty"`
}
type ClassroomAttemptView struct {
	ID        string                  `json:"id"`
	QuizID    string                  `json:"quiz_id"`
	Results   []ClassroomAnswerResult `json:"results"`
	Passed    bool                    `json:"passed"`
	Feedback  string                  `json:"feedback,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
}

const (
	MethodClassroomList           = "classroom.list"
	MethodClassroomGet            = "classroom.get"
	MethodClassroomUpdateSyllabus = "classroom.update_syllabus"
	MethodClassroomDelete         = "classroom.delete"
	MethodClassroomOpenTopic      = "classroom.open_topic"
	MethodClassroomRequestQuiz    = "classroom.request_quiz"
	MethodClassroomGetQuiz        = "classroom.get_quiz"
	MethodClassroomSubmit         = "classroom.submit"
	MethodClassroomAttempts       = "classroom.attempts"
)

type ClassroomCourseParams struct {
	CourseID string `json:"course_id"`
}
type ClassroomTopicParams struct {
	CourseID string `json:"course_id"`
	TopicID  string `json:"topic_id"`
}
type ClassroomSyllabusTopic struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}
type ClassroomSyllabusUnit struct {
	ID     string                   `json:"id,omitempty"`
	Title  string                   `json:"title"`
	Topics []ClassroomSyllabusTopic `json:"topics"`
}
type ClassroomUpdateSyllabusParams struct {
	CourseID string                  `json:"course_id"`
	Units    []ClassroomSyllabusUnit `json:"units"`
}
type ClassroomRequestQuizParams struct {
	CourseID string `json:"course_id"`
	TopicID  string `json:"topic_id,omitempty"`
	Final    bool   `json:"final,omitempty"`
}
type ClassroomGetQuizParams struct {
	QuizID string `json:"quiz_id"`
}
type ClassroomSubmitParams struct {
	QuizID  string            `json:"quiz_id"`
	Answers []ClassroomAnswer `json:"answers"`
}
type ClassroomAttemptsParams struct {
	CourseID string `json:"course_id"`
	TopicID  string `json:"topic_id,omitempty"`
}
type ClassroomListResult struct {
	Courses []ClassroomCourseSummary `json:"courses"`
}
type ClassroomOpenTopicResult struct {
	Topic ClassroomTopicView `json:"topic"`
	Class *ClassroomClass    `json:"class,omitempty"`
}
type ClassroomRequestQuizResult struct {
	Quiz      *ClassroomQuizView `json:"quiz,omitempty"`
	Requested bool               `json:"requested"`
}
type ClassroomSubmitResult struct {
	Attempt string                  `json:"attempt"`
	Passed  bool                    `json:"passed"`
	Results []ClassroomAnswerResult `json:"results"`
}
type ClassroomAttemptsResult struct {
	Attempts []ClassroomAttemptView `json:"attempts"`
}

// ClassroomEvent identifies the affected entities without embedding server models.
type ClassroomEvent struct {
	CourseID  string `json:"course_id"`
	TopicID   string `json:"topic_id,omitempty"`
	QuizID    string `json:"quiz_id,omitempty"`
	AttemptID string `json:"attempt_id,omitempty"`
	Final     bool   `json:"final,omitempty"`
}
type ClassroomCourseEvent struct {
	ClassroomEvent
	Course  *ClassroomCourseView `json:"course,omitempty"`
	Deleted bool                 `json:"deleted,omitempty"`
}
type ClassroomClassReadyEvent struct {
	ClassroomEvent
	Class ClassroomClass `json:"class"`
}
type ClassroomQuizReadyEvent struct {
	ClassroomEvent
	Quiz ClassroomQuizView `json:"quiz"`
}
type ClassroomAttemptGradedEvent struct {
	ClassroomEvent
	Passed  bool                    `json:"passed"`
	Results []ClassroomAnswerResult `json:"results"`
}
type ClassroomFeedbackReadyEvent struct {
	ClassroomEvent
	Feedback string `json:"feedback"`
}
type ClassroomTopicCompletedEvent struct {
	ClassroomEvent
	Topic    ClassroomTopicView `json:"topic"`
	Progress int                `json:"progress"`
}
