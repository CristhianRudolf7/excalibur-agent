// SQL-backed workspace persistence (Sections 9.2, 16.2, 16.4, 13.7).
//
// The in-memory structs stay the working set and the API: Registry,
// LeaseManager, ModeStore, IntegrationQueue and WorktreeManager keep
// their logic, and each gains an optional SQL mirror. Mutations write
// the map first (the fast path stays authoritative in-process) and
// then the porter; Restore loads the mirror back after a restart, so
// a crash loses at most the write in flight. History is different:
// SQLHistory is a standalone store with the same API as History,
// backed by file_versions rows and the content-addressed blob store,
// so preimages survive a kill between the intent and the effect
// (Section 13.7).
package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Reader is the read half the SQL stores need: satisfied by
// *store.DB and by test fakes.
type Reader interface {
	Read(ctx context.Context, fn store.ReadFunc) error
}

func bg() context.Context { return context.Background() }

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func fromMs(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func isNotFound(err error) bool { return err == sql.ErrNoRows }

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"encode_error":%q}`, err.Error())
	}
	return string(raw)
}

// --- Projects ---

// SQLProjectStore persists Registry rows in the Appendix A projects
// table. It mirrors Registry validation and uniqueness rules so the
// mirror and the map never disagree about what is legal.
type SQLProjectStore struct {
	W store.Writer
	R Reader
}

// OpenProjectStore returns a project store over db.
func OpenProjectStore(db *store.DB) *SQLProjectStore { return &SQLProjectStore{W: db, R: db} }

func scanProject(scan func(...any) error) (Project, error) {
	var p Project
	var id, name, alias, desc, rootsRaw, status, vcs, settingsRaw, groupsRaw, agentsRaw string
	var created, updated int64
	var scope, principal string
	if err := scan(&id, &name, &alias, &desc, &rootsRaw, &status, &vcs, &settingsRaw,
		&groupsRaw, &agentsRaw, &created, &updated, &scope, &principal); err != nil {
		return Project{}, err
	}
	p.ID = types.ProjectID(id)
	p.Name = name
	p.Alias = alias
	p.Description = desc
	_ = json.Unmarshal([]byte(rootsRaw), &p.Roots)
	p.Status = ProjectStatus(status)
	p.VCS = vcs
	_ = json.Unmarshal([]byte(settingsRaw), &p.Settings)
	_ = json.Unmarshal([]byte(groupsRaw), &p.SkillGroups)
	_ = json.Unmarshal([]byte(agentsRaw), &p.Agents)
	p.CreatedAt = fromMs(created)
	p.UpdatedAt = fromMs(updated)
	return p, nil
}

const projectCols = `id,name,alias,description,roots_json,status,vcs,settings_json,skill_groups_json,agents_json,created_at,updated_at,scope_id,principal_id`

// Register inserts a project after the Registry validation callers
// already ran: canonical roots, valid shape, unique name and alias
// (case-insensitive, like Registry).
func (s *SQLProjectStore) Register(p Project) error {
	var err error
	derr := s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		var dup string
		qerr := ex.QueryRow(ctx, `SELECT id FROM projects WHERE id=?`, string(p.ID)).Scan(&dup)
		if qerr == nil {
			return fmt.Errorf("workspace: duplicate project id %q", string(p.ID))
		}
		if qerr != nil && !isNotFound(qerr) {
			return qerr
		}
		rows, qerr := ex.Query(ctx, `SELECT id,name,alias FROM projects`)
		if qerr != nil {
			return qerr
		}
		defer rows.Close()
		for rows.Next() {
			var id, name, alias string
			if qerr := rows.Scan(&id, &name, &alias); qerr != nil {
				return qerr
			}
			if strings.EqualFold(name, p.Name) {
				return fmt.Errorf("workspace: duplicate project name %q", p.Name)
			}
			if p.Alias != "" && alias != "" && strings.EqualFold(alias, p.Alias) {
				return fmt.Errorf("workspace: duplicate project alias %q", p.Alias)
			}
		}
		if qerr := rows.Err(); qerr != nil {
			return qerr
		}
		_, qerr = ex.Exec(ctx, `INSERT INTO projects(`+projectCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			string(p.ID), p.Name, p.Alias, p.Description, mustJSON(p.Roots), string(p.Status),
			p.VCS, mustJSON(p.Settings), mustJSON(p.SkillGroups), mustJSON(p.Agents),
			ms(p.CreatedAt), ms(p.UpdatedAt), "project:"+string(p.ID), "")
		return qerr
	})
	if derr != nil {
		err = derr
	}
	return err
}

// Update replaces the mutable fields of a project, like
// Registry.Update: same validation, same uniqueness scope.
func (s *SQLProjectStore) Update(p Project) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		rows, err := ex.Query(ctx, `SELECT id,name,alias FROM projects`)
		if err != nil {
			return err
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var id, name, alias string
			if err := rows.Scan(&id, &name, &alias); err != nil {
				return err
			}
			if id == string(p.ID) {
				found = true
				continue
			}
			if strings.EqualFold(name, p.Name) {
				return fmt.Errorf("workspace: duplicate project name %q", p.Name)
			}
			if p.Alias != "" && alias != "" && strings.EqualFold(alias, p.Alias) {
				return fmt.Errorf("workspace: duplicate project alias %q", p.Alias)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("workspace: unknown project %q", string(p.ID))
		}
		_, err = ex.Exec(ctx, `UPDATE projects SET name=?,alias=?,description=?,roots_json=?,status=?,vcs=?,
			settings_json=?,skill_groups_json=?,agents_json=?,updated_at=? WHERE id=?`,
			p.Name, p.Alias, p.Description, mustJSON(p.Roots), string(p.Status), p.VCS,
			mustJSON(p.Settings), mustJSON(p.SkillGroups), mustJSON(p.Agents), ms(p.UpdatedAt), string(p.ID))
		return err
	})
}

// Archive marks a project archived.
func (s *SQLProjectStore) Archive(id types.ProjectID, at time.Time) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `UPDATE projects SET status='archived',updated_at=? WHERE id=?`, ms(at), string(id))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("workspace: unknown project %q", string(id))
		}
		return nil
	})
}

// Get returns one project.
func (s *SQLProjectStore) Get(id types.ProjectID) (Project, error) {
	var out Project
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		var err error
		out, err = scanProject(func(dest ...any) error {
			return q.QueryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE id=?`, string(id)).Scan(dest...)
		})
		if err != nil {
			if isNotFound(err) {
				return fmt.Errorf("workspace: unknown project %q", string(id))
			}
			return err
		}
		return nil
	})
	return out, err
}

// List returns all projects sorted by name.
func (s *SQLProjectStore) List() []Project {
	var out []Project
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+projectCols+` FROM projects ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanProject(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out
}

// AttachProjectStore wires the SQL mirror into the registry: every
// Register/Update/Archive writes through, and RestoreProjects reloads
// the mirror after a restart. Production builds the registry with a
// store attached; unit tests use the bare registry.
func (r *Registry) AttachProjectStore(s *SQLProjectStore) { r.persist = s }

// RestoreProjects replaces the in-memory set with the mirror rows.
func (r *Registry) RestoreProjects() error {
	if r.persist == nil {
		return fmt.Errorf("workspace: no project store attached")
	}
	rows := r.persist.List()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects = map[types.ProjectID]*Project{}
	for _, p := range rows {
		cp := p
		cp.Roots = append([]string(nil), p.Roots...)
		r.projects[cp.ID] = &cp
	}
	return nil
}

// --- Leases ---

// SQLLeaseStore mirrors LeaseManager rows in the Appendix A leases
// table for crash recovery: unexpired rows come back on Restore.
type SQLLeaseStore struct {
	W store.Writer
	R Reader
}

// OpenLeaseStore returns a lease mirror over db.
func OpenLeaseStore(db *store.DB) *SQLLeaseStore { return &SQLLeaseStore{W: db, R: db} }

// Grant records or renews a lease row when still held by nobody else
// or expired: the SQL mirror of the in-memory grant.
func (s *SQLLeaseStore) Grant(path, holder, activity string, expires time.Time) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO leases(path,holder,activity,expires_ms,created_at,updated_at)
			VALUES(?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET holder=excluded.holder,activity=excluded.activity,
			expires_ms=excluded.expires_ms,updated_at=excluded.updated_at`,
			path, holder, activity, expires.UTC().UnixMilli(), ms(time.Now().UTC()), ms(time.Now().UTC()))
		return err
	})
}

// ReleaseRow deletes the row when still held by holder.
func (s *SQLLeaseStore) ReleaseRow(path, holder string) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `DELETE FROM leases WHERE path=? AND holder=?`, path, holder)
		return err
	})
}

// ReleaseHolder deletes every row of holder, returning the count.
func (s *SQLLeaseStore) ReleaseHolder(holder string) (int, error) {
	var n int
	err := s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `DELETE FROM leases WHERE holder=?`, holder)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		n = int(affected)
		return nil
	})
	return n, err
}

// HeartbeatRow renews every row of holder, returning the count.
func (s *SQLLeaseStore) HeartbeatRow(holder string, expires time.Time) (int, error) {
	var n int
	err := s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `UPDATE leases SET expires_ms=?,updated_at=? WHERE holder=?`,
			expires.UTC().UnixMilli(), ms(time.Now().UTC()), holder)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		n = int(affected)
		return nil
	})
	return n, err
}

// ExpireRows deletes rows past their deadline, returning the count.
func (s *SQLLeaseStore) ExpireRows(now time.Time) (int, error) {
	var n int
	err := s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `DELETE FROM leases WHERE expires_ms<=?`, now.UTC().UnixMilli())
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		n = int(affected)
		return nil
	})
	return n, err
}

// Unexpired lists live rows ordered by path.
func (s *SQLLeaseStore) Unexpired(now time.Time) []LeaseSnapshot {
	var out []LeaseSnapshot
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT path,holder,activity,expires_ms FROM leases WHERE expires_ms>? ORDER BY path`, now.UTC().UnixMilli())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var snap LeaseSnapshot
			var expires int64
			if err := rows.Scan(&snap.Path, &snap.Holder, &snap.Activity, &expires); err != nil {
				return err
			}
			snap.Expires = fromMs(expires)
			snap.At = now
			out = append(out, snap)
		}
		return rows.Err()
	})
	return out
}

// persistLease writes the mirror after an in-memory grant. It runs
// without the manager lock; a failure rolls the map entry back when
// still ours so the two never disagree for long.
func (m *LeaseManager) persistLease(path, holder string) {
	if m.leaseSQL == nil {
		return
	}
	m.mu.Lock()
	e, ok := m.leases[path]
	m.mu.Unlock()
	if !ok {
		return
	}
	if err := m.leaseSQL.Grant(path, e.holder, e.activity, e.expires); err != nil {
		m.mu.Lock()
		if cur, still := m.leases[path]; still && cur.holder == holder {
			delete(m.leases, path)
		}
		m.mu.Unlock()
	}
}

// AttachLeaseStore wires the SQL mirror: grants, releases,
// heartbeats and expiries write through, and RestoreLeases reloads
// live rows after a restart.
func (m *LeaseManager) AttachLeaseStore(s *SQLLeaseStore) { m.leaseSQL = s }

// RestoreLeases loads unexpired mirror rows into the map.
func (m *LeaseManager) RestoreLeases() {
	if m.leaseSQL == nil {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leases == nil {
		m.leases = map[string]leaseEntry{}
	}
	for _, snap := range m.leaseSQL.Unexpired(now) {
		m.leases[snap.Path] = leaseEntry{holder: snap.Holder, activity: snap.Activity, expires: snap.Expires}
	}
}

// --- Modes ---

// SQLModeStore persists ModeStore overrides in project_modes.
type SQLModeStore struct {
	W store.Writer
	R Reader
}

// OpenModeStore returns a mode store over db with every saved
// override loaded. Use it instead of the zero ModeStore in
// production; unit tests keep the zero value.
func OpenModeStore(db *store.DB) (*ModeStore, error) {
	s := &SQLModeStore{W: db, R: db}
	overrides := map[string]WorkMode{}
	err := s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT project_id,mode FROM project_modes`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, mode string
			if err := rows.Scan(&id, &mode); err != nil {
				return err
			}
			if mode == string(ModeShared) || mode == string(ModeWorktrees) {
				overrides[id] = WorkMode(mode)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return &ModeStore{overrides: overrides, sql: s}, nil
}

// save persists one override.
func (s *SQLModeStore) save(projectID string, mode WorkMode) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO project_modes(project_id,mode,updated_at) VALUES(?,?,?)
			ON CONFLICT(project_id) DO UPDATE SET mode=excluded.mode,updated_at=excluded.updated_at`,
			projectID, string(mode), ms(time.Now().UTC()))
		return err
	})
}

// --- Integration queue ---

// SQLQueueStore persists IntegrationQueue items keyed by the
// integration branch (one queue per task) in integration_queue.
type SQLQueueStore struct {
	W store.Writer
	R Reader
}

// OpenQueueStore returns a queue mirror over db.
func OpenQueueStore(db *store.DB) *SQLQueueStore { return &SQLQueueStore{W: db, R: db} }

// EnqueueRow adds a queued item unless the branch is already queued.
func (s *SQLQueueStore) EnqueueRow(queueID, childID, branch string, enqueued time.Time) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO integration_queue(queue_id,child_id,branch,status,enqueued_ms,report,attempted)
			VALUES(?,?,?,?,?,?,?) ON CONFLICT(queue_id,child_id) DO NOTHING`,
			queueID, childID, branch, string(QueuePending), enqueued.UTC().UnixMilli(), "", 0)
		return err
	})
}

// SetRow records the outcome of one processed item.
func (s *SQLQueueStore) SetRow(queueID, childID string, status QueueStatus, report string, attempted bool) error {
	attemptedInt := 0
	if attempted {
		attemptedInt = 1
	}
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `UPDATE integration_queue SET status=?,report=?,attempted=? WHERE queue_id=? AND child_id=?`,
			string(status), report, attemptedInt, queueID, childID)
		return err
	})
}

// ListRows returns the queue in enqueue order.
func (s *SQLQueueStore) ListRows(queueID string) []QueueItem {
	var out []QueueItem
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT child_id,branch,status,enqueued_ms,report,attempted FROM integration_queue WHERE queue_id=? ORDER BY rowid`, queueID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it QueueItem
			var status string
			var enqueued int64
			var attempted int
			if err := rows.Scan(&it.ChildID, &it.Branch, &status, &enqueued, &it.Report, &attempted); err != nil {
				return err
			}
			it.Status = QueueStatus(status)
			it.Enqueued = fromMs(enqueued)
			it.Attempted = attempted != 0
			out = append(out, it)
		}
		return rows.Err()
	})
	return out
}

// queueIDOf keys the mirror by the integration branch: one queue per
// task (Section 16.4 point 3).
func (q *IntegrationQueue) queueIDOf() string { return q.integration }

// AttachQueueStore wires the SQL mirror: enqueues and outcomes write
// through, and RestoreQueue reloads the queue after a restart.
func (q *IntegrationQueue) AttachQueueStore(s *SQLQueueStore) { q.persist = s }

// RestoreQueue reloads mirror rows into the queue.
func (q *IntegrationQueue) RestoreQueue() {
	if q.persist == nil {
		return
	}
	rows := q.persist.ListRows(q.queueIDOf())
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = q.items[:0]
	for _, it := range rows {
		cp := it
		q.items = append(q.items, &cp)
	}
}

// persistEnqueue mirrors one enqueue; best-effort after the map.
func (q *IntegrationQueue) persistEnqueue(item *QueueItem) {
	if q.persist == nil {
		return
	}
	_ = q.persist.EnqueueRow(q.queueIDOf(), item.ChildID, item.Branch, item.Enqueued)
}

// persistOutcome mirrors one processed outcome; best-effort.
func (q *IntegrationQueue) persistOutcome(item *QueueItem) {
	if q.persist == nil {
		return
	}
	_ = q.persist.SetRow(q.queueIDOf(), item.ChildID, item.Status, item.Report, item.Attempted)
}

// --- Worktrees ---

// SQLWorktreeIndex records created worktrees outside the project root
// so a restart finds orphans the git worktree list may still show
// (Section 16.4 point 1).
type SQLWorktreeIndex struct {
	W store.Writer
	R Reader
}

// OpenWorktreeIndex returns a worktree index over db.
func OpenWorktreeIndex(db *store.DB) *SQLWorktreeIndex { return &SQLWorktreeIndex{W: db, R: db} }

// Record remembers one created worktree.
func (s *SQLWorktreeIndex) Record(projectID, childID, path, branch, baseCommit string) error {
	now := ms(time.Now().UTC())
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO worktrees(project_id,child_id,path,branch,base_commit,status,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(project_id,child_id) DO UPDATE SET path=excluded.path,
			branch=excluded.branch,base_commit=excluded.base_commit,status='active',updated_at=excluded.updated_at`,
			projectID, childID, path, branch, baseCommit, "active", now, now)
		return err
	})
}

// Forget drops one removed worktree.
func (s *SQLWorktreeIndex) Forget(projectID, childID string) error {
	return s.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `DELETE FROM worktrees WHERE project_id=? AND child_id=?`, projectID, childID)
		return err
	})
}

// WorktreeRecord is one indexed worktree.
type WorktreeRecord struct {
	ProjectID  string
	ChildID    string
	Path       string
	Branch     string
	BaseCommit string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// List returns the indexed worktrees of a project.
func (s *SQLWorktreeIndex) List(projectID string) []WorktreeRecord {
	var out []WorktreeRecord
	_ = s.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT child_id,path,branch,base_commit,status,created_at,updated_at FROM worktrees WHERE project_id=? ORDER BY child_id`, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rec WorktreeRecord
			var created, updated int64
			if err := rows.Scan(&rec.ChildID, &rec.Path, &rec.Branch, &rec.BaseCommit, &rec.Status, &created, &updated); err != nil {
				return err
			}
			rec.ProjectID = projectID
			rec.CreatedAt = fromMs(created)
			rec.UpdatedAt = fromMs(updated)
			out = append(out, rec)
		}
		return rows.Err()
	})
	return out
}

// AttachWorktreeIndex wires the index: CreateChild records, and
// RemoveChild forgets. The git worktree list stays authoritative for
// what exists on disk; the index is the crash-recovery trail.
func (m *WorktreeManager) AttachWorktreeIndex(s *SQLWorktreeIndex) { m.index = s }

// --- Edit history ---

// SQLHistory persists FileVersions in file_versions with blob content
// in the store blob store. It mirrors the History API so callers can
// swap the constructor: OpenSQLHistory in production, NewHistory in
// unit tests.
type SQLHistory struct {
	W     store.Writer
	R     Reader
	Blobs store.BlobStore

	mu      sync.Mutex
	counter uint64
	now     func() time.Time
}

// OpenSQLHistory returns an edit history over db with content in the
// db blob store.
func OpenSQLHistory(db *store.DB) *SQLHistory {
	return &SQLHistory{W: db, R: db, Blobs: db.Blob(), now: time.Now}
}

func historyBlobRef(digest string) string { return "blob-sha256:" + digest }

func parseHistoryRef(ref string) (string, bool) {
	d, ok := strings.CutPrefix(ref, "blob-sha256:")
	if !ok || d == "" {
		return "", false
	}
	return d, true
}

// PutBlob stores blob bytes and returns the content ref. Refs are
// content-derived ("blob-sha256:<hex>"), so a failed Put still
// returns the deterministic ref and a later write of the same content
// heals the row.
func (h *SQLHistory) PutBlob(data []byte) string {
	ctx := bg()
	if digest, err := h.Blobs.Put(ctx, bytes.NewReader(data)); err == nil {
		_ = h.Blobs.AddRef(ctx, digest)
		return historyBlobRef(digest)
	}
	sum := sha256.Sum256(data)
	return "blob-sha256:" + hex.EncodeToString(sum[:])
}

// Blob returns stored blob bytes.
func (h *SQLHistory) Blob(ref string) ([]byte, bool) {
	digest, ok := parseHistoryRef(ref)
	if !ok {
		return nil, false
	}
	rc, err := h.Blobs.Open(bg(), digest)
	if err != nil {
		return nil, false
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Record appends a version for path with its pre and post content,
// returning the version id.
func (h *SQLHistory) Record(path string, pre, post []byte, runID, toolCall string) string {
	h.mu.Lock()
	h.counter++
	n := h.counter
	h.mu.Unlock()
	at := time.Now().UTC()
	if h.now != nil {
		at = h.now().UTC()
	}
	id := fmt.Sprintf("ed_%d_%d", at.UnixNano(), n)
	preRef := h.PutBlob(pre)
	postRef := h.PutBlob(post)
	_ = h.W.Write(bg(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO file_versions(id,path,preimage_digest,postimage_digest,run_id,tool_call_id,created_at,scope_id)
			VALUES(?,?,?,?,?,?,?,?)`,
			id, filepath.Clean(path), preRef, postRef, runID, toolCall, at.UnixMilli(), "")
		return err
	})
	return id
}

// Versions lists the versions of path oldest first.
func (h *SQLHistory) Versions(path string) []FileVersion {
	var out []FileVersion
	_ = h.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,preimage_digest,postimage_digest,run_id,tool_call_id,created_at FROM file_versions WHERE path=? ORDER BY rowid`, filepath.Clean(path))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v FileVersion
			var created int64
			if err := rows.Scan(&v.ID, &v.Preimage, &v.Postimage, &v.RunID, &v.ToolCall, &created); err != nil {
				return err
			}
			v.Path = filepath.Clean(path)
			v.At = fromMs(created)
			out = append(out, v)
		}
		return rows.Err()
	})
	return out
}

// Get returns one version by edit id.
func (h *SQLHistory) Get(id string) (FileVersion, bool) {
	var out FileVersion
	var ok bool
	_ = h.R.Read(bg(), func(ctx context.Context, q store.Queryer) error {
		var created int64
		err := q.QueryRow(ctx, `SELECT path,preimage_digest,postimage_digest,run_id,tool_call_id,created_at FROM file_versions WHERE id=?`, id).
			Scan(&out.Path, &out.Preimage, &out.Postimage, &out.RunID, &out.ToolCall, &created)
		if err != nil {
			return err
		}
		out.ID = id
		out.At = fromMs(created)
		ok = true
		return nil
	})
	return out, ok
}

// Revert restores one edit without needing git (Section 13.7): same
// semantics as History.Revert, over durable rows.
func (h *SQLHistory) Revert(editID, runID, toolCall string) (string, error) {
	v, ok := h.Get(editID)
	if !ok {
		return "", fmt.Errorf("workspace: unknown edit %q", editID)
	}
	pre, okPre := h.Blob(v.Preimage)
	post, okPost := h.Blob(v.Postimage)
	if !okPre || !okPost {
		return "", fmt.Errorf("workspace: missing blobs for edit %q", editID)
	}
	cur, err := os.ReadFile(v.Path)
	if err != nil {
		return "", fmt.Errorf("workspace: read %s: %w", v.Path, err)
	}
	if string(cur) == string(post) {
		if err := atomicWrite(v.Path, pre); err != nil {
			return "", err
		}
		return h.Record(v.Path, cur, pre, runID, toolCall), nil
	}
	merged, conflicted, detail := Merge3(string(post), string(pre), string(cur))
	if conflicted {
		return "", &ConflictError{EditID: editID, Path: v.Path, Merged: merged, Conflict: detail}
	}
	if err := atomicWrite(v.Path, []byte(merged)); err != nil {
		return "", err
	}
	return h.Record(v.Path, cur, []byte(merged), runID, toolCall), nil
}
