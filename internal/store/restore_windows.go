package store

import (
	"database/sql"
	"errors"
)

// SQLite handles do not share delete access on Windows. Release both the
// pinned connection and its pool before replacing the corrupt database.
func closeBeforeRestore(conn *sql.Conn, db *sql.DB) error {
	connErr := conn.Close()
	return errors.Join(connErr, db.Close())
}
