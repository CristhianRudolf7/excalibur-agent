package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"time"
)

// Job is the durable extraction cursor. Attempts count failed attempts, so a
// shutdown never spends a retry. NextPage is the next OCR page to process.
type Job struct {
	ID, DocID, ScopeID, BlobDigest, State string
	Attempts, NextPage                    int
	CreatedAt, UpdatedAt                  time.Time
	Error                                 string
}

// Queue operations must be durable before returning. Claim atomically takes the
// oldest pending job; nil means there is currently no pending work.
type Queue interface {
	Put(context.Context, Job) error
	Recover(context.Context) error
	Claim(context.Context) (*Job, error)
	Save(context.Context, Job) error
	Get(context.Context, string) (Job, error)
}

type SQLQueue struct{ DB *store.DB }

func NewSQLQueue(db *store.DB) *SQLQueue { return &SQLQueue{DB: db} }
func (q *SQLQueue) Put(ctx context.Context, j Job) error {
	return q.DB.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO library_jobs(id,doc_id,scope_id,blob_digest,state,attempts,next_page,created_at,updated_at,error) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(doc_id) DO NOTHING`, j.ID, j.DocID, j.ScopeID, j.BlobDigest, j.State, j.Attempts, j.NextPage, j.CreatedAt.UnixMilli(), j.UpdatedAt.UnixMilli(), j.Error)
		return err
	})
}
func (q *SQLQueue) Recover(ctx context.Context) error {
	return q.DB.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `UPDATE library_jobs SET state='pending',updated_at=? WHERE state='processing'`, time.Now().UnixMilli())
		return err
	})
}

const jobColumns = "id,doc_id,scope_id,blob_digest,state,attempts,next_page,created_at,updated_at,error"

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var created, updated int64
	err := row.Scan(&j.ID, &j.DocID, &j.ScopeID, &j.BlobDigest, &j.State, &j.Attempts, &j.NextPage, &created, &updated, &j.Error)
	j.CreatedAt = time.UnixMilli(created)
	j.UpdatedAt = time.UnixMilli(updated)
	return j, err
}
func (q *SQLQueue) Claim(ctx context.Context) (*Job, error) {
	var out *Job
	err := q.DB.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		rows, err := ex.Query(ctx, `SELECT `+jobColumns+` FROM library_jobs WHERE state='pending' ORDER BY created_at,id LIMIT 1`)
		if err != nil {
			return err
		}
		if !rows.Next() {
			err = rows.Err()
			_ = rows.Close()
			return err
		}
		j, err := scanJob(rows)
		_ = rows.Close()
		if err != nil {
			return err
		}
		_, err = ex.Exec(ctx, `UPDATE library_jobs SET state='processing',updated_at=? WHERE id=? AND state='pending'`, time.Now().UnixMilli(), j.ID)
		if err != nil {
			return err
		}
		j.State = "processing"
		out = &j
		return nil
	})
	return out, err
}
func (q *SQLQueue) Save(ctx context.Context, j Job) error {
	return q.DB.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		// A cancellation is terminal; an in-flight worker cannot overwrite it.
		_, err := ex.Exec(ctx, `UPDATE library_jobs SET state=?,attempts=?,next_page=?,updated_at=?,error=? WHERE id=? AND state!='canceled'`, j.State, j.Attempts, j.NextPage, time.Now().UnixMilli(), j.Error, j.ID)
		return err
	})
}
func (q *SQLQueue) Get(ctx context.Context, id string) (Job, error) {
	var j Job
	err := q.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		j, err = scanJob(q.QueryRow(ctx, `SELECT `+jobColumns+` FROM library_jobs WHERE doc_id=?`, id))
		return err
	})
	if err != nil {
		return j, fmt.Errorf("library job %s: %w", id, err)
	}
	return j, nil
}

func (q *SQLQueue) LoadPage(ctx context.Context, id string, page int) (docextract.Page, bool, error) {
	var raw string
	err := q.DB.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT data_json FROM library_job_pages WHERE job_id=? AND page=?`, id, page).Scan(&raw)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return docextract.Page{}, false, nil
	}
	if err != nil {
		return docextract.Page{}, false, err
	}
	var p docextract.Page
	err = json.Unmarshal([]byte(raw), &p)
	return p, err == nil, err
}
func (q *SQLQueue) SavePage(ctx context.Context, id string, p docextract.Page) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.DB.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO library_job_pages(job_id,page,data_json) VALUES(?,?,?) ON CONFLICT(job_id,page) DO UPDATE SET data_json=excluded.data_json`, id, p.Number, string(raw))
		return err
	})
}
