package store

import (
	"context"
	"database/sql"
)

// queryerConn adapts one pooled read connection to Queryer.
type queryerConn struct {
	conn *sql.Conn
}

func (q queryerConn) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := q.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (q queryerConn) QueryRow(ctx context.Context, query string, args ...any) Row {
	return q.conn.QueryRowContext(ctx, query, args...)
}

// txExecer adapts one group-commit transaction to Execer. One command
// runs inside one savepoint of the transaction (Section 4.3); the
// porter owns savepoint framing, this type only forwards calls.
type txExecer struct {
	tx *sql.Tx
}

func (e txExecer) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return e.tx.ExecContext(ctx, query, args...)
}

func (e txExecer) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := e.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (e txExecer) QueryRow(ctx context.Context, query string, args ...any) Row {
	return e.tx.QueryRowContext(ctx, query, args...)
}

// connExecer adapts a bare connection to Execer for migrations and
// setup, which manage their own transactions.
type connExecer struct {
	conn *sql.Conn
}

func (e connExecer) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return e.conn.ExecContext(ctx, query, args...)
}

func (e connExecer) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := e.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (e connExecer) QueryRow(ctx context.Context, query string, args ...any) Row {
	return e.conn.QueryRowContext(ctx, query, args...)
}
