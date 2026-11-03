package classroom

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Store persists course aggregates through the daemon's SQLite porter. A nil
// database supplies an isolated, in-memory store for embedded callers.
type Store struct {
	DB      *store.DB
	mu      sync.Mutex
	records map[string]string
}

type courseRecord struct {
	Course   Course
	Quizzes  []Quiz
	Attempts []Attempt
}

// Fixed-width UTC timestamps keep SQLite ordering chronological even when a
// timestamp ends in fractional zeroes.
const orderedTime = "2006-01-02T15:04:05.000000000Z"

func NewStore(db *store.DB) *Store { return &Store{DB: db, records: map[string]string{}} }
func missing() error {
	return &api.Error{Code: api.CodeUnknownTarget, Message: "unknown classroom target"}
}

type reader interface {
	Query(context.Context, string, ...any) (store.Rows, error)
	QueryRow(context.Context, string, ...any) store.Row
}

func loadRecord(ctx context.Context, q reader, id string) (r courseRecord, err error) {
	var data string
	if err = q.QueryRow(ctx, `SELECT data_json FROM classroom_courses WHERE id=?`, id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = missing()
		}
		return
	}
	if err = json.Unmarshal([]byte(data), &r.Course); err != nil {
		return
	}
	for _, table := range []string{"classroom_quizzes", "classroom_attempts"} {
		err = func() error {
			rows, e := q.Query(ctx, `SELECT data_json FROM `+table+` WHERE course_id=? ORDER BY created_at,id`, id)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				if e := rows.Scan(&data); e != nil {
					return e
				}
				if table == "classroom_quizzes" {
					var v Quiz
					if e := json.Unmarshal([]byte(data), &v); e != nil {
						return e
					}
					r.Quizzes = append(r.Quizzes, v)
				} else {
					var v Attempt
					if e := json.Unmarshal([]byte(data), &v); e != nil {
						return e
					}
					r.Attempts = append(r.Attempts, v)
				}
			}
			return rows.Err()
		}()
		if err != nil {
			return
		}
	}
	return
}

func (s *Store) record(ctx context.Context, id string) (r courseRecord, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		data := s.records[id]
		if data == "" {
			return r, missing()
		}
		err = json.Unmarshal([]byte(data), &r)
		return
	}
	err = s.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error { r, err = loadRecord(ctx, q, id); return err })
	return
}

func (s *Store) Get(ctx context.Context, id string) (Course, error) {
	r, err := s.record(ctx, id)
	return r.Course, err
}

func (s *Store) List(ctx context.Context) ([]Course, error) {
	out := []Course{}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, data := range s.records {
			var r courseRecord
			if err := json.Unmarshal([]byte(data), &r); err != nil {
				return nil, err
			}
			out = append(out, r.Course)
		}
	} else {
		err := s.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
			rows, err := q.Query(ctx, `SELECT data_json FROM classroom_courses`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var data string
				var c Course
				if err := rows.Scan(&data); err != nil {
					return err
				}
				if err := json.Unmarshal([]byte(data), &c); err != nil {
					return err
				}
				out = append(out, c)
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// mutate loads, validates and saves the entire change inside one porter write.
// Callback errors roll back the course, quizzes and attempts together.
func (s *Store) mutate(ctx context.Context, id string, create bool, fn func(*courseRecord) error) (r courseRecord, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		data := s.records[id]
		if data == "" && !create {
			return r, missing()
		}
		if data != "" {
			if err = json.Unmarshal([]byte(data), &r); err != nil {
				return
			}
		}
		if err = fn(&r); err != nil {
			return
		}
		var raw []byte
		raw, err = json.Marshal(r)
		if err == nil {
			s.records[id] = string(raw)
		}
		return
	}
	result := make(chan courseRecord, 1)
	err = s.DB.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		var r courseRecord
		if !create {
			var e error
			r, e = loadRecord(ctx, ex, id)
			if e != nil {
				return e
			}
		}
		if e := fn(&r); e != nil {
			return e
		}
		raw, e := json.Marshal(r.Course)
		if e != nil {
			return e
		}
		if _, e = ex.Exec(ctx, `INSERT INTO classroom_courses(id,updated_at,data_json) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at,data_json=excluded.data_json`, id, r.Course.UpdatedAt.UTC().Format(orderedTime), string(raw)); e != nil {
			return e
		}
		for _, quiz := range r.Quizzes {
			raw, e = json.Marshal(quiz)
			if e != nil {
				return e
			}
			if _, e = ex.Exec(ctx, `INSERT INTO classroom_quizzes(id,course_id,created_at,data_json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET data_json=excluded.data_json`, quiz.ID, id, quiz.CreatedAt.UTC().Format(orderedTime), string(raw)); e != nil {
				return e
			}
		}
		for _, attempt := range r.Attempts {
			raw, e = json.Marshal(attempt)
			if e != nil {
				return e
			}
			if _, e = ex.Exec(ctx, `INSERT INTO classroom_attempts(id,course_id,quiz_id,created_at,data_json) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET data_json=excluded.data_json`, attempt.ID, id, attempt.QuizID, attempt.CreatedAt.UTC().Format(orderedTime), string(raw)); e != nil {
				return e
			}
		}
		result <- r
		return nil
	})
	if err != nil {
		return courseRecord{}, err
	}
	r = <-result
	return
}

func (s *Store) locate(ctx context.Context, id string, attempt bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for courseID, data := range s.records {
			var r courseRecord
			if err := json.Unmarshal([]byte(data), &r); err != nil {
				return "", err
			}
			if attempt {
				for _, a := range r.Attempts {
					if a.ID == id {
						return courseID, nil
					}
				}
			} else {
				for _, q := range r.Quizzes {
					if q.ID == id {
						return courseID, nil
					}
				}
			}
		}
		return "", missing()
	}
	table := "classroom_quizzes"
	if attempt {
		table = "classroom_attempts"
	}
	var courseID string
	err := s.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT course_id FROM `+table+` WHERE id=?`, id).Scan(&courseID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = missing()
	}
	return courseID, err
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.records[id] == "" {
			return missing()
		}
		delete(s.records, id)
		return nil
	}
	return s.DB.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		// The porter does not require foreign_keys on every connection. Explicit
		// child deletion also keeps erasure atomic with that setting disabled.
		for _, table := range []string{"classroom_attempts", "classroom_quizzes"} {
			if _, err := ex.Exec(ctx, `DELETE FROM `+table+` WHERE course_id=?`, id); err != nil {
				return err
			}
		}
		result, err := ex.Exec(ctx, `DELETE FROM classroom_courses WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err == nil && n == 0 {
			return missing()
		}
		return err
	})
}
