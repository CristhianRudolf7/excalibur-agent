// Package fake provides an in-memory AgentService for tool and daemon tests.
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
)

// Agent records calls and holds trusted course fixtures. Set fixtures before use;
// SnapshotCalls provides a synchronized view after asynchronous daemon turns.
type Agent struct {
	mu      sync.Mutex
	Courses map[string]classroom.AgentCourse
	Calls   []string
	Err     error
}

func (a *Agent) SnapshotCalls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.Calls...)
}
func (a *Agent) CreateCourse(_ context.Context, c classroom.Course) (classroom.Course, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "create_course")
	if a.Courses == nil {
		a.Courses = map[string]classroom.AgentCourse{}
	}
	c.ID = "course-created"
	a.Courses[c.ID] = classroom.AgentCourse{Course: c}
	return c, a.Err
}
func (a *Agent) GetCourse(_ context.Context, id string) (classroom.AgentCourse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "get_course")
	if a.Err != nil {
		return classroom.AgentCourse{}, a.Err
	}
	c, ok := a.Courses[id]
	if !ok {
		return c, fmt.Errorf("unknown course")
	}
	return clone(c), nil
}
func (a *Agent) UpdateSyllabus(_ context.Context, id string, units []classroom.Unit) (classroom.Course, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "update_syllabus")
	c := a.Courses[id]
	c.Units = units
	a.Courses[id] = c
	return c.Course, a.Err
}
func (a *Agent) WriteClass(_ context.Context, course, topic string, c classroom.Class) (classroom.Class, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "write_class")
	if a.Err != nil {
		return c, a.Err
	}
	record := clone(a.Courses[course])
	for i := range record.Units {
		for j := range record.Units[i].Topics {
			if record.Units[i].Topics[j].ID == topic {
				record.Units[i].Topics[j].Class = &c
			}
		}
	}
	a.Courses[course] = record
	return c, nil
}
func (a *Agent) CreateQuiz(_ context.Context, q classroom.Quiz) (classroom.Quiz, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "create_quiz")
	q.ID = "quiz-created"
	c := a.Courses[q.CourseID]
	c.Quizzes = append(c.Quizzes, q)
	a.Courses[q.CourseID] = c
	return q, a.Err
}
func (a *Agent) WriteFeedback(_ context.Context, id, feedback string) (classroom.Attempt, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "write_feedback")
	if a.Err != nil {
		return classroom.Attempt{}, a.Err
	}
	for course, record := range a.Courses {
		record = clone(record)
		for i := range record.Attempts {
			if record.Attempts[i].ID == id {
				record.Attempts[i].Feedback = feedback
			}
		}
		a.Courses[course] = record
	}
	return classroom.Attempt{ID: id, Feedback: feedback}, nil
}
func (a *Agent) ListCourses(context.Context) ([]classroom.Course, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "list_courses")
	out := []classroom.Course{}
	for _, c := range a.Courses {
		out = append(out, c.Course)
	}
	return out, a.Err
}

func clone(c classroom.AgentCourse) classroom.AgentCourse {
	raw, _ := json.Marshal(c)
	var out classroom.AgentCourse
	_ = json.Unmarshal(raw, &out)
	return out
}

var _ classroom.AgentService = (*Agent)(nil)
