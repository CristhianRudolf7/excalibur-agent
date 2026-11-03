package classroom

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func TestStoreRoundTripRestartRollbackAndDelete(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[persistent], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "classroom.db")
			var db *store.DB
			open := func() *store.DB {
				d, e := store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
				if e != nil {
					t.Fatal(e)
				}
				return d
			}
			if persistent {
				db = open()
			}
			s := NewStore(db)
			now := time.Now().UTC()
			want := courseRecord{
				Course:   Course{ID: "course", Title: "Course", UpdatedAt: now, Units: []Unit{{ID: "unit", Topics: []Topic{{ID: "topic", Class: &Class{Format: "chat", Content: "Lesson", CreatedAt: now}}}}}},
				Quizzes:  []Quiz{{ID: "quiz", CourseID: "course", TopicID: "topic", CreatedAt: now, Questions: []Question{{ID: "q", Kind: KindSingle, Options: []string{"a", "b"}, Correct: []int{1}}}}},
				Attempts: []Attempt{{ID: "attempt", QuizID: "quiz", CreatedAt: now, Answers: []Answer{{QuestionID: "q", Selected: []int{0}}}, Results: []AnswerResult{{QuestionID: "q", Correct: false}}, Feedback: "Review the lesson"}},
			}
			if _, err := s.mutate(ctx, "course", true, func(r *courseRecord) error { *r = want; return nil }); err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("rollback")
			if _, err := s.mutate(ctx, "course", false, func(r *courseRecord) error { r.Course.Title = "lost"; r.Attempts[0].Feedback = "lost"; return sentinel }); !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			if persistent {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				db = open()
				defer db.Close()
				s = NewStore(db)
			}
			got, err := s.record(ctx, "course")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("roundtrip: %+v %v", got, err)
			}
			for _, a := range []bool{false, true} {
				id := "quiz"
				if a {
					id = "attempt"
				}
				courseID, err := s.locate(ctx, id, a)
				if err != nil || courseID != "course" {
					t.Fatalf("locate: %s %v", courseID, err)
				}
			}
			list, err := s.List(ctx)
			if err != nil || len(list) != 1 {
				t.Fatalf("list: %+v %v", list, err)
			}
			// Returned values never share storage with a caller.
			got.Course.Units[0].Topics[0].Class.Content = "external mutation"
			fresh, _ := s.Get(ctx, "course")
			if fresh.Units[0].Topics[0].Class.Content != "Lesson" {
				t.Fatal("store aliased caller")
			}
			if err := s.Delete(ctx, "course"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Get(ctx, "course"); err == nil {
				t.Fatal("deleted course exists")
			}
			for _, a := range []bool{false, true} {
				id := "quiz"
				if a {
					id = "attempt"
				}
				if _, err := s.locate(ctx, id, a); err == nil {
					t.Fatal("orphaned child")
				}
			}
			if err := s.Delete(ctx, "course"); err == nil {
				t.Fatal("missing delete succeeded")
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := s.List(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestClassroomMigrationUpgradeAndChronologicalHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgraded.db")
	previous := migrations.New()
	for _, m := range migrations.Main().Ordered() {
		if m.Version < 10 {
			if err := previous.Register(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	db, err := store.Open(ctx, path, store.DefaultOptions(), previous)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewStore(db)
	early := time.Date(2026, 11, 3, 12, 0, 0, 100000000, time.UTC)
	later := early.Add(100 * time.Nanosecond)
	_, err = s.mutate(ctx, "course", true, func(r *courseRecord) error {
		r.Course = Course{ID: "course", Title: "Upgraded", UpdatedAt: early}
		r.Quizzes = []Quiz{{ID: "quiz-early", CourseID: "course", CreatedAt: early}, {ID: "quiz-later", CourseID: "course", CreatedAt: later}}
		r.Attempts = []Attempt{{ID: "attempt-early", QuizID: "quiz-early", CreatedAt: early}, {ID: "attempt-later", QuizID: "quiz-later", CreatedAt: later}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.record(ctx, "course")
	if err != nil {
		t.Fatal(err)
	}
	if record.Quizzes[0].ID != "quiz-early" || record.Attempts[0].ID != "attempt-early" {
		t.Fatal("fractional timestamp sorting changed history order", record)
	}
}
