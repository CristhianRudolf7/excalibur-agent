// Package classroom implements personalized courses: a syllabus of topics
// (optionally grouped in units), stored class content delivered in chat or
// on the interactive whiteboard, mandatory quizzes graded on the server and a
// final exam. A topic is complete only when every question of a quiz attempt
// is answered correctly; a course reaches 100% only when its final exam is
// passed the same way.
package classroom

import "time"

// Course is one personalized course owned by the local user.
type Course struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Goal        string     `json:"goal"`  // what the user wants to learn
	Level       string     `json:"level"` // e.g. beginner, intermediate, advanced
	Language    string     `json:"language"`
	Units       []Unit     `json:"units"` // a course without units has one implicit unit
	FinalExam   ExamState  `json:"final_exam"`
	Progress    int        `json:"progress"` // 0-100, see Progress rules
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Unit groups topics of a large syllabus. Title may be empty for the
// implicit unit of a small course.
type Unit struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Topics []Topic `json:"topics"`
}

// Topic is one entry of the syllabus. Topics can be opened in any order.
type Topic struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Status      string     `json:"status"` // not_started | in_progress | completed
	Class       *Class     `json:"class,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Class is the stored lesson of a topic.
type Class struct {
	Format       string    `json:"format"`  // "chat" or "whiteboard"
	Content      string    `json:"content"` // markdown lesson text (always present, also for whiteboard classes)
	WhiteboardID string    `json:"whiteboard_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Question kinds. Only closed questions exist.
const (
	KindSingle   = "single"   // pick exactly one option
	KindMultiple = "multiple" // pick every correct option
	KindMatch    = "match"    // pair every left item with one right item
)

// Question is a closed quiz question. Answer fields are never sent to the
// browser; QuestionView is the client shape.
type Question struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Prompt      string   `json:"prompt"`
	Options     []string `json:"options,omitempty"`     // single/multiple
	Correct     []int    `json:"correct,omitempty"`     // option indexes (single: exactly one)
	Left        []string `json:"left,omitempty"`        // match
	Right       []string `json:"right,omitempty"`       // match
	Pairs       []int    `json:"pairs,omitempty"`       // Pairs[i] = index in Right matching Left[i]
	Explanation string   `json:"explanation,omitempty"` // shown after grading
}

// Quiz is one generated set of questions for a topic or the final exam.
// Every retry uses a new Quiz with new questions.
type Quiz struct {
	ID        string     `json:"id"`
	CourseID  string     `json:"course_id"`
	TopicID   string     `json:"topic_id,omitempty"` // empty for the final exam
	Final     bool       `json:"final"`
	Questions []Question `json:"questions"`
	CreatedAt time.Time  `json:"created_at"`
}

// Answer is the user's response to one question.
type Answer struct {
	QuestionID string `json:"question_id"`
	Selected   []int  `json:"selected,omitempty"` // single/multiple
	Pairs      []int  `json:"pairs,omitempty"`    // match
}

// Attempt is a graded submission.
type Attempt struct {
	ID        string         `json:"id"`
	QuizID    string         `json:"quiz_id"`
	Answers   []Answer       `json:"answers"`
	Results   []AnswerResult `json:"results"`
	Passed    bool           `json:"passed"`             // every answer correct
	Feedback  string         `json:"feedback,omitempty"` // agent feedback written after grading
	CreatedAt time.Time      `json:"created_at"`
}

// AnswerResult says whether one answer was correct; the correct answer is
// not revealed to the client.
type AnswerResult struct {
	QuestionID  string `json:"question_id"`
	Correct     bool   `json:"correct"`
	Explanation string `json:"explanation,omitempty"`
}

// ExamState is the final exam status of a course.
type ExamState struct {
	Unlocked bool       `json:"unlocked"` // every topic completed
	Passed   bool       `json:"passed"`
	PassedAt *time.Time `json:"passed_at,omitempty"`
}

// Progress rules: completed topics count up to 99%; the course shows 100%
// only after the final exam is passed. Editing the syllabus recomputes it.
// Minimum final exam size: max(10, 2 x the largest topic quiz).
const (
	MaxTopicProgress  = 99
	MinFinalQuestions = 10
)
