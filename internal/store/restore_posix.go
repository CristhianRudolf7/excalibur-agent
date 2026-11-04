//go:build !windows

package store

import "database/sql"

// POSIX can replace the file while SQLite still holds its old inode.
func closeBeforeRestore(*sql.Conn, *sql.DB) error { return nil }
