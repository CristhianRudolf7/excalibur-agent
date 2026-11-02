package migrations

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// LibraryJobs is v7. Register after the library-core v6 migration.
func LibraryJobs() store.Migration {
	return store.Migration{Version: 7, Name: "library-jobs", Up: upV7}
}

func upV7(ctx context.Context, ex store.Execer) error {
	return execAll(ctx, ex, []string{
		`CREATE TABLE IF NOT EXISTS library_jobs (
 id TEXT PRIMARY KEY, doc_id TEXT NOT NULL UNIQUE, scope_id TEXT NOT NULL,
 blob_digest TEXT NOT NULL, state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 next_page INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 error TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS library_job_pages (
 job_id TEXT NOT NULL, page INTEGER NOT NULL, data_json TEXT NOT NULL,
 PRIMARY KEY(job_id,page))`,
		`CREATE INDEX IF NOT EXISTS idx_library_jobs_state ON library_jobs(state, created_at)`,
	})
}
