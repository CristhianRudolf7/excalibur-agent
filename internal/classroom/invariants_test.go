package classroom

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestConcurrentSubmissionsAcrossServicesCommitOneAttempt(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[persistent], func(t *testing.T) {
			var db *store.DB
			if persistent {
				var err error
				db, err = store.Open(testContext, filepath.Join(t.TempDir(), "courses.db"), store.DefaultOptions(), migrations.Main())
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
			}
			st := NewStore(db)
			s := NewService(st, nil)
			second := NewService(st, nil)
			if persistent {
				second = NewService(NewStore(db), nil)
			}
			c := courseForTest(t, s, 1)
			q, err := s.CreateQuiz(testContext, c.ID, c.Units[0].Topics[0].ID, false, questionsForTest(1))
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, service := range []*Service{s, second} {
				safego.Go("classroom-concurrent-submit", nil, func() { <-start; _, err := service.Submit(testContext, q.ID, correctAnswers(q)); results <- err })
			}
			close(start)
			failures := 0
			for i := 0; i < 2; i++ {
				if err := <-results; err != nil {
					requireCode(t, err, api.CodeConflict)
					failures++
				}
			}
			history, err := s.Attempts(testContext, c.ID, "")
			if err != nil || failures != 1 || len(history) != 1 {
				t.Fatal(history, err, failures)
			}
			c, _ = s.GetCourse(testContext, c.ID)
			if c.Progress != 99 {
				t.Fatal(c)
			}
		})
	}
}

func TestRestartReusesLatestUnansweredQuizAndFeedback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "courses.db")
	open := func() *store.DB {
		db, err := store.Open(testContext, path, store.DefaultOptions(), migrations.Main())
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := open()
	s := NewService(NewStore(db), nil)
	c := courseForTest(t, s, 2)
	topicID := c.Units[0].Topics[1].ID
	if _, err := s.CreateQuiz(testContext, c.ID, topicID, false, questionsForTest(1)); err != nil {
		t.Fatal(err)
	}
	q, err := s.CreateQuiz(testContext, c.ID, topicID, false, questionsForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	other := passTopic(t, s, c, 0, 1)
	history, _ := s.Attempts(testContext, c.ID, other.TopicID)
	if _, err = s.WriteFeedback(testContext, history[0].ID, "Stored feedback"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db = open()
	defer db.Close()
	s = NewService(NewStore(db), nil)
	request, err := s.RequestQuiz(testContext, c.ID, topicID, false)
	if err != nil || request.Requested || request.Quiz == nil || request.Quiz.ID != q.ID {
		t.Fatal(request, err)
	}
	history, err = s.Attempts(testContext, c.ID, other.TopicID)
	if err != nil || len(history) != 1 || history[0].Feedback != "Stored feedback" {
		t.Fatal(history, err)
	}
	c, err = s.GetCourse(testContext, c.ID)
	if err != nil || c.Progress != 49 {
		t.Fatal(c, err)
	}
}

func TestFinalMinimumTracksCurrentTopicsAndRejectsStaleExams(t *testing.T) {
	s := NewService(nil, nil)
	c := courseForTest(t, s, 2)
	passTopic(t, s, c, 0, 8)
	passTopic(t, s, c, 1, 1)
	_, err := s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(15))
	requireCode(t, err, api.CodeInvalidParams)
	units := owned(c.Units)
	units[0].Topics = units[0].Topics[1:]
	c, err = s.UpdateSyllabus(testContext, c.ID, units)
	if err != nil || !c.FinalExam.Unlocked || c.Progress != 99 {
		t.Fatal(c, err)
	}
	_, err = s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(9))
	requireCode(t, err, api.CodeInvalidParams)
	exam, err := s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(10))
	if err != nil {
		t.Fatal(err)
	}
	// A larger topic quiz makes the previously generated final too short.
	passTopic(t, s, c, 0, 6)
	request, err := s.RequestQuiz(testContext, c.ID, "", true)
	if err != nil || !request.Requested || request.Quiz != nil {
		t.Fatal(request, err)
	}
	_, err = s.Submit(testContext, exam.ID, correctAnswers(exam))
	requireCode(t, err, api.CodeConflict)
	// Adding an incomplete topic locks an otherwise valid final at submission.
	exam, err = s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(12))
	if err != nil {
		t.Fatal(err)
	}
	units = owned(c.Units)
	units[0].Topics = append(units[0].Topics, Topic{Title: "New"})
	if _, err = s.UpdateSyllabus(testContext, c.ID, units); err != nil {
		t.Fatal(err)
	}
	_, err = s.Submit(testContext, exam.ID, correctAnswers(exam))
	requireCode(t, err, api.CodeConflict)
}

func TestFailedAndRepeatedPassesEmitCompletionOnce(t *testing.T) {
	bus := telemetry.NewBus(nil)
	s := NewService(nil, bus)
	c := courseForTest(t, s, 1)
	topicID := c.Units[0].Topics[0].ID
	for i := 0; i < 3; i++ {
		q, err := s.CreateQuiz(testContext, c.ID, topicID, false, questionsForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		answers := correctAnswers(q)
		if i == 0 {
			answers = nil
		}
		if _, err = s.Submit(testContext, q.ID, answers); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			c, _ = s.GetCourse(testContext, c.ID)
			if c.Progress != 0 || c.FinalExam.Unlocked {
				t.Fatal(c)
			}
		}
	}
	for i := 0; i < 3; i++ {
		q, err := s.CreateQuiz(testContext, c.ID, "", true, questionsForTest(10))
		if err != nil {
			t.Fatal(err)
		}
		answers := correctAnswers(q)
		if i == 0 {
			answers = nil
		}
		if _, err = s.Submit(testContext, q.ID, answers); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			c, _ = s.GetCourse(testContext, c.ID)
			if c.Progress != 99 || c.FinalExam.Passed {
				t.Fatal(c)
			}
		}
	}
	counts := map[types.EventType]int{}
	for _, ev := range bus.Replay(types.Cursor{}) {
		counts[ev.Type]++
	}
	if counts["classroom.topic_completed"] != 1 || counts["classroom.course_completed"] != 1 {
		t.Fatal(counts)
	}
}

func TestMatchingAndMultipleMalformedAndPartialAnswers(t *testing.T) {
	questions := []Question{
		{ID: "multiple", Kind: KindMultiple, Options: []string{"a", "b", "c"}, Correct: []int{0, 2}},
		{ID: "match", Kind: KindMatch, Left: []string{"a", "b"}, Right: []string{"A", "B"}, Pairs: []int{0, 1}},
	}
	quiz := Quiz{Questions: questions}
	for _, answers := range [][]Answer{
		{{QuestionID: "multiple", Selected: []int{0, 0}}}, {{QuestionID: "multiple", Selected: []int{3}}}, {{QuestionID: "multiple", Pairs: []int{0}}},
		{{QuestionID: "match", Selected: []int{0}}}, {{QuestionID: "match", Pairs: []int{0, 0}}}, {{QuestionID: "match", Pairs: []int{-1}}}, {{QuestionID: "match", Pairs: []int{2}}},
	} {
		_, _, err := grade(quiz, answers)
		requireCode(t, err, api.CodeInvalidParams)
	}
	for _, answers := range [][]Answer{
		{{QuestionID: "multiple", Selected: []int{0, 1, 2}}, {QuestionID: "match", Pairs: []int{0, 1}}},
		{{QuestionID: "multiple", Selected: []int{0, 2}}, {QuestionID: "match", Pairs: []int{0}}},
	} {
		_, passed, err := grade(quiz, answers)
		if err != nil || passed {
			t.Fatal(passed, err)
		}
	}
}

func TestCancelledPorterMutationReturnsNoPartialRecord(t *testing.T) {
	db, err := store.Open(testContext, filepath.Join(t.TempDir(), "courses.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewStore(db)
	service := NewService(s, nil)
	c := courseForTest(t, service, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	callbackDone := make(chan struct{})
	returned := make(chan error, 1)
	ctx, cancel := context.WithCancel(testContext)
	defer cancel()
	safego.Go("classroom-cancelled-mutation", nil, func() {
		record, err := s.mutate(ctx, c.ID, false, func(r *courseRecord) error {
			close(started)
			<-release
			r.Course.Title = "Uncommitted"
			close(callbackDone)
			return ctx.Err()
		})
		if record.Course.ID != "" {
			returned <- errors.New("returned partial record after cancellation")
			return
		}
		returned <- err
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("porter did not start")
	}
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller did not observe cancellation")
	}
	close(release)
	<-callbackDone
	got, err := s.Get(testContext, c.ID)
	if err != nil || got.Title != c.Title {
		t.Fatal(got, err)
	}
}
