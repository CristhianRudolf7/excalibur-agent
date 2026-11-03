package classroom

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Service owns course state transitions. All mutations are serialized and
// persisted atomically; returned server models are owned by the caller.
type Service struct {
	Store *Store
	Bus   *telemetry.Bus
	mu    sync.Mutex
}

func NewService(s *Store, bus *telemetry.Bus) *Service {
	if s == nil {
		s = NewStore(nil)
	}
	return &Service{Store: s, Bus: bus}
}
func newID() string             { return "cls_" + types.NewULID() }
func invalid(msg string) error  { return &api.Error{Code: api.CodeInvalidParams, Message: msg} }
func conflict(msg string) error { return &api.Error{Code: api.CodeConflict, Message: msg} }
func owned[T any](v T) T {
	raw, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}

func topic(c *Course, id string) (*Topic, error) {
	for i := range c.Units {
		for j := range c.Units[i].Topics {
			t := &c.Units[i].Topics[j]
			if t.ID == id {
				return t, nil
			}
		}
	}
	return nil, missing()
}

// normalizeSyllabus preserves stored state only for IDs already in this course.
// The client cannot assign completion, lessons, or arbitrary topic identities.
func normalizeSyllabus(units []Unit, previous []Unit) ([]Unit, error) {
	units = owned(units)
	oldTopics := map[string]Topic{}
	oldUnits := map[string]bool{}
	for _, u := range previous {
		oldUnits[u.ID] = true
		for _, t := range u.Topics {
			oldTopics[t.ID] = t
		}
	}
	seenUnits := map[string]bool{}
	seenTopics := map[string]bool{}
	count := 0
	for i := range units {
		u := &units[i]
		// Ids that do not belong to the current syllabus (for example ids an
		// agent invented while drafting) denote new units.
		if u.ID == "" || !oldUnits[u.ID] {
			u.ID = newID()
		}
		if seenUnits[u.ID] {
			return nil, invalid("duplicate unit id")
		}
		seenUnits[u.ID] = true
		if u.Topics == nil {
			u.Topics = []Topic{}
		}
		for j := range u.Topics {
			t := &u.Topics[j]
			title, summary := strings.TrimSpace(t.Title), t.Summary
			if title == "" {
				return nil, invalid("topic title required")
			}
			if prev, ok := oldTopics[t.ID]; ok && t.ID != "" {
				*t = prev
			} else {
				// Unknown or empty ids denote new topics.
				*t = Topic{ID: newID(), Status: "not_started"}
			}
			if seenTopics[t.ID] {
				return nil, invalid("duplicate topic id")
			}
			seenTopics[t.ID] = true
			t.Title = title
			t.Summary = summary
			count++
		}
	}
	if count == 0 {
		return nil, invalid("syllabus requires at least one topic")
	}
	return units, nil
}

func recompute(c *Course) {
	total, completed := 0, 0
	for _, u := range c.Units {
		for _, t := range u.Topics {
			total++
			if t.Status == "completed" {
				completed++
			}
		}
	}
	c.FinalExam.Unlocked = total > 0 && completed == total
	if !c.FinalExam.Unlocked {
		c.FinalExam.Passed = false
		c.FinalExam.PassedAt = nil
		c.CompletedAt = nil
	}
	c.Progress = 0
	if total > 0 {
		c.Progress = completed * MaxTopicProgress / total
	}
	if c.FinalExam.Passed {
		c.Progress = 100
		c.CompletedAt = c.FinalExam.PassedAt
	}
}

func (s *Service) CreateCourse(ctx context.Context, c Course) (Course, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(c.Title) == "" {
		return Course{}, invalid("course title required")
	}
	units, err := normalizeSyllabus(c.Units, nil)
	if err != nil {
		return Course{}, err
	}
	now := time.Now().UTC()
	c = Course{ID: newID(), Title: strings.TrimSpace(c.Title), Goal: c.Goal, Level: c.Level, Language: c.Language, Units: units, CreatedAt: now, UpdatedAt: now}
	r, err := s.Store.mutate(ctx, c.ID, true, func(r *courseRecord) error { r.Course = c; return nil })
	if err == nil {
		err = s.courseEvent("course_created", r.Course)
	}
	return r.Course, err
}
func (s *Service) GetCourse(ctx context.Context, id string) (Course, error) {
	return s.Store.Get(ctx, id)
}
func (s *Service) ListCourses(ctx context.Context) ([]CourseSummary, error) {
	courses, err := s.Store.List(ctx)
	out := []CourseSummary{}
	for _, c := range courses {
		out = append(out, c.Summary())
	}
	return out, err
}
func (s *Service) DeleteCourse(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Store.Delete(ctx, id); err != nil {
		return err
	}
	return s.emit("course_updated", api.ClassroomCourseEvent{ClassroomEvent: api.ClassroomEvent{CourseID: id}, Deleted: true})
}
func (s *Service) UpdateSyllabus(ctx context.Context, id string, units []Unit) (Course, error) {
	units = owned(units)
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Store.mutate(ctx, id, false, func(r *courseRecord) error {
		normalized, e := normalizeSyllabus(units, r.Course.Units)
		if e != nil {
			return e
		}
		r.Course.Units = normalized
		recompute(&r.Course)
		r.Course.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err == nil {
		err = s.courseEvent("course_updated", r.Course)
	}
	return r.Course, err
}

func (s *Service) OpenTopic(ctx context.Context, courseID, topicID string) (OpenTopicResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result OpenTopicResult
	_, err := s.Store.mutate(ctx, courseID, false, func(r *courseRecord) error {
		t, e := topic(&r.Course, topicID)
		if e != nil {
			return e
		}
		if t.Status != "completed" {
			t.Status = "in_progress"
		}
		result = OpenTopicResult{Topic: *t, Class: t.Class}
		r.Course.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return OpenTopicResult{}, err
	}
	if result.Class == nil {
		err = s.emit("class_requested", api.ClassroomEvent{CourseID: courseID, TopicID: topicID})
	}
	return result, err
}
func (s *Service) WriteClass(ctx context.Context, courseID, topicID string, lesson Class) (Class, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lesson.Format != "chat" && lesson.Format != "whiteboard" {
		return Class{}, invalid("class format must be chat or whiteboard")
	}
	if strings.TrimSpace(lesson.Content) == "" {
		return Class{}, invalid("class content required")
	}
	if lesson.Format == "whiteboard" && strings.TrimSpace(lesson.WhiteboardID) == "" {
		return Class{}, invalid("whiteboard_id required")
	}
	if lesson.Format == "chat" {
		lesson.WhiteboardID = ""
	}
	lesson.CreatedAt = time.Now().UTC()
	_, err := s.Store.mutate(ctx, courseID, false, func(r *courseRecord) error {
		t, e := topic(&r.Course, topicID)
		if e != nil {
			return e
		}
		t.Class = &lesson
		if t.Status != "completed" {
			t.Status = "in_progress"
		}
		r.Course.UpdatedAt = lesson.CreatedAt
		return nil
	})
	if err == nil {
		err = s.emit("class_ready", api.ClassroomClassReadyEvent{ClassroomEvent: api.ClassroomEvent{CourseID: courseID, TopicID: topicID}, Class: *classView(&lesson)})
	}
	return lesson, err
}

func validateQuestions(questions []Question) error {
	if len(questions) == 0 {
		return invalid("quiz requires questions")
	}
	seen := map[string]bool{}
	for _, q := range questions {
		if q.ID != "" && seen[q.ID] {
			return invalid("duplicate question id")
		}
		seen[q.ID] = true
		if strings.TrimSpace(q.Prompt) == "" {
			return invalid("question prompt required")
		}
		switch q.Kind {
		case KindSingle, KindMultiple:
			if len(q.Options) < 2 || len(q.Options) > 8 {
				return invalid("choice questions require 2 to 8 options")
			}
			for _, v := range q.Options {
				if strings.TrimSpace(v) == "" {
					return invalid("empty option")
				}
			}
			if (q.Kind == KindSingle && len(q.Correct) != 1) || len(q.Correct) == 0 {
				return invalid("invalid correct answer count")
			}
			if !validIndexes(q.Correct, len(q.Options), false) {
				return invalid("invalid correct indexes")
			}
			if len(q.Left) > 0 || len(q.Right) > 0 || len(q.Pairs) > 0 {
				return invalid("choice question cannot contain matching fields")
			}
		case KindMatch:
			n := len(q.Left)
			if n < 2 || n > 8 || len(q.Right) != n || !validIndexes(q.Pairs, n, true) {
				return invalid("matching requires 2 to 8 equal-length sides and a permutation")
			}
			for _, side := range [][]string{q.Left, q.Right} {
				for _, v := range side {
					if strings.TrimSpace(v) == "" {
						return invalid("empty matching item")
					}
				}
			}
			if len(q.Options) > 0 || len(q.Correct) > 0 {
				return invalid("matching question cannot contain choice fields")
			}
		default:
			return invalid("invalid question kind")
		}
	}
	return nil
}
func validIndexes(indexes []int, n int, permutation bool) bool {
	if permutation && len(indexes) != n {
		return false
	}
	seen := map[int]bool{}
	for _, i := range indexes {
		if i < 0 || i >= n || seen[i] {
			return false
		}
		seen[i] = true
	}
	return true
}
func finalMinimum(r *courseRecord) int {
	n := MinFinalQuestions
	active := map[string]bool{}
	for _, u := range r.Course.Units {
		for _, t := range u.Topics {
			active[t.ID] = true
		}
	}
	for _, q := range r.Quizzes {
		if !q.Final && active[q.TopicID] && len(q.Questions)*2 > n {
			n = len(q.Questions) * 2
		}
	}
	return n
}
func quizTarget(r *courseRecord, topicID string, final bool) error {
	if final {
		if topicID != "" {
			return invalid("final quiz cannot have topic_id")
		}
		if !r.Course.FinalExam.Unlocked {
			return conflict("final exam is locked")
		}
		return nil
	}
	if topicID == "" {
		return invalid("topic_id required for topic quiz")
	}
	_, err := topic(&r.Course, topicID)
	return err
}
func (s *Service) CreateQuiz(ctx context.Context, courseID, topicID string, final bool, questions []Question) (Quiz, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	questions = owned(questions)
	if err := validateQuestions(questions); err != nil {
		return Quiz{}, err
	}
	for i := range questions {
		if questions[i].ID == "" {
			questions[i].ID = newID()
		}
	}
	quiz := Quiz{ID: newID(), CourseID: courseID, TopicID: topicID, Final: final, Questions: questions, CreatedAt: time.Now().UTC()}
	_, err := s.Store.mutate(ctx, courseID, false, func(r *courseRecord) error {
		if e := quizTarget(r, topicID, final); e != nil {
			return e
		}
		if final && len(questions) < finalMinimum(r) {
			return invalid("final exam has too few questions")
		}
		r.Quizzes = append(r.Quizzes, quiz)
		r.Course.UpdatedAt = quiz.CreatedAt
		return nil
	})
	if err == nil {
		err = s.emit("quiz_ready", api.ClassroomQuizReadyEvent{ClassroomEvent: api.ClassroomEvent{CourseID: courseID, TopicID: topicID, QuizID: quiz.ID, Final: final}, Quiz: quiz.View()})
	}
	return quiz, err
}
func (s *Service) GetQuiz(ctx context.Context, id string) (Quiz, error) {
	courseID, err := s.Store.locate(ctx, id, false)
	if err != nil {
		return Quiz{}, err
	}
	r, err := s.Store.record(ctx, courseID)
	if err != nil {
		return Quiz{}, err
	}
	for _, q := range r.Quizzes {
		if q.ID == id {
			return q, nil
		}
	}
	return Quiz{}, missing()
}

// RequestQuiz reuses the newest unanswered quiz, including after a restart.
// A nil Quiz means the agent must generate a fresh set of questions.
func (s *Service) RequestQuiz(ctx context.Context, courseID, topicID string, final bool) (QuizRequestResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Store.record(ctx, courseID)
	if err != nil {
		return QuizRequestResult{}, err
	}
	if err = quizTarget(&r, topicID, final); err != nil {
		return QuizRequestResult{}, err
	}
	attempted := map[string]bool{}
	for _, a := range r.Attempts {
		attempted[a.QuizID] = true
	}
	var latest *Quiz
	for _, q := range r.Quizzes {
		if q.TopicID == topicID && q.Final == final && !attempted[q.ID] && (!final || len(q.Questions) >= finalMinimum(&r)) {
			if latest == nil || q.CreatedAt.After(latest.CreatedAt) {
				v := q
				latest = &v
			}
		}
	}
	if latest == nil {
		err = s.emit("quiz_requested", api.ClassroomEvent{CourseID: courseID, TopicID: topicID, Final: final})
	}
	return QuizRequestResult{Quiz: latest, Requested: latest == nil}, err
}

func grade(q Quiz, answers []Answer) ([]AnswerResult, bool, error) {
	byID := map[string]Answer{}
	questions := map[string]Question{}
	for _, question := range q.Questions {
		questions[question.ID] = question
	}
	for _, a := range answers {
		question, ok := questions[a.QuestionID]
		if !ok {
			return nil, false, invalid("unknown answer question_id")
		}
		if _, ok := byID[a.QuestionID]; ok {
			return nil, false, invalid("duplicate answer question_id")
		}
		if question.Kind == KindMatch {
			if len(a.Selected) > 0 || (len(a.Pairs) > 0 && !validIndexes(a.Pairs, len(question.Right), false)) {
				return nil, false, invalid("invalid matching answer")
			}
		} else if len(a.Pairs) > 0 || !validIndexes(a.Selected, len(question.Options), false) || (question.Kind == KindSingle && len(a.Selected) > 1) {
			return nil, false, invalid("invalid choice answer")
		}
		byID[a.QuestionID] = a
	}
	results := make([]AnswerResult, 0, len(q.Questions))
	passed := true
	for _, question := range q.Questions {
		a, ok := byID[question.ID]
		if question.Kind == KindMatch {
			ok = ok && len(a.Pairs) == len(question.Pairs)
			if ok {
				for i, v := range question.Pairs {
					if a.Pairs[i] != v {
						ok = false
					}
				}
			}
		} else {
			ok = ok && len(a.Selected) == len(question.Correct)
			selected := map[int]bool{}
			for _, v := range a.Selected {
				selected[v] = true
			}
			for _, v := range question.Correct {
				if !selected[v] {
					ok = false
				}
			}
		}
		results = append(results, AnswerResult{QuestionID: question.ID, Correct: ok, Explanation: question.Explanation})
		passed = passed && ok
	}
	return results, passed, nil
}
func (s *Service) Submit(ctx context.Context, quizID string, answers []Answer) (SubmitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	courseID, err := s.Store.locate(ctx, quizID, false)
	if err != nil {
		return SubmitResult{}, err
	}
	answers = owned(answers)
	var attempt Attempt
	var quizEvent api.ClassroomEvent
	var topicCompleted, courseCompleted bool
	record, err := s.Store.mutate(ctx, courseID, false, func(r *courseRecord) error {
		var quiz *Quiz
		for i := range r.Quizzes {
			if r.Quizzes[i].ID == quizID {
				quiz = &r.Quizzes[i]
				break
			}
		}
		if quiz == nil {
			return missing()
		}
		for _, a := range r.Attempts {
			if a.QuizID == quizID {
				return conflict("quiz already answered; request a new quiz")
			}
		}
		if e := quizTarget(r, quiz.TopicID, quiz.Final); e != nil {
			return e
		}
		if quiz.Final && len(quiz.Questions) < finalMinimum(r) {
			return conflict("final exam has too few questions for the current syllabus")
		}
		results, passed, e := grade(*quiz, answers)
		if e != nil {
			return e
		}
		quizEvent = api.ClassroomEvent{CourseID: courseID, TopicID: quiz.TopicID, QuizID: quizID, Final: quiz.Final}
		now := time.Now().UTC()
		attempt = Attempt{ID: newID(), QuizID: quizID, Answers: answers, Results: results, Passed: passed, CreatedAt: now}
		r.Attempts = append(r.Attempts, attempt)
		if passed {
			if quiz.Final {
				if !r.Course.FinalExam.Passed {
					courseCompleted = true
					r.Course.FinalExam.Passed = true
					r.Course.FinalExam.PassedAt = &now
				}
			} else {
				t, _ := topic(&r.Course, quiz.TopicID)
				if t.Status != "completed" {
					topicCompleted = true
					t.Status = "completed"
					t.CompletedAt = &now
				}
			}
		}
		recompute(&r.Course)
		r.Course.UpdatedAt = now
		return nil
	})
	if err != nil {
		return SubmitResult{}, err
	}
	if err == nil {
		quizEvent.AttemptID = attempt.ID
		view := attempt.View()
		err = s.emit("attempt_graded", api.ClassroomAttemptGradedEvent{ClassroomEvent: quizEvent, Passed: attempt.Passed, Results: view.Results})
		if err == nil && topicCompleted {
			t, _ := topic(&record.Course, quizEvent.TopicID)
			err = s.emit("topic_completed", api.ClassroomTopicCompletedEvent{ClassroomEvent: quizEvent, Topic: topicView(*t), Progress: record.Course.Progress})
		}
		if err == nil && courseCompleted {
			err = s.courseEvent("course_completed", record.Course)
		}
		if err == nil {
			err = s.courseEvent("course_updated", record.Course)
		}
	}
	return SubmitResult{Attempt: attempt.ID, Passed: attempt.Passed, Results: attempt.Results}, err
}
func (s *Service) WriteFeedback(ctx context.Context, attemptID, feedback string) (Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(feedback) == "" {
		return Attempt{}, invalid("feedback required")
	}
	courseID, err := s.Store.locate(ctx, attemptID, true)
	if err != nil {
		return Attempt{}, err
	}
	var attempt Attempt
	record, err := s.Store.mutate(ctx, courseID, false, func(r *courseRecord) error {
		for i := range r.Attempts {
			if r.Attempts[i].ID == attemptID {
				r.Attempts[i].Feedback = feedback
				attempt = r.Attempts[i]
				r.Course.UpdatedAt = time.Now().UTC()
				return nil
			}
		}
		return missing()
	})
	if err != nil {
		return Attempt{}, err
	}
	if err == nil {
		envelope := api.ClassroomEvent{CourseID: courseID, AttemptID: attemptID, QuizID: attempt.QuizID}
		for _, q := range record.Quizzes {
			if q.ID == attempt.QuizID {
				envelope.TopicID = q.TopicID
				envelope.Final = q.Final
				break
			}
		}
		err = s.emit("feedback_ready", api.ClassroomFeedbackReadyEvent{ClassroomEvent: envelope, Feedback: feedback})
	}
	return attempt, err
}
func (s *Service) Attempts(ctx context.Context, courseID, topicID string) ([]Attempt, error) {
	r, err := s.Store.record(ctx, courseID)
	if err != nil {
		return nil, err
	}
	if topicID != "" {
		if _, err = topic(&r.Course, topicID); err != nil {
			return nil, err
		}
	}
	ids := map[string]bool{}
	for _, q := range r.Quizzes {
		if topicID == "" || q.TopicID == topicID {
			ids[q.ID] = true
		}
	}
	out := []Attempt{}
	for _, a := range r.Attempts {
		if ids[a.QuizID] {
			out = append(out, a)
		}
	}
	return out, nil
}
