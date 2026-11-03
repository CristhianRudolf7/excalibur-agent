package migrations

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func upV10(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE classroom_courses (id TEXT PRIMARY KEY, updated_at TEXT NOT NULL, data_json TEXT NOT NULL)`,
		`CREATE TABLE classroom_quizzes (id TEXT PRIMARY KEY, course_id TEXT NOT NULL REFERENCES classroom_courses(id) ON DELETE CASCADE, created_at TEXT NOT NULL, data_json TEXT NOT NULL)`,
		`CREATE INDEX idx_classroom_quizzes_course ON classroom_quizzes(course_id, created_at, id)`,
		`CREATE TABLE classroom_attempts (id TEXT PRIMARY KEY, course_id TEXT NOT NULL REFERENCES classroom_courses(id) ON DELETE CASCADE, quiz_id TEXT NOT NULL REFERENCES classroom_quizzes(id) ON DELETE CASCADE, created_at TEXT NOT NULL, data_json TEXT NOT NULL)`,
		`CREATE INDEX idx_classroom_attempts_course ON classroom_attempts(course_id, created_at, id)`,
	})
}
