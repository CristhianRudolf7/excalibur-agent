// SQL-backed planning persistence (Sections 8.2, 8.10, 4.4).
//
// SQLStore implements PlanningStore over the store porter: writes go
// on the normal lane (BEGIN IMMEDIATE, group commit), reads use the
// pooled connections. Every method runs one porter round trip with a
// background context: the PlanningStore interface predates wiring and
// carries no context, so cancellation applies at the caller, not at
// the row.
//
// Batch atomicity (Section 8.10): ApplyBatch runs the check/apply
// closures against a txPlanning bound to the single porter
// transaction, so validation and application share one commit. Short
// codes allocate from short_counters inside the same transaction with
// the same UPSERT the store package uses, so codes never repeat, not
// even across crashes (Section 4.4).
package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Reader is the read half an SQLStore needs: satisfied by *store.DB
// (Read/ReadInteractive) and by test fakes.
type Reader interface {
	Read(ctx context.Context, fn store.ReadFunc) error
}

// SQLStore persists planning rows in the Appendix A tables through
// the porter. The zero value is unusable; build it with OpenSQLStore.
// It is safe for concurrent use.
type SQLStore struct {
	W store.Writer
	R Reader
}

// OpenSQLStore returns a PlanningStore over db. Both halves use db:
// writes on the normal lane, reads from the pool.
func OpenSQLStore(db *store.DB) *SQLStore { return &SQLStore{W: db, R: db} }

// txDB is the statement surface one transaction offers: the porter
// Execer inside a write, or a read-only adapter inside a read.
type txDB interface {
	Exec(ctx context.Context, query string, args ...any) (store.Result, error)
	Query(ctx context.Context, query string, args ...any) (store.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) store.Row
}

// readOnly adapts a Queryer to txDB for read paths: Exec always
// fails, so a write through a read handle is a loud error, never a
// silent no-op.
type readOnly struct{ store.Queryer }

func (r readOnly) Exec(context.Context, string, ...any) (store.Result, error) {
	return nil, fmt.Errorf("tasks: write through a read handle")
}

// txPlanning implements PlanningStore over one txDB: inside a porter
// Write the handle is the transaction (batch path included), inside
// a Read it is a pooled connection.
type txPlanning struct{ db txDB }

// write runs fn in one porter transaction over a txPlanning.
func (s *SQLStore) write(fn func(tx *txPlanning) error) error {
	return s.W.Write(context.Background(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		return fn(&txPlanning{db: ex})
	})
}

// read runs fn on one pooled connection over a txPlanning.
func (s *SQLStore) read(fn func(tx *txPlanning) error) error {
	return s.R.Read(context.Background(), func(ctx context.Context, q store.Queryer) error {
		return fn(&txPlanning{db: readOnly{q}})
	})
}

// mustJSON encodes v for a *_json column. Encoding never fails for
// the shapes used here; on failure it records the error inline so
// the row stays inspectable instead of dropping the write.
func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"encode_error":%q}`, err.Error())
	}
	return string(raw)
}

func fromJSON(raw string, v any) error {
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), v)
}

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

// wrapExists maps a primary-key conflict to the same already-exists
// error the MemoryStore returns, so callers see one contract.
func wrapExists(err error, what string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf("tasks: %s already exists", what)
	}
	return err
}

// nextShortTx allocates one short code inside the caller's
// transaction: the same INSERT..ON CONFLICT..RETURNING the store
// package uses for short_counters, so concurrent batches never
// collide and discarded numbers are never filled (Section 4.4).
func (tx *txPlanning) nextShortTx(ctx context.Context, scopeID, series string) (int, error) {
	var n int
	row := tx.db.QueryRow(ctx, `INSERT INTO short_counters(scope_id,series,next_n) VALUES(?,?,2)
		ON CONFLICT(scope_id,series) DO UPDATE SET next_n=next_n+1
		RETURNING next_n-1`, scopeID, series)
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("tasks: allocate %s code: %w", series, err)
	}
	return n, nil
}

func bg() context.Context { return context.Background() }

func scopeID(s types.Scope) string { return s.String() }

func parseScope(raw string) types.Scope {
	s, err := types.ParseScope(raw)
	if err != nil {
		return types.GeneralScope()
	}
	return s
}

// --- Projects ---

// RegisterProject implements PlanningStore.
func (tx *txPlanning) RegisterProject(p Project) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO projects(id,name,alias,description,roots_json,status,vcs,settings_json,skill_groups_json,agents_json,created_at,updated_at,scope_id,principal_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(p.ID), p.Name, p.Alias, "", mustJSON(p.Roots), projectStatus(p), "", "{}", "[]", "[]",
		ms(p.CreatedAt), ms(p.UpdatedAt), "", "")
	return wrapExists(err, fmt.Sprintf("project %q", p.ID))
}

// GetProject implements PlanningStore.
func (tx *txPlanning) GetProject(id types.ProjectID) (Project, error) {
	ctx := bg()
	row := tx.db.QueryRow(ctx, `SELECT name,alias,roots_json,status,created_at,updated_at FROM projects WHERE id=?`, string(id))
	var name, alias, rootsRaw, status string
	var created, updated int64
	if err := row.Scan(&name, &alias, &rootsRaw, &status, &created, &updated); err != nil {
		if isNotFound(err) {
			return Project{}, fmt.Errorf("tasks: unknown project %q", id)
		}
		return Project{}, err
	}
	var roots []string
	_ = fromJSON(rootsRaw, &roots)
	return Project{ID: id, Name: name, Alias: alias, Roots: roots,
		Archived: status == "archived", CreatedAt: fromMs(created), UpdatedAt: fromMs(updated)}, nil
}

// UpdateProject implements PlanningStore.
func (tx *txPlanning) UpdateProject(p Project) error {
	ctx := bg()
	res, err := tx.db.Exec(ctx, `UPDATE projects SET name=?,alias=?,roots_json=?,status=?,updated_at=? WHERE id=?`,
		p.Name, p.Alias, mustJSON(p.Roots), projectStatus(p), ms(p.UpdatedAt), string(p.ID))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("tasks: unknown project %q", p.ID)
	}
	return nil
}

// ListProjects implements PlanningStore.
func (tx *txPlanning) ListProjects(includeArchived bool) []Project {
	ctx := bg()
	q := `SELECT id,name,alias,roots_json,status,created_at,updated_at FROM projects ORDER BY id`
	rows, err := tx.db.Query(ctx, q)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var id, name, alias, rootsRaw, status string
		var created, updated int64
		if err := rows.Scan(&id, &name, &alias, &rootsRaw, &status, &created, &updated); err != nil {
			return out
		}
		archived := status == "archived"
		if archived && !includeArchived {
			continue
		}
		var roots []string
		_ = fromJSON(rootsRaw, &roots)
		out = append(out, Project{ID: types.ProjectID(id), Name: name, Alias: alias,
			Roots: roots, Archived: archived, CreatedAt: fromMs(created), UpdatedAt: fromMs(updated)})
	}
	return out
}

func projectStatus(p Project) string {
	if p.Archived {
		return "archived"
	}
	return "active"
}

// --- Tasks ---

func taskRow(t types.Task) (args []any) {
	rootRun := ""
	if t.RootRunID != nil {
		rootRun = string(*t.RootRunID)
	}
	return []any{string(t.ID), string(t.Code), scopeID(t.Scope), string(t.Level),
		mustJSON(t.Request), string(t.ConversationID), string(t.NoticePolicy),
		rootRun, mustJSON(t.Budget), string(t.State), t.Version,
		ms(t.CreatedAt), ms(t.UpdatedAt), ""}
}

func scanTask(scan func(...any) error) (types.Task, error) {
	var t types.Task
	var id, code, scopeRaw, level, reqRaw, conv, notice, rootRun, budgetRaw, state string
	var version int
	var created, updated int64
	var principal string
	if err := scan(&id, &code, &scopeRaw, &level, &reqRaw, &conv, &notice, &rootRun, &budgetRaw, &state, &version, &created, &updated, &principal); err != nil {
		return types.Task{}, err
	}
	t.ID = types.TaskID(id)
	t.Code = types.TaskRef(code)
	t.Scope = parseScope(scopeRaw)
	t.Level = types.TaskLevel(level)
	_ = fromJSON(reqRaw, &t.Request)
	t.ConversationID = types.ConversationID(conv)
	t.NoticePolicy = types.NoticeClass(notice)
	if rootRun != "" {
		r := types.RunID(rootRun)
		t.RootRunID = &r
	}
	_ = fromJSON(budgetRaw, &t.Budget)
	t.State = types.TaskState(state)
	t.Version = version
	t.CreatedAt = fromMs(created)
	t.UpdatedAt = fromMs(updated)
	return t, nil
}

const taskCols = `id,short_code,scope_id,level,request_json,origin_conversation_id,notice_policy,root_run_id,budget_json,status,version,created_at,updated_at,principal_id`

// CreateTask implements PlanningStore.
func (tx *txPlanning) CreateTask(t types.Task) error {
	ctx := bg()
	args := taskRow(t)
	_, err := tx.db.Exec(ctx, `INSERT INTO tasks(`+taskCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	return wrapExists(err, fmt.Sprintf("task %q", t.ID))
}

// GetTask implements PlanningStore.
func (tx *txPlanning) GetTask(id types.TaskID) (types.Task, error) {
	ctx := bg()
	t, err := scanTask(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT `+taskCols+` FROM tasks WHERE id=?`, string(id)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Task{}, fmt.Errorf("tasks: unknown task %q", id)
		}
		return types.Task{}, err
	}
	return t, nil
}

// UpdateTask implements PlanningStore with optimistic concurrency.
func (tx *txPlanning) UpdateTask(t types.Task, wantVersion int) error {
	ctx := bg()
	rootRun := ""
	if t.RootRunID != nil {
		rootRun = string(*t.RootRunID)
	}
	res, err := tx.db.Exec(ctx, `UPDATE tasks SET short_code=?,scope_id=?,level=?,request_json=?,origin_conversation_id=?,
		notice_policy=?,root_run_id=?,budget_json=?,status=?,version=?,updated_at=? WHERE id=? AND version=?`,
		string(t.Code), scopeID(t.Scope), string(t.Level), mustJSON(t.Request), string(t.ConversationID),
		string(t.NoticePolicy), rootRun, mustJSON(t.Budget), string(t.State), wantVersion+1, ms(time.Now().UTC()),
		string(t.ID), wantVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		cur, gerr := tx.GetTask(t.ID)
		if gerr != nil {
			return gerr
		}
		return &VersionConflict{Entity: "task", ID: string(t.ID), Have: cur.Version, Want: wantVersion, Current: cur}
	}
	return nil
}

// SetTaskRun implements PlanningStore.
func (tx *txPlanning) SetTaskRun(id types.TaskID, run types.RunID, state types.TaskState) error {
	ctx := bg()
	res, err := tx.db.Exec(ctx, `UPDATE tasks SET root_run_id=?,status=?,version=version+1,updated_at=? WHERE id=?`,
		string(run), string(state), ms(time.Now().UTC()), string(id))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("tasks: unknown task %q", id)
	}
	return nil
}

// ReopenTask implements PlanningStore.
func (tx *txPlanning) ReopenTask(id types.TaskID, run types.RunID) error {
	ctx := bg()
	cur, err := tx.GetTask(id)
	if err != nil {
		return err
	}
	switch cur.State {
	case types.TaskCompleted, types.TaskCompletedPending, types.TaskFailed, types.TaskCanceled:
	default:
		return fmt.Errorf("tasks: task %q is %q, only finished tasks reopen", id, cur.State)
	}
	var incarn int64
	if err := tx.db.QueryRow(ctx, `SELECT COUNT(*) FROM task_history WHERE id=?`, string(id)).Scan(&incarn); err != nil {
		return err
	}
	args := taskRow(cur)
	if _, err := tx.db.Exec(ctx, `INSERT INTO task_history(id,incarnation,short_code,scope_id,level,request_json,origin_conversation_id,
		notice_policy,root_run_id,budget_json,status,version,created_at,updated_at,principal_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		append([]any{string(cur.ID), incarn + 1}, args[1:]...)...); err != nil {
		return err
	}
	res, err := tx.db.Exec(ctx, `UPDATE tasks SET root_run_id=?,status=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
		string(run), string(types.TaskOpen), ms(time.Now().UTC()), string(id), cur.Version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return &VersionConflict{Entity: "task", ID: string(id), Have: cur.Version, Want: cur.Version, Current: cur}
	}
	return nil
}

// TaskHistory returns the previous incarnations of a reopened task,
// oldest first. It is not part of PlanningStore; reports and the
// completion gate use it.
func (s *SQLStore) TaskHistory(id types.TaskID) ([]types.Task, error) {
	var out []types.Task
	err := s.read(func(tx *txPlanning) error {
		rows, err := tx.db.Query(bg(), `SELECT short_code,scope_id,level,request_json,origin_conversation_id,notice_policy,
			root_run_id,budget_json,status,version,created_at,updated_at,principal_id FROM task_history WHERE id=? ORDER BY incarnation`, string(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var code, scopeRaw, level, reqRaw, conv, notice, rootRun, budgetRaw, state string
			var version int
			var created, updated int64
			var principal string
			if err := rows.Scan(&code, &scopeRaw, &level, &reqRaw, &conv, &notice, &rootRun, &budgetRaw, &state, &version, &created, &updated, &principal); err != nil {
				return err
			}
			var t types.Task
			t.ID = id
			t.Code = types.TaskRef(code)
			t.Scope = parseScope(scopeRaw)
			t.Level = types.TaskLevel(level)
			_ = fromJSON(reqRaw, &t.Request)
			t.ConversationID = types.ConversationID(conv)
			t.NoticePolicy = types.NoticeClass(notice)
			if rootRun != "" {
				r := types.RunID(rootRun)
				t.RootRunID = &r
			}
			_ = fromJSON(budgetRaw, &t.Budget)
			t.State = types.TaskState(state)
			t.Version = version
			t.CreatedAt = fromMs(created)
			t.UpdatedAt = fromMs(updated)
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// ListTasks implements PlanningStore.
func (tx *txPlanning) ListTasks(scope types.Scope) []types.Task {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT `+taskCols+` FROM tasks WHERE scope_id=? ORDER BY id`, scopeID(scope))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, t)
	}
	return out
}

// --- Plans ---

func (tx *txPlanning) insertPlanRow(ctx context.Context, table string, p Plan) error {
	idCol := "id"
	if table == "plan_versions" {
		idCol = "plan_id"
	}
	_, err := tx.db.Exec(ctx, `INSERT INTO `+table+`(`+idCol+`,scope_id,type,task_id,version,status,created_at,updated_at,principal_id)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		p.ID, scopeID(p.Scope), string(p.Type), string(p.TaskID), p.Version, string(p.State),
		ms(p.CreatedAt), ms(p.UpdatedAt), "")
	return err
}

func scanPlan(scan func(...any) error) (Plan, error) {
	var p Plan
	var id, scopeRaw, typ, taskID, state string
	var version int
	var created, updated int64
	var principal string
	if err := scan(&id, &scopeRaw, &typ, &taskID, &version, &state, &created, &updated, &principal); err != nil {
		return Plan{}, err
	}
	p.ID = id
	p.Scope = parseScope(scopeRaw)
	p.Type = types.PlanType(typ)
	p.TaskID = types.TaskID(taskID)
	p.Version = version
	p.State = PlanState(state)
	p.CreatedAt = fromMs(created)
	p.UpdatedAt = fromMs(updated)
	return p, nil
}

// CreatePlan implements PlanningStore.
func (tx *txPlanning) CreatePlan(p Plan) error {
	ctx := bg()
	if err := wrapExists(tx.insertPlanRow(ctx, "plans", p), fmt.Sprintf("plan %q", p.ID)); err != nil {
		return err
	}
	return wrapExists(tx.insertPlanRow(ctx, "plan_versions", p), fmt.Sprintf("plan %q", p.ID))
}

// GetPlan implements PlanningStore: the latest version.
func (tx *txPlanning) GetPlan(id string) (Plan, error) {
	ctx := bg()
	p, err := scanPlan(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT id,scope_id,type,task_id,version,status,created_at,updated_at,principal_id FROM plans WHERE id=?`, id).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return Plan{}, fmt.Errorf("tasks: unknown plan %q", id)
		}
		return Plan{}, err
	}
	return p, nil
}

// UpdatePlan implements PlanningStore: the update lands as a new
// version, keeping the previous one queryable.
func (tx *txPlanning) UpdatePlan(p Plan, wantVersion int) error {
	ctx := bg()
	cur, err := tx.GetPlan(p.ID)
	if err != nil {
		return err
	}
	if cur.Version != wantVersion {
		return &VersionConflict{Entity: "plan", ID: p.ID, Have: cur.Version, Want: wantVersion, Current: cur}
	}
	p.Version = cur.Version + 1
	p.UpdatedAt = time.Now().UTC()
	if err := tx.insertPlanRow(ctx, "plan_versions", p); err != nil {
		return wrapExists(err, fmt.Sprintf("plan %q", p.ID))
	}
	res, err := tx.db.Exec(ctx, `UPDATE plans SET scope_id=?,type=?,task_id=?,version=?,status=?,updated_at=? WHERE id=? AND version=?`,
		scopeID(p.Scope), string(p.Type), string(p.TaskID), p.Version, string(p.State), ms(p.UpdatedAt), p.ID, wantVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return &VersionConflict{Entity: "plan", ID: p.ID, Have: cur.Version, Want: wantVersion, Current: cur}
	}
	return nil
}

// PlanVersions implements PlanningStore.
func (tx *txPlanning) PlanVersions(planID string) []Plan {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT plan_id,scope_id,type,task_id,version,status,created_at,updated_at,principal_id FROM plan_versions WHERE plan_id=? ORDER BY version`, planID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, p)
	}
	return out
}

// ListPlans implements PlanningStore.
func (tx *txPlanning) ListPlans(scope types.Scope, typ types.PlanType) []Plan {
	ctx := bg()
	q := `SELECT id,scope_id,type,task_id,version,status,created_at,updated_at,principal_id FROM plans WHERE scope_id=?`
	args := []any{scopeID(scope)}
	if typ != "" {
		q += ` AND type=?`
		args = append(args, string(typ))
	}
	q += ` ORDER BY id`
	rows, err := tx.db.Query(ctx, q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, p)
	}
	return out
}

// --- Plan items ---

func (tx *txPlanning) AddPlanItem(item PlanItem) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO plan_items(id,short_code,plan_id,task_id,title,detail,status,priority,agent_id,estimate_json,version,created_at,updated_at,scope_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Code, item.PlanID, string(item.TaskID), item.Title, item.Detail,
		string(item.State), string(item.Priority), item.Agent, mustJSON(map[string]int64{"ms": item.Estimate.Milliseconds()}),
		item.Version, ms(item.CreatedAt), ms(item.UpdatedAt), "")
	return wrapExists(err, fmt.Sprintf("plan item %q", item.ID))
}

func scanPlanItem(scan func(...any) error) (PlanItem, error) {
	var it PlanItem
	var id, code, planID, taskID, title, detail, state, priority, agent, estRaw string
	var version int
	var created, updated int64
	var scope string
	if err := scan(&id, &code, &planID, &taskID, &title, &detail, &state, &priority, &agent, &estRaw, &version, &created, &updated, &scope); err != nil {
		return PlanItem{}, err
	}
	it.ID = id
	it.Code = code
	it.PlanID = planID
	it.TaskID = types.TaskID(taskID)
	it.Title = title
	it.Detail = detail
	it.State = types.PlanItemState(state)
	it.Priority = types.Priority(priority)
	it.Agent = agent
	var est struct {
		MS int64 `json:"ms"`
	}
	_ = fromJSON(estRaw, &est)
	it.Estimate = time.Duration(est.MS) * time.Millisecond
	it.Version = version
	it.CreatedAt = fromMs(created)
	it.UpdatedAt = fromMs(updated)
	return it, nil
}

// GetPlanItem implements PlanningStore.
func (tx *txPlanning) GetPlanItem(id string) (PlanItem, error) {
	ctx := bg()
	it, err := scanPlanItem(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT id,short_code,plan_id,task_id,title,detail,status,priority,agent_id,estimate_json,version,created_at,updated_at,scope_id FROM plan_items WHERE id=?`, id).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return PlanItem{}, fmt.Errorf("tasks: unknown plan item %q", id)
		}
		return PlanItem{}, err
	}
	return it, nil
}

// UpdatePlanItem implements PlanningStore with optimistic concurrency.
func (tx *txPlanning) UpdatePlanItem(item PlanItem, wantVersion int) error {
	ctx := bg()
	res, err := tx.db.Exec(ctx, `UPDATE plan_items SET short_code=?,title=?,detail=?,status=?,priority=?,agent_id=?,estimate_json=?,version=?,updated_at=? WHERE id=? AND version=?`,
		item.Code, item.Title, item.Detail, string(item.State), string(item.Priority), item.Agent,
		mustJSON(map[string]int64{"ms": item.Estimate.Milliseconds()}), wantVersion+1, ms(time.Now().UTC()), item.ID, wantVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		cur, gerr := tx.GetPlanItem(item.ID)
		if gerr != nil {
			return gerr
		}
		return &VersionConflict{Entity: "plan_item", ID: item.ID, Have: cur.Version, Want: wantVersion, Current: cur}
	}
	return nil
}

// ListPlanItems implements PlanningStore in insertion order.
func (tx *txPlanning) ListPlanItems(planID string) []PlanItem {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT id,short_code,plan_id,task_id,title,detail,status,priority,agent_id,estimate_json,version,created_at,updated_at,scope_id FROM plan_items WHERE plan_id=? ORDER BY rowid`, planID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []PlanItem
	for rows.Next() {
		it, err := scanPlanItem(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, it)
	}
	return out
}

// --- Dependencies ---

// AddDependency implements PlanningStore.
func (tx *txPlanning) AddDependency(planID string, d Dependency) error {
	ctx := bg()
	if _, err := tx.GetPlanItem(d.From); err != nil {
		return fmt.Errorf("tasks: dependency from unknown item %q", d.From)
	}
	if _, err := tx.GetPlanItem(d.To); err != nil {
		return fmt.Errorf("tasks: dependency to unknown item %q", d.To)
	}
	nodes := []string{}
	for _, it := range tx.ListPlanItems(planID) {
		nodes = append(nodes, it.ID)
	}
	if WouldCycle(nodes, tx.ListDependencies(planID), d.From, d.To) {
		return fmt.Errorf("tasks: dependency %q -> %q would close a cycle", d.From, d.To)
	}
	_, err := tx.db.Exec(ctx, `INSERT INTO dependencies(from_id,to_id,kind) VALUES(?,?,?)
		ON CONFLICT(from_id,to_id) DO UPDATE SET kind=excluded.kind`, d.From, d.To, string(d.Type))
	return err
}

// ListDependencies implements PlanningStore.
func (tx *txPlanning) ListDependencies(planID string) []Dependency {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT d.from_id,d.to_id,d.kind FROM dependencies d
		JOIN plan_items i ON i.id=d.from_id WHERE i.plan_id=? ORDER BY d.from_id,d.to_id`, planID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Dependency
	for rows.Next() {
		var d Dependency
		var kind string
		if err := rows.Scan(&d.From, &d.To, &kind); err != nil {
			return out
		}
		d.Type = types.DependencyType(kind)
		out = append(out, d)
	}
	return out
}

// --- Milestones ---

// AddMilestone implements PlanningStore.
func (tx *txPlanning) AddMilestone(mil Milestone) error {
	ctx := bg()
	status := "open"
	if mil.Done {
		status = "done"
	}
	_, err := tx.db.Exec(ctx, `INSERT INTO milestones(id,plan_id,short_code,title,criteria_json,target_date_ms,status,created_at,updated_at,scope_id)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		mil.ID, mil.PlanID, string(mil.Code), mil.Title, mustJSON(mil.Criteria), ms(mil.TargetDate),
		status, ms(mil.CreatedAt), ms(mil.UpdatedAt), "")
	return wrapExists(err, fmt.Sprintf("milestone %q", mil.ID))
}

// SetMilestoneDone implements PlanningStore.
func (tx *txPlanning) SetMilestoneDone(planID string, code types.MilestoneRef, done bool) error {
	ctx := bg()
	status := "open"
	if done {
		status = "done"
	}
	res, err := tx.db.Exec(ctx, `UPDATE milestones SET status=?,updated_at=? WHERE plan_id=? AND short_code=?`,
		status, ms(time.Now().UTC()), planID, string(code))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("tasks: unknown milestone %q in plan %q", code, planID)
	}
	return nil
}

// ListMilestones implements PlanningStore in insertion order.
func (tx *txPlanning) ListMilestones(planID string) []Milestone {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT id,short_code,title,criteria_json,target_date_ms,status,created_at,updated_at FROM milestones WHERE plan_id=? ORDER BY rowid`, planID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Milestone
	for rows.Next() {
		var m Milestone
		var id, code, title, critRaw, status string
		var target, created, updated int64
		if err := rows.Scan(&id, &code, &title, &critRaw, &target, &status, &created, &updated); err != nil {
			return out
		}
		m.ID = id
		m.Code = types.MilestoneRef(code)
		m.PlanID = planID
		m.Title = title
		_ = fromJSON(critRaw, &m.Criteria)
		m.TargetDate = fromMs(target)
		m.Done = status == "done"
		m.CreatedAt = fromMs(created)
		m.UpdatedAt = fromMs(updated)
		out = append(out, m)
	}
	return out
}

// --- Requirements ---

func scanRequirement(scan func(...any) error) (types.Requirement, error) {
	var r types.Requirement
	var id, code, scopeRaw, text, origin, kind, priority, state string
	var version int
	var created, updated int64
	var principal string
	if err := scan(&id, &code, &scopeRaw, &text, &origin, &kind, &priority, &state, &version, &created, &updated, &principal); err != nil {
		return types.Requirement{}, err
	}
	r.ID = types.RequirementID(id)
	r.Code = types.RequirementRef(code)
	r.Scope = parseScope(scopeRaw)
	r.Text = text
	if origin != "" {
		m := types.MessageID(origin)
		r.OriginMsg = &m
	}
	r.Type = types.RequirementType(kind)
	r.Priority = types.Priority(priority)
	r.State = types.RequirementState(state)
	r.Version = version
	r.CreatedAt = fromMs(created)
	r.UpdatedAt = fromMs(updated)
	return r, nil
}

const reqCols = `id,short_code,scope_id,text,origin_msg_id,kind,priority,status,version,created_at,updated_at,principal_id`

// AddRequirement implements PlanningStore.
func (tx *txPlanning) AddRequirement(r types.Requirement) error {
	ctx := bg()
	origin := ""
	if r.OriginMsg != nil {
		origin = string(*r.OriginMsg)
	}
	_, err := tx.db.Exec(ctx, `INSERT INTO requirements(`+reqCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(r.ID), string(r.Code), scopeID(r.Scope), r.Text, origin, string(r.Type),
		string(r.Priority), string(r.State), r.Version, ms(r.CreatedAt), ms(r.UpdatedAt), "")
	return wrapExists(err, fmt.Sprintf("requirement %q", r.ID))
}

// GetRequirement implements PlanningStore.
func (tx *txPlanning) GetRequirement(id types.RequirementID) (types.Requirement, error) {
	ctx := bg()
	r, err := scanRequirement(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT `+reqCols+` FROM requirements WHERE id=?`, string(id)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Requirement{}, fmt.Errorf("tasks: unknown requirement %q", id)
		}
		return types.Requirement{}, err
	}
	return r, nil
}

// UpdateRequirement implements PlanningStore with optimistic concurrency.
func (tx *txPlanning) UpdateRequirement(r types.Requirement, wantVersion int) error {
	ctx := bg()
	origin := ""
	if r.OriginMsg != nil {
		origin = string(*r.OriginMsg)
	}
	res, err := tx.db.Exec(ctx, `UPDATE requirements SET short_code=?,scope_id=?,text=?,origin_msg_id=?,kind=?,priority=?,status=?,version=?,updated_at=? WHERE id=? AND version=?`,
		string(r.Code), scopeID(r.Scope), r.Text, origin, string(r.Type), string(r.Priority),
		string(r.State), wantVersion+1, ms(time.Now().UTC()), string(r.ID), wantVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		cur, gerr := tx.GetRequirement(r.ID)
		if gerr != nil {
			return gerr
		}
		return &VersionConflict{Entity: "requirement", ID: string(r.ID), Have: cur.Version, Want: wantVersion, Current: cur}
	}
	return nil
}

// ListRequirements implements PlanningStore.
func (tx *txPlanning) ListRequirements(scope types.Scope) []types.Requirement {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT `+reqCols+` FROM requirements WHERE scope_id=? ORDER BY id`, scopeID(scope))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Requirement
	for rows.Next() {
		r, err := scanRequirement(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, r)
	}
	return out
}

// --- Criteria ---

func scanCriterion(scan func(...any) error) (types.Criterion, error) {
	var c types.Criterion
	var id, code, reqID, statement, method, expected, impactRaw, state string
	var created, updated int64
	var scope string
	if err := scan(&id, &code, &reqID, &statement, &method, &expected, &impactRaw, &state, &created, &updated, &scope); err != nil {
		return types.Criterion{}, err
	}
	c.ID = types.CriterionID(id)
	if ref, err := types.ParseCriterionRef(code); err == nil {
		c.Code = ref
	}
	c.RequirementID = types.RequirementID(reqID)
	c.Statement = statement
	c.Method = types.CriterionMethod(method)
	c.Expected = expected
	_ = fromJSON(impactRaw, &c.ImpactArea)
	c.State = types.CriterionState(state)
	c.CreatedAt = fromMs(created)
	c.UpdatedAt = fromMs(updated)
	return c, nil
}

const critCols = `id,short_code,requirement_id,statement,method,expected,impact_json,status,created_at,updated_at,scope_id`

// AddCriterion implements PlanningStore.
func (tx *txPlanning) AddCriterion(c types.Criterion) error {
	ctx := bg()
	req, err := tx.GetRequirement(c.RequirementID)
	if err != nil {
		return err
	}
	_, err = tx.db.Exec(ctx, `INSERT INTO criteria(`+critCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		string(c.ID), c.Code.String(), string(c.RequirementID), c.Statement, string(c.Method),
		c.Expected, mustJSON(c.ImpactArea), string(c.State), ms(c.CreatedAt), ms(c.UpdatedAt), scopeID(req.Scope))
	if err != nil {
		return wrapExists(err, fmt.Sprintf("criterion %q", c.ID))
	}
	_, err = tx.db.Exec(ctx, `INSERT INTO criterion_versions(criterion_id,version,updated_at) VALUES(?,?,?)
		ON CONFLICT(criterion_id) DO NOTHING`, string(c.ID), 1, ms(time.Now().UTC()))
	return err
}

// GetCriterion implements PlanningStore.
func (tx *txPlanning) GetCriterion(id types.CriterionID) (types.Criterion, error) {
	ctx := bg()
	c, err := scanCriterion(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT `+critCols+` FROM criteria WHERE id=?`, string(id)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Criterion{}, fmt.Errorf("tasks: unknown criterion %q", id)
		}
		return types.Criterion{}, err
	}
	return c, nil
}

// criterionVersion reads the optimistic-concurrency version: rows
// created outside AddCriterion start at 1, like new rows.
func (tx *txPlanning) criterionVersion(ctx context.Context, id types.CriterionID) (int, error) {
	var v int
	err := tx.db.QueryRow(ctx, `SELECT version FROM criterion_versions WHERE criterion_id=?`, string(id)).Scan(&v)
	if err != nil {
		if isNotFound(err) {
			return 1, nil
		}
		return 0, err
	}
	if v == 0 {
		v = 1
	}
	return v, nil
}

// UpdateCriterion implements PlanningStore with optimistic concurrency.
func (tx *txPlanning) UpdateCriterion(c types.Criterion, wantVersion int) error {
	ctx := bg()
	cur, err := tx.GetCriterion(c.ID)
	if err != nil {
		return err
	}
	ver, err := tx.criterionVersion(ctx, c.ID)
	if err != nil {
		return err
	}
	if ver != wantVersion {
		return &VersionConflict{Entity: "criterion", ID: string(c.ID), Have: ver, Want: wantVersion, Current: cur}
	}
	_, err = tx.db.Exec(ctx, `UPDATE criteria SET statement=?,method=?,expected=?,impact_json=?,status=?,updated_at=? WHERE id=?`,
		c.Statement, string(c.Method), c.Expected, mustJSON(c.ImpactArea), string(c.State), ms(time.Now().UTC()), string(c.ID))
	if err != nil {
		return err
	}
	_, err = tx.db.Exec(ctx, `UPDATE criterion_versions SET version=?,updated_at=? WHERE criterion_id=?`, ver+1, ms(time.Now().UTC()), string(c.ID))
	return err
}

// CriterionVersion implements PlanningStore.
func (tx *txPlanning) CriterionVersion(id types.CriterionID) (int, error) {
	ctx := bg()
	if _, err := tx.GetCriterion(id); err != nil {
		return 0, err
	}
	return tx.criterionVersion(ctx, id)
}

// ListCriteria implements PlanningStore.
func (tx *txPlanning) ListCriteria(req types.RequirementID) []types.Criterion {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT `+critCols+` FROM criteria WHERE requirement_id=? ORDER BY id`, string(req))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Criterion
	for rows.Next() {
		c, err := scanCriterion(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, c)
	}
	return out
}

// --- Checks ---

func scanCheck(scan func(...any) error) (types.Check, error) {
	var c types.Check
	var id, critID, defRaw, env string
	var created int64
	var scope string
	if err := scan(&id, &critID, &defRaw, &env, &created, &scope); err != nil {
		return types.Check{}, err
	}
	c.ID = types.CheckID(id)
	c.CriterionID = types.CriterionID(critID)
	var def string
	if err := fromJSON(defRaw, &def); err != nil || (def == "" && defRaw != "" && defRaw != `""`) {
		def = defRaw
	}
	c.Definition = def
	c.Environment = env
	c.CreatedAt = fromMs(created)
	return c, nil
}

// AddCheck implements PlanningStore.
func (tx *txPlanning) AddCheck(c types.Check) error {
	ctx := bg()
	crit, err := tx.GetCriterion(c.CriterionID)
	if err != nil {
		return err
	}
	req, err := tx.GetRequirement(crit.RequirementID)
	if err != nil {
		return err
	}
	scope := scopeID(req.Scope)
	_, err = tx.db.Exec(ctx, `INSERT INTO checks(id,criterion_id,definition_json,environment,created_at,scope_id) VALUES(?,?,?,?,?,?)`,
		string(c.ID), string(c.CriterionID), mustJSON(c.Definition), c.Environment, ms(c.CreatedAt), scope)
	return wrapExists(err, fmt.Sprintf("check %q", c.ID))
}

// GetCheck implements PlanningStore.
func (tx *txPlanning) GetCheck(id types.CheckID) (types.Check, error) {
	ctx := bg()
	c, err := scanCheck(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT id,criterion_id,definition_json,environment,created_at,scope_id FROM checks WHERE id=?`, string(id)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Check{}, fmt.Errorf("tasks: unknown check %q", id)
		}
		return types.Check{}, err
	}
	return c, nil
}

// ListChecks implements PlanningStore in id order.
func (tx *txPlanning) ListChecks(criterion types.CriterionID) []types.Check {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT id,criterion_id,definition_json,environment,created_at,scope_id FROM checks WHERE criterion_id=? ORDER BY id`, string(criterion))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Check
	for rows.Next() {
		c, err := scanCheck(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, c)
	}
	return out
}

// --- Evidence ---

func scanEvidence(scan func(...any) error) (EvidenceRecord, error) {
	var rec EvidenceRecord
	var id, checkID, runID, result, blob, codeVer, printsRaw, toolCall string
	var revSeen int
	var created int64
	var scope string
	if err := scan(&id, &checkID, &runID, &result, &blob, &codeVer, &printsRaw, &toolCall, &revSeen, &created, &scope); err != nil {
		return EvidenceRecord{}, err
	}
	rec.Evidence.ID = types.EvidenceID(id)
	rec.Evidence.CheckID = types.CheckID(checkID)
	rec.Evidence.RunID = types.RunID(runID)
	rec.Evidence.Result = types.EvidenceResult(result)
	rec.Evidence.OutputBlob = blob
	var code string
	if err := fromJSON(codeVer, &code); err != nil || (code == "" && codeVer != "" && codeVer != `""`) {
		code = codeVer
	}
	rec.Evidence.CodeVersion = code
	_ = fromJSON(printsRaw, &rec.Evidence.Fingerprints)
	rec.Evidence.ToolCallID = toolCall
	rec.Evidence.CreatedAt = fromMs(created)
	rec.InstructionRevSeen = revSeen
	return rec, nil
}

const evCols = `id,check_id,run_id,result,blob_digest,code_version_json,fingerprints_json,tool_call_id,instruction_rev_seen,created_at,scope_id`

// RecordEvidence implements PlanningStore.
func (tx *txPlanning) RecordEvidence(rec EvidenceRecord) error {
	ctx := bg()
	chk, err := tx.GetCheck(rec.Evidence.CheckID)
	if err != nil {
		return fmt.Errorf("tasks: evidence for unknown check %q", rec.Evidence.CheckID)
	}
	crit, err := tx.GetCriterion(chk.CriterionID)
	if err != nil {
		return err
	}
	req, err := tx.GetRequirement(crit.RequirementID)
	if err != nil {
		return err
	}
	_, err = tx.db.Exec(ctx, `INSERT INTO evidence(`+evCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		string(rec.Evidence.ID), string(rec.Evidence.CheckID), string(rec.Evidence.RunID),
		string(rec.Evidence.Result), rec.Evidence.OutputBlob, mustJSON(rec.Evidence.CodeVersion),
		mustJSON(rec.Evidence.Fingerprints), rec.Evidence.ToolCallID, rec.InstructionRevSeen,
		ms(rec.Evidence.CreatedAt), scopeID(req.Scope))
	return wrapExists(err, fmt.Sprintf("evidence %q", rec.Evidence.ID))
}

// LatestEvidence implements PlanningStore.
func (tx *txPlanning) LatestEvidence(check types.CheckID) (EvidenceRecord, bool) {
	ctx := bg()
	rec, err := scanEvidence(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT `+evCols+` FROM evidence WHERE check_id=? ORDER BY rowid DESC LIMIT 1`, string(check)).Scan(dest...)
	})
	if err != nil {
		return EvidenceRecord{}, false
	}
	return rec, true
}

// EvidenceHistory implements PlanningStore in record order.
func (tx *txPlanning) EvidenceHistory(check types.CheckID) []EvidenceRecord {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT `+evCols+` FROM evidence WHERE check_id=? ORDER BY rowid`, string(check))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []EvidenceRecord
	for rows.Next() {
		rec, err := scanEvidence(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, rec)
	}
	return out
}

// --- Assumptions, decisions, logbook ---

// AddAssumption implements PlanningStore.
func (tx *txPlanning) AddAssumption(a types.Assumption) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO assumptions(id,text,reason,status,linked_json,created_at,updated_at,scope_id)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET text=excluded.text,reason=excluded.reason,status=excluded.status,updated_at=excluded.updated_at`,
		a.ID, a.Text, a.Reason, a.State, "[]", ms(a.CreatedAt), ms(a.UpdatedAt), "")
	return err
}

// AddDecision implements PlanningStore.
func (tx *txPlanning) AddDecision(d types.Decision) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO decisions(id,statement,reason,alternatives_json,status,superseded_by,created_at,updated_at,scope_id)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET statement=excluded.statement,reason=excluded.reason,
		alternatives_json=excluded.alternatives_json,status=excluded.status,superseded_by=excluded.superseded_by,updated_at=excluded.updated_at`,
		d.ID, d.Statement, d.Reason, mustJSON(d.Alternatives), d.State, d.ReplacedBy, ms(d.CreatedAt), ms(d.UpdatedAt), "")
	return err
}

// AppendLog implements PlanningStore.
func (tx *txPlanning) AppendLog(e ProjectLogEntry) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO project_log(id,project_id,type,text,refs_json,created_at,scope_id) VALUES(?,?,?,?,?,?,?)`,
		e.ID, string(e.ProjectID), e.Kind, e.Text, mustJSON(e.Refs), ms(e.CreatedAt), "")
	return wrapExists(err, fmt.Sprintf("log entry %q", e.ID))
}

// ListLog implements PlanningStore in append order.
func (tx *txPlanning) ListLog(project types.ProjectID) []ProjectLogEntry {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT id,type,text,refs_json,created_at FROM project_log WHERE project_id=? ORDER BY rowid`, string(project))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ProjectLogEntry
	for rows.Next() {
		var e ProjectLogEntry
		var id, kind, text, refsRaw string
		var created int64
		if err := rows.Scan(&id, &kind, &text, &refsRaw, &created); err != nil {
			return out
		}
		e.ID = id
		e.ProjectID = project
		e.Kind = kind
		e.Text = text
		_ = fromJSON(refsRaw, &e.Refs)
		e.CreatedAt = fromMs(created)
		out = append(out, e)
	}
	return out
}

// --- Edges, revisions, fingerprints ---

// LinkTaskRequirement implements PlanningStore.
func (tx *txPlanning) LinkTaskRequirement(task types.TaskID, req types.RequirementID) error {
	ctx := bg()
	if _, err := tx.GetTask(task); err != nil {
		return err
	}
	if _, err := tx.GetRequirement(req); err != nil {
		return err
	}
	_, err := tx.db.Exec(ctx, `INSERT INTO task_requirements(task_id,requirement_id,created_at) VALUES(?,?,?)
		ON CONFLICT(task_id,requirement_id) DO NOTHING`, string(task), string(req), ms(time.Now().UTC()))
	return err
}

// TaskRequirements implements PlanningStore in link order.
func (tx *txPlanning) TaskRequirements(task types.TaskID) []types.RequirementID {
	ctx := bg()
	rows, err := tx.db.Query(ctx, `SELECT requirement_id FROM task_requirements WHERE task_id=? ORDER BY rowid`, string(task))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.RequirementID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out
		}
		out = append(out, types.RequirementID(id))
	}
	return out
}

// InstructionRev implements PlanningStore.
func (tx *txPlanning) InstructionRev(scope types.Scope) int {
	ctx := bg()
	var rev int
	if err := tx.db.QueryRow(ctx, `SELECT rev FROM instruction_revs WHERE scope_id=?`, scopeID(scope)).Scan(&rev); err != nil {
		return 0
	}
	return rev
}

// SetInstructionRev implements PlanningStore.
func (tx *txPlanning) SetInstructionRev(scope types.Scope, rev int) {
	ctx := bg()
	_, _ = tx.db.Exec(ctx, `INSERT INTO instruction_revs(scope_id,rev,updated_at) VALUES(?,?,?)
		ON CONFLICT(scope_id) DO UPDATE SET rev=excluded.rev,updated_at=excluded.updated_at`,
		scopeID(scope), rev, ms(time.Now().UTC()))
}

// RecordFileEdit implements PlanningStore.
func (tx *txPlanning) RecordFileEdit(path, fingerprint string) {
	ctx := bg()
	_, _ = tx.db.Exec(ctx, `INSERT INTO file_fingerprints(path,fingerprint,updated_at) VALUES(?,?,?)
		ON CONFLICT(path) DO UPDATE SET fingerprint=excluded.fingerprint,updated_at=excluded.updated_at`,
		path, fingerprint, ms(time.Now().UTC()))
}

// FileFingerprint implements PlanningStore.
func (tx *txPlanning) FileFingerprint(path string) (string, bool) {
	ctx := bg()
	var fp string
	if err := tx.db.QueryRow(ctx, `SELECT fingerprint FROM file_fingerprints WHERE path=?`, path).Scan(&fp); err != nil {
		return "", false
	}
	return fp, true
}

// --- Short codes ---

// NextTaskCode implements PlanningStore.
func (tx *txPlanning) NextTaskCode(scope types.Scope) types.TaskRef {
	n, err := tx.nextShortTx(bg(), scopeID(scope), "T")
	if err != nil {
		panic(err)
	}
	return types.FormatTaskRef(n)
}

// NextRequirementCode implements PlanningStore.
func (tx *txPlanning) NextRequirementCode(scope types.Scope) types.RequirementRef {
	n, err := tx.nextShortTx(bg(), scopeID(scope), "R")
	if err != nil {
		panic(err)
	}
	return types.FormatRequirementRef(n)
}

// NextCriterionCode implements PlanningStore.
func (tx *txPlanning) NextCriterionCode(req types.RequirementID, reqNum int) types.CriterionRef {
	n, err := tx.nextShortTx(bg(), string(req), "C")
	if err != nil {
		panic(err)
	}
	return types.CriterionRef{Requirement: reqNum, Criterion: n}
}

// NextMilestoneCode implements PlanningStore.
func (tx *txPlanning) NextMilestoneCode(planID string) types.MilestoneRef {
	n, err := tx.nextShortTx(bg(), planID, "M")
	if err != nil {
		panic(err)
	}
	return types.FormatMilestoneRef(n)
}

// RequirementByCode implements PlanningStore.
func (tx *txPlanning) RequirementByCode(scope types.Scope, code types.RequirementRef) (types.Requirement, error) {
	ctx := bg()
	r, err := scanRequirement(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT `+reqCols+` FROM requirements WHERE scope_id=? AND short_code=?`, scopeID(scope), string(code)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Requirement{}, fmt.Errorf("tasks: unknown requirement %q in scope %q", code, scope.String())
		}
		return types.Requirement{}, err
	}
	return r, nil
}

// TaskByCode implements PlanningStore.
func (tx *txPlanning) TaskByCode(scope types.Scope, code types.TaskRef) (types.Task, error) {
	ctx := bg()
	t, err := scanTask(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT `+taskCols+` FROM tasks WHERE scope_id=? AND short_code=?`, scopeID(scope), string(code)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Task{}, fmt.Errorf("tasks: unknown task %q in scope %q", code, scope.String())
		}
		return types.Task{}, err
	}
	return t, nil
}

// CriterionByCode implements PlanningStore.
func (tx *txPlanning) CriterionByCode(scope types.Scope, code string) (types.Criterion, error) {
	ref, err := types.ParseCriterionRef(code)
	if err != nil {
		return types.Criterion{}, fmt.Errorf("tasks: %w", err)
	}
	reqCode := types.FormatRequirementRef(ref.Requirement)
	req, err := tx.RequirementByCode(scope, reqCode)
	if err != nil {
		return types.Criterion{}, err
	}
	ctx := bg()
	c, err := scanCriterion(func(dest ...any) error {
		return tx.db.QueryRow(ctx, `SELECT `+critCols+` FROM criteria WHERE requirement_id=? AND short_code=?`,
			string(req.ID), ref.String()).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Criterion{}, fmt.Errorf("tasks: unknown criterion %q in scope %q", code, scope.String())
		}
		return types.Criterion{}, err
	}
	return c, nil
}

// --- SQLStore method set: one porter round trip per call ---

// RegisterProject implements PlanningStore.
func (s *SQLStore) RegisterProject(p Project) error {
	return s.write(func(tx *txPlanning) error { return tx.RegisterProject(p) })
}

// GetProject implements PlanningStore.
func (s *SQLStore) GetProject(id types.ProjectID) (Project, error) {
	var out Project
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.GetProject(id)
		return err
	})
}

// UpdateProject implements PlanningStore.
func (s *SQLStore) UpdateProject(p Project) error {
	return s.write(func(tx *txPlanning) error { return tx.UpdateProject(p) })
}

// ListProjects implements PlanningStore.
func (s *SQLStore) ListProjects(includeArchived bool) []Project {
	var out []Project
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListProjects(includeArchived)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CreateTask implements PlanningStore.
func (s *SQLStore) CreateTask(t types.Task) error {
	return s.write(func(tx *txPlanning) error { return tx.CreateTask(t) })
}

// GetTask implements PlanningStore.
func (s *SQLStore) GetTask(id types.TaskID) (types.Task, error) {
	var out types.Task
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.GetTask(id)
		return err
	})
}

// UpdateTask implements PlanningStore.
func (s *SQLStore) UpdateTask(t types.Task, wantVersion int) error {
	return s.write(func(tx *txPlanning) error { return tx.UpdateTask(t, wantVersion) })
}

// SetTaskRun implements PlanningStore.
func (s *SQLStore) SetTaskRun(id types.TaskID, run types.RunID, state types.TaskState) error {
	return s.write(func(tx *txPlanning) error { return tx.SetTaskRun(id, run, state) })
}

// ReopenTask implements PlanningStore.
func (s *SQLStore) ReopenTask(id types.TaskID, run types.RunID) error {
	return s.write(func(tx *txPlanning) error { return tx.ReopenTask(id, run) })
}

// ListTasks implements PlanningStore.
func (s *SQLStore) ListTasks(scope types.Scope) []types.Task {
	var out []types.Task
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListTasks(scope)
		return nil
	})
	return out
}

// CreatePlan implements PlanningStore.
func (s *SQLStore) CreatePlan(p Plan) error {
	return s.write(func(tx *txPlanning) error { return tx.CreatePlan(p) })
}

// GetPlan implements PlanningStore.
func (s *SQLStore) GetPlan(id string) (Plan, error) {
	var out Plan
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.GetPlan(id)
		return err
	})
}

// UpdatePlan implements PlanningStore.
func (s *SQLStore) UpdatePlan(p Plan, wantVersion int) error {
	return s.write(func(tx *txPlanning) error { return tx.UpdatePlan(p, wantVersion) })
}

// PlanVersions implements PlanningStore.
func (s *SQLStore) PlanVersions(planID string) []Plan {
	var out []Plan
	_ = s.read(func(tx *txPlanning) error {
		out = tx.PlanVersions(planID)
		return nil
	})
	return out
}

// ListPlans implements PlanningStore.
func (s *SQLStore) ListPlans(scope types.Scope, typ types.PlanType) []Plan {
	var out []Plan
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListPlans(scope, typ)
		return nil
	})
	return out
}

// AddPlanItem implements PlanningStore.
func (s *SQLStore) AddPlanItem(item PlanItem) error {
	return s.write(func(tx *txPlanning) error { return tx.AddPlanItem(item) })
}

// GetPlanItem implements PlanningStore.
func (s *SQLStore) GetPlanItem(id string) (PlanItem, error) {
	var out PlanItem
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.GetPlanItem(id)
		return err
	})
}

// UpdatePlanItem implements PlanningStore.
func (s *SQLStore) UpdatePlanItem(item PlanItem, wantVersion int) error {
	return s.write(func(tx *txPlanning) error { return tx.UpdatePlanItem(item, wantVersion) })
}

// ListPlanItems implements PlanningStore.
func (s *SQLStore) ListPlanItems(planID string) []PlanItem {
	var out []PlanItem
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListPlanItems(planID)
		return nil
	})
	return out
}

// AddDependency implements PlanningStore.
func (s *SQLStore) AddDependency(planID string, d Dependency) error {
	return s.write(func(tx *txPlanning) error { return tx.AddDependency(planID, d) })
}

// ListDependencies implements PlanningStore.
func (s *SQLStore) ListDependencies(planID string) []Dependency {
	var out []Dependency
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListDependencies(planID)
		return nil
	})
	return out
}

// AddMilestone implements PlanningStore.
func (s *SQLStore) AddMilestone(mil Milestone) error {
	return s.write(func(tx *txPlanning) error { return tx.AddMilestone(mil) })
}

// SetMilestoneDone implements PlanningStore.
func (s *SQLStore) SetMilestoneDone(planID string, code types.MilestoneRef, done bool) error {
	return s.write(func(tx *txPlanning) error { return tx.SetMilestoneDone(planID, code, done) })
}

// ListMilestones implements PlanningStore.
func (s *SQLStore) ListMilestones(planID string) []Milestone {
	var out []Milestone
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListMilestones(planID)
		return nil
	})
	return out
}

// AddRequirement implements PlanningStore.
func (s *SQLStore) AddRequirement(r types.Requirement) error {
	return s.write(func(tx *txPlanning) error { return tx.AddRequirement(r) })
}

// GetRequirement implements PlanningStore.
func (s *SQLStore) GetRequirement(id types.RequirementID) (types.Requirement, error) {
	var out types.Requirement
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.GetRequirement(id)
		return err
	})
}

// UpdateRequirement implements PlanningStore.
func (s *SQLStore) UpdateRequirement(r types.Requirement, wantVersion int) error {
	return s.write(func(tx *txPlanning) error { return tx.UpdateRequirement(r, wantVersion) })
}

// ListRequirements implements PlanningStore.
func (s *SQLStore) ListRequirements(scope types.Scope) []types.Requirement {
	var out []types.Requirement
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListRequirements(scope)
		return nil
	})
	return out
}

// AddCriterion implements PlanningStore.
func (s *SQLStore) AddCriterion(c types.Criterion) error {
	return s.write(func(tx *txPlanning) error { return tx.AddCriterion(c) })
}

// GetCriterion implements PlanningStore.
func (s *SQLStore) GetCriterion(id types.CriterionID) (types.Criterion, error) {
	var out types.Criterion
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.GetCriterion(id)
		return err
	})
}

// UpdateCriterion implements PlanningStore.
func (s *SQLStore) UpdateCriterion(c types.Criterion, wantVersion int) error {
	return s.write(func(tx *txPlanning) error { return tx.UpdateCriterion(c, wantVersion) })
}

// CriterionVersion implements PlanningStore.
func (s *SQLStore) CriterionVersion(id types.CriterionID) (int, error) {
	var out int
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.CriterionVersion(id)
		return err
	})
}

// ListCriteria implements PlanningStore.
func (s *SQLStore) ListCriteria(req types.RequirementID) []types.Criterion {
	var out []types.Criterion
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListCriteria(req)
		return nil
	})
	return out
}

// AddCheck implements PlanningStore.
func (s *SQLStore) AddCheck(c types.Check) error {
	return s.write(func(tx *txPlanning) error { return tx.AddCheck(c) })
}

// GetCheck implements PlanningStore.
func (s *SQLStore) GetCheck(id types.CheckID) (types.Check, error) {
	var out types.Check
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.GetCheck(id)
		return err
	})
}

// ListChecks implements PlanningStore.
func (s *SQLStore) ListChecks(criterion types.CriterionID) []types.Check {
	var out []types.Check
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListChecks(criterion)
		return nil
	})
	return out
}

// RecordEvidence implements PlanningStore.
func (s *SQLStore) RecordEvidence(rec EvidenceRecord) error {
	return s.write(func(tx *txPlanning) error { return tx.RecordEvidence(rec) })
}

// LatestEvidence implements PlanningStore.
func (s *SQLStore) LatestEvidence(check types.CheckID) (EvidenceRecord, bool) {
	var out EvidenceRecord
	var ok bool
	_ = s.read(func(tx *txPlanning) error {
		out, ok = tx.LatestEvidence(check)
		return nil
	})
	return out, ok
}

// EvidenceHistory implements PlanningStore.
func (s *SQLStore) EvidenceHistory(check types.CheckID) []EvidenceRecord {
	var out []EvidenceRecord
	_ = s.read(func(tx *txPlanning) error {
		out = tx.EvidenceHistory(check)
		return nil
	})
	return out
}

// AddAssumption implements PlanningStore.
func (s *SQLStore) AddAssumption(a types.Assumption) error {
	return s.write(func(tx *txPlanning) error { return tx.AddAssumption(a) })
}

// AddDecision implements PlanningStore.
func (s *SQLStore) AddDecision(d types.Decision) error {
	return s.write(func(tx *txPlanning) error { return tx.AddDecision(d) })
}

// AppendLog implements PlanningStore.
func (s *SQLStore) AppendLog(e ProjectLogEntry) error {
	return s.write(func(tx *txPlanning) error { return tx.AppendLog(e) })
}

// ListLog implements PlanningStore.
func (s *SQLStore) ListLog(project types.ProjectID) []ProjectLogEntry {
	var out []ProjectLogEntry
	_ = s.read(func(tx *txPlanning) error {
		out = tx.ListLog(project)
		return nil
	})
	return out
}

// LinkTaskRequirement implements PlanningStore.
func (s *SQLStore) LinkTaskRequirement(task types.TaskID, req types.RequirementID) error {
	return s.write(func(tx *txPlanning) error { return tx.LinkTaskRequirement(task, req) })
}

// TaskRequirements implements PlanningStore.
func (s *SQLStore) TaskRequirements(task types.TaskID) []types.RequirementID {
	var out []types.RequirementID
	_ = s.read(func(tx *txPlanning) error {
		out = tx.TaskRequirements(task)
		return nil
	})
	return out
}

// InstructionRev implements PlanningStore.
func (s *SQLStore) InstructionRev(scope types.Scope) int {
	var out int
	_ = s.read(func(tx *txPlanning) error {
		out = tx.InstructionRev(scope)
		return nil
	})
	return out
}

// SetInstructionRev implements PlanningStore.
func (s *SQLStore) SetInstructionRev(scope types.Scope, rev int) {
	_ = s.write(func(tx *txPlanning) error {
		tx.SetInstructionRev(scope, rev)
		return nil
	})
}

// RecordFileEdit implements PlanningStore.
func (s *SQLStore) RecordFileEdit(path, fingerprint string) {
	_ = s.write(func(tx *txPlanning) error {
		tx.RecordFileEdit(path, fingerprint)
		return nil
	})
}

// FileFingerprint implements PlanningStore.
func (s *SQLStore) FileFingerprint(path string) (string, bool) {
	var out string
	var ok bool
	_ = s.read(func(tx *txPlanning) error {
		out, ok = tx.FileFingerprint(path)
		return nil
	})
	return out, ok
}

// NextTaskCode implements PlanningStore.
func (s *SQLStore) NextTaskCode(scope types.Scope) types.TaskRef {
	var out types.TaskRef
	_ = s.write(func(tx *txPlanning) error {
		out = tx.NextTaskCode(scope)
		return nil
	})
	return out
}

// NextRequirementCode implements PlanningStore.
func (s *SQLStore) NextRequirementCode(scope types.Scope) types.RequirementRef {
	var out types.RequirementRef
	_ = s.write(func(tx *txPlanning) error {
		out = tx.NextRequirementCode(scope)
		return nil
	})
	return out
}

// NextCriterionCode implements PlanningStore.
func (s *SQLStore) NextCriterionCode(req types.RequirementID, reqNum int) types.CriterionRef {
	var out types.CriterionRef
	_ = s.write(func(tx *txPlanning) error {
		out = tx.NextCriterionCode(req, reqNum)
		return nil
	})
	return out
}

// NextMilestoneCode implements PlanningStore.
func (s *SQLStore) NextMilestoneCode(planID string) types.MilestoneRef {
	var out types.MilestoneRef
	_ = s.write(func(tx *txPlanning) error {
		out = tx.NextMilestoneCode(planID)
		return nil
	})
	return out
}

// RequirementByCode implements PlanningStore.
func (s *SQLStore) RequirementByCode(scope types.Scope, code types.RequirementRef) (types.Requirement, error) {
	var out types.Requirement
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.RequirementByCode(scope, code)
		return err
	})
}

// TaskByCode implements PlanningStore.
func (s *SQLStore) TaskByCode(scope types.Scope, code types.TaskRef) (types.Task, error) {
	var out types.Task
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.TaskByCode(scope, code)
		return err
	})
}

// CriterionByCode implements PlanningStore.
func (s *SQLStore) CriterionByCode(scope types.Scope, code string) (types.Criterion, error) {
	var out types.Criterion
	return out, s.read(func(tx *txPlanning) error {
		var err error
		out, err = tx.CriterionByCode(scope, code)
		return err
	})
}

// Compile-time check: SQLStore and txPlanning both satisfy PlanningStore.
var (
	_ PlanningStore = (*SQLStore)(nil)
	_ PlanningStore = (*txPlanning)(nil)
)
