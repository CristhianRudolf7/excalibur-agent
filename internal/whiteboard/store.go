package whiteboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Store uses the existing porter for atomic board, version and op-log writes.
// A nil DB provides a run-local store for embedded runs without daemon storage.
type Store struct {
	DB     *store.DB
	mu     sync.Mutex
	boards map[string]string
	keys   map[string]int64
	order  []string
}

func NewStore(db *store.DB) *Store {
	return &Store{DB: db, boards: map[string]string{}, keys: map[string]int64{}}
}
func missing() error { return &api.Error{Code: api.CodeUnknownTarget, Message: "unknown whiteboard"} }
func (s *Store) Get(ctx context.Context, id string) (b Board, err error) {
	var data string
	if s.DB == nil {
		s.mu.Lock()
		data = s.boards[id]
		s.mu.Unlock()
		if data == "" {
			return b, missing()
		}
	} else {
		err = s.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
			return q.QueryRow(ctx, `SELECT data_json FROM whiteboards WHERE id=?`, id).Scan(&data)
		})
		if errors.Is(err, sql.ErrNoRows) {
			return b, missing()
		}
		if err != nil {
			return b, err
		}
	}
	err = json.Unmarshal([]byte(data), &b)
	return
}
func (s *Store) List(ctx context.Context, conv string) ([]BoardSummary, error) {
	out := []BoardSummary{}
	consume := func(data string) error {
		var b Board
		if err := json.Unmarshal([]byte(data), &b); err != nil {
			return err
		}
		if conv == "" || conv == b.ConversationID {
			out = append(out, b.Summary())
		}
		return nil
	}
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, id := range s.order {
			data := s.boards[id]
			if data == "" {
				continue
			}
			if err := consume(data); err != nil {
				return nil, err
			}
		}

	} else {
		err := s.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
			rows, err := q.Query(ctx, `SELECT data_json FROM whiteboards WHERE (?='' OR conversation_id=?) ORDER BY created_at,id`, conv, conv)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var data string
				if err := rows.Scan(&data); err != nil {
					return err
				}
				if err := consume(data); err != nil {
					return err
				}
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// mutate executes fn against the latest version inside the writer transaction.
// Idempotency lookup precedes version validation so retries return their original version.
func (s *Store) mutate(ctx context.Context, id, key, author string, ops any, fn func(*Board) error) (Board, bool, error) {
	var b Board
	var duplicate bool
	var err error
	run := func(data string, lookup func() (int64, bool, error), save func(string, int64) error) error {
		if key != "" {
			v, ok, e := lookup()
			if e != nil {
				return e
			}
			if ok {
				duplicate = true
				b.Version = v
				return nil
			}
		}
		if data != "" {
			if e := json.Unmarshal([]byte(data), &b); e != nil {
				return e
			}
		}
		if e := fn(&b); e != nil {
			return e
		}
		raw, e := json.Marshal(b)
		if e != nil {
			return e
		}
		return save(string(raw), b.Version)
	}
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		err = run(s.boards[id], func() (int64, bool, error) { v, ok := s.keys[id+"/"+key]; return v, ok, nil }, func(data string, v int64) error {
			if _, exists := s.boards[id]; !exists {
				s.order = append(s.order, id)
			}
			s.boards[id] = data
			if key != "" {
				s.keys[id+"/"+key] = v
			}
			return nil
		})
		return b, duplicate, err
	}
	err = s.DB.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		var data string
		e := ex.QueryRow(ctx, `SELECT data_json FROM whiteboards WHERE id=?`, id).Scan(&data)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		return run(data, func() (int64, bool, error) {
			var v int64
			e := ex.QueryRow(ctx, `SELECT version FROM whiteboard_ops WHERE board_id=? AND client_op_id=?`, id, key).Scan(&v)
			if errors.Is(e, sql.ErrNoRows) {
				return 0, false, nil
			}
			return v, e == nil, e
		}, func(data string, v int64) error {
			if _, e := ex.Exec(ctx, `INSERT INTO whiteboards(id,conversation_id,created_at,data_json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET data_json=excluded.data_json`, id, b.ConversationID, b.UpdatedAt.UnixNano(), data); e != nil {
				return e
			}
			if _, e := ex.Exec(ctx, `INSERT INTO whiteboard_versions(board_id,version,data_json) VALUES(?,?,?)`, id, v, data); e != nil {
				return e
			}
			log, err := json.Marshal(ops)
			if err != nil {
				return err
			}
			_, e := ex.Exec(ctx, `INSERT INTO whiteboard_ops(board_id,client_op_id,version,ops_json,author) VALUES(?,?,?,?,?)`, id, key, v, string(log), author)
			return e
		})
	})
	if err != nil {
		return Board{}, false, err
	}
	return b, duplicate, nil
}

// Delete removes the active board, retaining a final version and the audit log.
func (s *Store) Delete(ctx context.Context, id string) error {
	if s.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.boards[id] == "" {
			return missing()
		}
		delete(s.boards, id)
		return nil
	}
	return s.DB.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		var data string
		if err := ex.QueryRow(ctx, `SELECT data_json FROM whiteboards WHERE id=?`, id).Scan(&data); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return missing()
			}
			return err
		}
		var b Board
		if err := json.Unmarshal([]byte(data), &b); err != nil {
			return err
		}
		b.Version++
		b.UpdatedAt = time.Now().UTC()
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		if _, err = ex.Exec(ctx, `INSERT INTO whiteboard_versions(board_id,version,data_json) VALUES(?,?,?)`, id, b.Version, string(raw)); err != nil {
			return err
		}
		if _, err = ex.Exec(ctx, `INSERT INTO whiteboard_ops(board_id,client_op_id,version,ops_json,author) VALUES(?,?,?,?,?)`, id, newID(), b.Version, `[{"op":"delete"}]`, "agent"); err != nil {
			return err
		}
		_, err = ex.Exec(ctx, `DELETE FROM whiteboards WHERE id=?`, id)
		return err
	})
}

// Version loads an immutable checkpoint for replay, including deleted boards.
func (s *Store) Version(ctx context.Context, id string, version int64) (Board, error) {
	if s.DB == nil {
		return Board{}, missing()
	}
	var data string
	err := s.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT data_json FROM whiteboard_versions WHERE board_id=? AND version=?`, id, version).Scan(&data)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Board{}, missing()
	}
	if err != nil {
		return Board{}, err
	}
	var b Board
	err = json.Unmarshal([]byte(data), &b)
	return b, err
}
