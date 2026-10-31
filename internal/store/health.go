package store

import (
	"context"
	"database/sql"
	"strings"
)

// quickCheck runs PRAGMA quick_check after a crash (Section 4.3). It
// reports false when any row is not "ok".
func quickCheck(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, err := conn.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return false, err
		}
		if strings.TrimSpace(line) != "ok" {
			return false, nil
		}
	}
	return true, rows.Err()
}

// QuickCheck runs PRAGMA quick_check on the open database.
func (db *DB) QuickCheck(ctx context.Context) (bool, error) {
	var out bool
	err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		rows, err := q.Query(ctx, "PRAGMA quick_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		out = true
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			if strings.TrimSpace(line) != "ok" {
				out = false
			}
		}
		return rows.Err()
	})
	return out, err
}
