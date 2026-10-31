// SQL-backed orchestrator persistence (Sections 5.1-5.2, 15).
//
// SQLStore implements Store over the store porter: writes on the
// normal lane, reads from the pool. Like the tasks adapter, every
// method runs one porter round trip with a background context: the
// Store interface predates wiring and carries no context.
//
// Delivery reports land in run_reports (latest wins); usage
// accumulates in run_usage. Mailbox order is insertion order
// (rowid), matching the MemoryStore send-order guarantee (15.5).
package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// SQLStore persists agents, runs, mailbox, instructions, questions,
// reports and usage in the Appendix A tables plus the v5 wiring
// tables. The zero value is unusable; build it with OpenSQLStore.
// It is safe for concurrent use.
type SQLStore struct {
	W store.Writer
	R sqlReader
}

// sqlReader is the read half an SQLStore needs: satisfied by
// *store.DB and by test fakes.
type sqlReader interface {
	Read(ctx context.Context, fn store.ReadFunc) error
}

// OpenSQLStore returns a Store over db: writes on the normal lane,
// reads from the pool.
func OpenSQLStore(db *store.DB) *SQLStore { return &SQLStore{W: db, R: db} }

// txDB is the statement surface one transaction offers. Reads run on
// a pooled connection through the failing-write adapter.
type txDB interface {
	Exec(ctx context.Context, query string, args ...any) (store.Result, error)
	Query(ctx context.Context, query string, args ...any) (store.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) store.Row
}

// readOnly adapts a Queryer to txDB for read paths.
type readOnly struct{ store.Queryer }

func (r readOnly) Exec(context.Context, string, ...any) (store.Result, error) {
	return nil, fmt.Errorf("orchestrator: write through a read handle")
}

// txStore implements Store over one txDB.
type txStore struct{ db txDB }

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

func wrapExists(err error, what string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf("orchestrator: %s already exists", what)
	}
	return err
}

// --- Agents ---

// SaveAgent implements Store.
func (tx *txStore) SaveAgent(a types.Agent) error {
	ctx := bg()
	parent := ""
	if a.ParentID != nil {
		parent = string(*a.ParentID)
	}
	_, err := tx.db.Exec(ctx, `INSERT INTO agents(id,name,description,profile_id,parent_id,level,scope_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		string(a.ID), a.Name, a.Description, a.Profile, parent, a.Level,
		a.Scope.String(), ms(a.CreatedAt), ms(a.UpdatedAt))
	return wrapExists(err, fmt.Sprintf("agent %q", a.ID))
}

func scanAgent(scan func(...any) error) (types.Agent, error) {
	var a types.Agent
	var id, name, desc, profile, parent, scopeRaw string
	var level int
	var created, updated int64
	if err := scan(&id, &name, &desc, &profile, &parent, &level, &scopeRaw, &created, &updated); err != nil {
		return types.Agent{}, err
	}
	a.ID = types.AgentID(id)
	a.Name = name
	a.Description = desc
	a.Profile = profile
	if parent != "" {
		p := types.AgentID(parent)
		a.ParentID = &p
	}
	a.Level = level
	if scopeRaw != "" {
		if s, err := types.ParseScope(scopeRaw); err == nil {
			a.Scope = s
		}
	}
	a.CreatedAt = fromMs(created)
	a.UpdatedAt = fromMs(updated)
	return a, nil
}

const agentCols = `id,name,description,profile_id,parent_id,level,scope_id,created_at,updated_at`

// GetAgent implements Store.
func (tx *txStore) GetAgent(id types.AgentID) (types.Agent, error) {
	a, err := scanAgent(func(dest ...any) error {
		return tx.db.QueryRow(bg(), `SELECT `+agentCols+` FROM agents WHERE id=?`, string(id)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Agent{}, fmt.Errorf("orchestrator: unknown agent %q", id)
		}
		return types.Agent{}, err
	}
	return a, nil
}

// ListAgents implements Store in deterministic (id) order.
func (tx *txStore) ListAgents() []types.Agent {
	rows, err := tx.db.Query(bg(), `SELECT `+agentCols+` FROM agents ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Agent
	for rows.Next() {
		a, err := scanAgent(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, a)
	}
	return out
}

// Children implements Store in deterministic (id) order.
func (tx *txStore) Children(parent types.AgentID) []types.Agent {
	rows, err := tx.db.Query(bg(), `SELECT `+agentCols+` FROM agents WHERE parent_id=? ORDER BY id`, string(parent))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Agent
	for rows.Next() {
		a, err := scanAgent(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, a)
	}
	return out
}

// FindAgentsByName implements Store in deterministic (id) order.
func (tx *txStore) FindAgentsByName(name string) []types.Agent {
	rows, err := tx.db.Query(bg(), `SELECT `+agentCols+` FROM agents WHERE name=? ORDER BY id`, name)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Agent
	for rows.Next() {
		a, err := scanAgent(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, a)
	}
	return out
}

// --- Runs ---

func scanRun(scan func(...any) error) (RunRecord, error) {
	var rec RunRecord
	var r types.Run
	var id, agentID, taskID, state, reason, pausedBy string
	var queuePos, instrRev int
	var budgetRaw, lastProgress, waitReason, pauseReason, blockReason string
	var heartbeat, created, updated int64
	var lane, scopeRaw string
	if err := scan(&id, &agentID, &taskID, &state, &reason, &pausedBy, &queuePos, &budgetRaw,
		&heartbeat, &lastProgress, &waitReason, &pauseReason, &blockReason, &lane, &scopeRaw, &instrRev, &created, &updated); err != nil {
		return RunRecord{}, err
	}
	r.ID = types.RunID(id)
	r.AgentID = types.AgentID(agentID)
	r.TaskID = types.TaskID(taskID)
	r.State = types.RunState(state)
	_ = reason
	if waitReason != "" {
		w := types.WaitReason(waitReason)
		r.WaitReason = &w
	}
	if pauseReason != "" {
		p := types.PauseReason(pauseReason)
		r.PauseReason = &p
	}
	if blockReason != "" {
		b := types.BlockReason(blockReason)
		r.BlockReason = &b
	}
	r.PausedBy = pausedBy
	r.QueuePosition = queuePos
	_ = json.Unmarshal([]byte(budgetRaw), &r.Budget)
	r.HeartbeatAt = fromMs(heartbeat)
	r.LastProgress = lastProgress
	r.InstructionsRevSeen = instrRev
	r.CreatedAt = fromMs(created)
	r.UpdatedAt = fromMs(updated)
	rec.Run = r
	switch lane {
	case "P0":
		rec.Lane = LaneP0
	case "P2":
		rec.Lane = LaneP2
	case "P3":
		rec.Lane = LaneP3
	default:
		rec.Lane = LaneP1
	}
	if scopeRaw != "" {
		if s, err := types.ParseScope(scopeRaw); err == nil {
			rec.Scope = s
		}
	}
	return rec, nil
}

const runCols = `id,agent_id,task_id,status,reason,paused_by,queue_pos,budget_json,heartbeat_ms,last_progress,wait_reason,pause_reason,block_reason,lane,scope_id,instructions_rev_seen,created_at,updated_at`

// CreateRun implements Store.
func (tx *txStore) CreateRun(r RunRecord) error {
	ctx := bg()
	var wait, pause, block string
	if r.Run.WaitReason != nil {
		wait = string(*r.Run.WaitReason)
	}
	if r.Run.PauseReason != nil {
		pause = string(*r.Run.PauseReason)
	}
	if r.Run.BlockReason != nil {
		block = string(*r.Run.BlockReason)
	}
	_, err := tx.db.Exec(ctx, `INSERT INTO runs(id,agent_id,task_id,status,reason,paused_by,queue_pos,budget_json,spent_json,heartbeat_ms,
		last_progress,instructions_rev_seen,lane,wait_reason,pause_reason,block_reason,created_at,updated_at,scope_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(r.Run.ID), string(r.Run.AgentID), string(r.Run.TaskID), string(r.Run.State), "",
		r.Run.PausedBy, r.Run.QueuePosition, mustJSON(r.Run.Budget), "{}", ms(r.Run.HeartbeatAt),
		r.Run.LastProgress, r.Run.InstructionsRevSeen, r.Lane.String(), wait, pause, block,
		ms(r.Run.CreatedAt), ms(r.Run.UpdatedAt), r.Scope.String())
	return wrapExists(err, fmt.Sprintf("run %q", r.Run.ID))
}

// GetRun implements Store.
func (tx *txStore) GetRun(id types.RunID) (RunRecord, error) {
	rec, err := scanRun(func(dest ...any) error {
		return tx.db.QueryRow(bg(), `SELECT `+runCols+` FROM runs WHERE id=?`, string(id)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return RunRecord{}, fmt.Errorf("orchestrator: unknown run %q", id)
		}
		return RunRecord{}, err
	}
	return rec, nil
}

// UpdateRun implements Store.
func (tx *txStore) UpdateRun(r RunRecord) error {
	ctx := bg()
	var wait, pause, block string
	if r.Run.WaitReason != nil {
		wait = string(*r.Run.WaitReason)
	}
	if r.Run.PauseReason != nil {
		pause = string(*r.Run.PauseReason)
	}
	if r.Run.BlockReason != nil {
		block = string(*r.Run.BlockReason)
	}
	res, err := tx.db.Exec(ctx, `UPDATE runs SET agent_id=?,task_id=?,status=?,paused_by=?,queue_pos=?,budget_json=?,
		heartbeat_ms=?,last_progress=?,instructions_rev_seen=?,lane=?,wait_reason=?,pause_reason=?,block_reason=?,updated_at=?,scope_id=? WHERE id=?`,
		string(r.Run.AgentID), string(r.Run.TaskID), string(r.Run.State), r.Run.PausedBy, r.Run.QueuePosition,
		mustJSON(r.Run.Budget), ms(r.Run.HeartbeatAt), r.Run.LastProgress, r.Run.InstructionsRevSeen,
		r.Lane.String(), wait, pause, block, ms(time.Now().UTC()), r.Scope.String(), string(r.Run.ID))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("orchestrator: unknown run %q", r.Run.ID)
	}
	return nil
}

// ListRuns implements Store in deterministic (id) order.
func (tx *txStore) ListRuns() []RunRecord {
	rows, err := tx.db.Query(bg(), `SELECT `+runCols+` FROM runs ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		rec, err := scanRun(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, rec)
	}
	return out
}

// RunsByState implements Store in deterministic (id) order.
func (tx *txStore) RunsByState(s types.RunState) []RunRecord {
	rows, err := tx.db.Query(bg(), `SELECT `+runCols+` FROM runs WHERE status=? ORDER BY id`, string(s))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		rec, err := scanRun(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, rec)
	}
	return out
}

// --- Mailbox ---

func scanMessage(scan func(...any) error) (Message, error) {
	var m Message
	var id, from, to, typ, payload, priority, ackNote, scope string
	var delivered, ackMs, created int64
	if err := scan(&id, &from, &to, &typ, &payload, &priority, &delivered, &ackMs, &ackNote, &created, &scope); err != nil {
		return Message{}, err
	}
	m.ID = types.MessageID(id)
	m.From = types.AgentID(from)
	m.To = types.AgentID(to)
	m.Type = MessageType(typ)
	m.Priority = DirectivePriority(priority)
	m.Payload = payload
	m.Acked = ackMs != 0
	m.AckedAt = fromMs(ackMs)
	m.AckNote = ackNote
	m.CreatedAt = fromMs(created)
	return m, nil
}

const msgCols = `id,from_id,to_id,type,payload_json,priority,delivered,ack_ms,ack_note,created_at,scope_id`

// AppendMessage implements Store: a duplicate id is a redelivery and
// is ignored, which gives at-least-once delivery without doubles
// (Section 15.5).
func (tx *txStore) AppendMessage(msg Message) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO agent_messages(id,from_id,to_id,type,payload_json,priority,delivered,ack_ms,ack_note,created_at,scope_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		string(msg.ID), string(msg.From), string(msg.To), string(msg.Type), msg.Payload,
		string(msg.Priority), 1, 0, "", ms(msg.CreatedAt), "")
	return err
}

// GetMessage implements Store.
func (tx *txStore) GetMessage(id types.MessageID) (Message, error) {
	m, err := scanMessage(func(dest ...any) error {
		return tx.db.QueryRow(bg(), `SELECT `+msgCols+` FROM agent_messages WHERE id=?`, string(id)).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return Message{}, fmt.Errorf("orchestrator: unknown message %q", id)
		}
		return Message{}, err
	}
	return m, nil
}

func (tx *txStore) listMessages(where string, args ...any) []Message {
	rows, err := tx.db.Query(bg(), `SELECT `+msgCols+` FROM agent_messages WHERE `+where+` ORDER BY rowid`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, m)
	}
	return out
}

// PendingMessages implements Store in send order.
func (tx *txStore) PendingMessages(to types.AgentID) []Message {
	return tx.listMessages(`to_id=? AND ack_ms=0`, string(to))
}

// UnackedDirectives implements Store in send order.
func (tx *txStore) UnackedDirectives(to types.AgentID) []Message {
	return tx.listMessages(`to_id=? AND type='directive' AND ack_ms=0`, string(to))
}

// AckMessage implements Store.
func (tx *txStore) AckMessage(id types.MessageID, note string) error {
	ctx := bg()
	res, err := tx.db.Exec(ctx, `UPDATE agent_messages SET ack_ms=?,ack_note=?,delivered=1
		WHERE id=? AND ack_ms=0`, ms(time.Now().UTC()), note, string(id))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, gerr := tx.GetMessage(id); gerr != nil {
			return gerr
		}
	}
	return nil
}

// --- Instructions, questions, reports, usage ---

// SaveInstruction implements Store: revisions only move forward per
// scope.
func (tx *txStore) SaveInstruction(ins types.Instruction) error {
	ctx := bg()
	var max int
	err := tx.db.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0) FROM instructions WHERE scope_id=?`, ins.Scope.String()).Scan(&max)
	if err != nil {
		return err
	}
	if ins.Revision <= max {
		return fmt.Errorf("orchestrator: instruction revision %d not newer than %d for scope %q",
			ins.Revision, max, ins.Scope.String())
	}
	_, err = tx.db.Exec(ctx, `INSERT INTO instructions(id,scope_id,text,revision,origin_msg_id,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		ins.ID, ins.Scope.String(), ins.Text, ins.Revision, ins.Origin, ins.State, ms(ins.CreatedAt), ms(ins.UpdatedAt))
	return wrapExists(err, fmt.Sprintf("instruction %q", ins.ID))
}

func scanInstruction(scan func(...any) error) (types.Instruction, error) {
	var ins types.Instruction
	var id, scopeRaw, text, origin, state string
	var rev int
	var created, updated int64
	if err := scan(&id, &scopeRaw, &text, &rev, &origin, &state, &created, &updated); err != nil {
		return types.Instruction{}, err
	}
	ins.ID = id
	if s, err := types.ParseScope(scopeRaw); err == nil {
		ins.Scope = s
	}
	ins.Text = text
	ins.Revision = rev
	ins.Origin = origin
	ins.State = state
	ins.CreatedAt = fromMs(created)
	ins.UpdatedAt = fromMs(updated)
	return ins, nil
}

// LatestInstruction implements Store.
func (tx *txStore) LatestInstruction(scope types.Scope) (types.Instruction, bool) {
	ins, err := scanInstruction(func(dest ...any) error {
		return tx.db.QueryRow(bg(), `SELECT id,scope_id,text,revision,origin_msg_id,status,created_at,updated_at
			FROM instructions WHERE scope_id=? ORDER BY revision DESC LIMIT 1`, scope.String()).Scan(dest...)
	})
	if err != nil {
		return types.Instruction{}, false
	}
	return ins, true
}

// InstructionText implements Store.
func (tx *txStore) InstructionText(scope types.Scope, rev int) (string, bool) {
	var text string
	err := tx.db.QueryRow(bg(), `SELECT text FROM instructions WHERE scope_id=? AND revision=?`, scope.String(), rev).Scan(&text)
	if err != nil {
		return "", false
	}
	return text, true
}

// SaveQuestion implements Store.
func (tx *txStore) SaveQuestion(q types.Question) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO questions(id,run_id,type,content_json,scope,blocking,default_value,deadline_ms,status,answer,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		q.ID, string(q.RunID), q.Kind, q.Content, "", boolToInt(q.Blocking), q.Default,
		ms(q.Deadline), q.State, q.Answer, ms(q.CreatedAt), ms(q.UpdatedAt))
	return wrapExists(err, fmt.Sprintf("question %q", q.ID))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanQuestion(scan func(...any) error) (types.Question, error) {
	var q types.Question
	var id, runID, kind, content, scope string
	var blocking int
	var def string
	var deadline int64
	var state, answer string
	var created, updated int64
	if err := scan(&id, &runID, &kind, &content, &scope, &blocking, &def, &deadline, &state, &answer, &created, &updated); err != nil {
		return types.Question{}, err
	}
	q.ID = id
	q.RunID = types.RunID(runID)
	q.Kind = kind
	q.Content = content
	q.Blocking = blocking != 0
	q.Default = def
	q.Deadline = fromMs(deadline)
	q.State = state
	q.Answer = answer
	q.CreatedAt = fromMs(created)
	q.UpdatedAt = fromMs(updated)
	return q, nil
}

const qCols = `id,run_id,type,content_json,scope,blocking,default_value,deadline_ms,status,answer,created_at,updated_at`

// GetQuestion implements Store.
func (tx *txStore) GetQuestion(id string) (types.Question, error) {
	q, err := scanQuestion(func(dest ...any) error {
		return tx.db.QueryRow(bg(), `SELECT `+qCols+` FROM questions WHERE id=?`, id).Scan(dest...)
	})
	if err != nil {
		if isNotFound(err) {
			return types.Question{}, fmt.Errorf("orchestrator: unknown question %q", id)
		}
		return types.Question{}, err
	}
	return q, nil
}

// PendingQuestions implements Store in id order.
func (tx *txStore) PendingQuestions(run types.RunID) []types.Question {
	rows, err := tx.db.Query(bg(), `SELECT `+qCols+` FROM questions WHERE run_id=? AND status='open' ORDER BY id`, string(run))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.Question
	for rows.Next() {
		q, err := scanQuestion(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, q)
	}
	return out
}

// AnswerQuestion implements Store.
func (tx *txStore) AnswerQuestion(id, answer string) error {
	ctx := bg()
	res, err := tx.db.Exec(ctx, `UPDATE questions SET status='answered',answer=?,updated_at=? WHERE id=? AND status='open'`,
		answer, ms(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, gerr := tx.GetQuestion(id); gerr != nil {
			return gerr
		}
	}
	return nil
}

// SaveReport implements Store.
func (tx *txStore) SaveReport(rep DeliveryReport) error {
	ctx := bg()
	state := string(rep.State)
	_, err := tx.db.Exec(ctx, `INSERT INTO run_reports(run_id,agent_id,state,reason,summary,deliverables_json,evidence_json,
		files_json,assumptions_json,queries_json,pending_json,usage_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(rep.RunID), string(rep.AgentID), state, rep.Reason, rep.Summary,
		mustJSON(rep.Deliverables), mustJSON(rep.Evidence), mustJSON(rep.FilesChanged),
		mustJSON(rep.Assumptions), mustJSON(rep.OpenQueries), mustJSON(rep.Pending),
		mustJSON(rep.Usage), ms(rep.CreatedAt))
	return err
}

func scanReport(scan func(...any) error) (DeliveryReport, error) {
	var rep DeliveryReport
	var runID, agentID, state, reason, summary string
	var delivRaw, evRaw, filesRaw, assumRaw, queriesRaw, pendingRaw, usageRaw string
	var created int64
	if err := scan(&runID, &agentID, &state, &reason, &summary, &delivRaw, &evRaw, &filesRaw,
		&assumRaw, &queriesRaw, &pendingRaw, &usageRaw, &created); err != nil {
		return DeliveryReport{}, err
	}
	rep.RunID = types.RunID(runID)
	rep.AgentID = types.AgentID(agentID)
	rep.State = types.RunState(state)
	rep.Reason = reason
	rep.Summary = summary
	_ = json.Unmarshal([]byte(delivRaw), &rep.Deliverables)
	_ = json.Unmarshal([]byte(evRaw), &rep.Evidence)
	_ = json.Unmarshal([]byte(filesRaw), &rep.FilesChanged)
	_ = json.Unmarshal([]byte(assumRaw), &rep.Assumptions)
	_ = json.Unmarshal([]byte(queriesRaw), &rep.OpenQueries)
	_ = json.Unmarshal([]byte(pendingRaw), &rep.Pending)
	_ = json.Unmarshal([]byte(usageRaw), &rep.Usage)
	rep.CreatedAt = fromMs(created)
	return rep, nil
}

const repCols = `run_id,agent_id,state,reason,summary,deliverables_json,evidence_json,files_json,assumptions_json,queries_json,pending_json,usage_json,created_at`

// LatestReport implements Store.
func (tx *txStore) LatestReport(run types.RunID) (DeliveryReport, bool) {
	rep, err := scanReport(func(dest ...any) error {
		return tx.db.QueryRow(bg(), `SELECT `+repCols+` FROM run_reports WHERE run_id=? ORDER BY id DESC LIMIT 1`, string(run)).Scan(dest...)
	})
	if err != nil {
		return DeliveryReport{}, false
	}
	return rep, true
}

// RecordUsage implements Store: usage accumulates per run.
func (tx *txStore) RecordUsage(run types.RunID, u Usage) error {
	ctx := bg()
	_, err := tx.db.Exec(ctx, `INSERT INTO run_usage(run_id,steps,input_tokens,output_tokens,cost_usd,elapsed_ms,tool_calls,updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(run_id) DO UPDATE SET steps=steps+excluded.steps,input_tokens=input_tokens+excluded.input_tokens,
		output_tokens=output_tokens+excluded.output_tokens,cost_usd=cost_usd+excluded.cost_usd,
		elapsed_ms=elapsed_ms+excluded.elapsed_ms,tool_calls=tool_calls+excluded.tool_calls,updated_at=excluded.updated_at`,
		string(run), u.Steps, u.InputTokens, u.OutputTokens, u.CostUSD, u.Elapsed.Milliseconds(), u.ToolCalls, ms(time.Now().UTC()))
	return err
}

// GetUsage implements Store.
func (tx *txStore) GetUsage(run types.RunID) Usage {
	var u Usage
	var elapsed int64
	err := tx.db.QueryRow(bg(), `SELECT steps,input_tokens,output_tokens,cost_usd,elapsed_ms,tool_calls FROM run_usage WHERE run_id=?`, string(run)).Scan(
		&u.Steps, &u.InputTokens, &u.OutputTokens, &u.CostUSD, &elapsed, &u.ToolCalls)
	if err != nil {
		return Usage{}
	}
	u.Elapsed = time.Duration(elapsed) * time.Millisecond
	return u
}

// --- SQLStore method set: one porter round trip per call ---

func (s *SQLStore) write(fn func(tx *txStore) error) error {
	return s.W.Write(context.Background(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		return fn(&txStore{db: ex})
	})
}

func (s *SQLStore) read(fn func(tx *txStore) error) error {
	return s.R.Read(context.Background(), func(ctx context.Context, q store.Queryer) error {
		return fn(&txStore{db: readOnly{q}})
	})
}

// SaveAgent implements Store.
func (s *SQLStore) SaveAgent(a types.Agent) error {
	return s.write(func(tx *txStore) error { return tx.SaveAgent(a) })
}

// GetAgent implements Store.
func (s *SQLStore) GetAgent(id types.AgentID) (types.Agent, error) {
	var out types.Agent
	return out, s.read(func(tx *txStore) error {
		var err error
		out, err = tx.GetAgent(id)
		return err
	})
}

// ListAgents implements Store.
func (s *SQLStore) ListAgents() []types.Agent {
	var out []types.Agent
	_ = s.read(func(tx *txStore) error {
		out = tx.ListAgents()
		return nil
	})
	return out
}

// Children implements Store.
func (s *SQLStore) Children(parent types.AgentID) []types.Agent {
	var out []types.Agent
	_ = s.read(func(tx *txStore) error {
		out = tx.Children(parent)
		return nil
	})
	return out
}

// FindAgentsByName implements Store.
func (s *SQLStore) FindAgentsByName(name string) []types.Agent {
	var out []types.Agent
	_ = s.read(func(tx *txStore) error {
		out = tx.FindAgentsByName(name)
		return nil
	})
	return out
}

// CreateRun implements Store.
func (s *SQLStore) CreateRun(r RunRecord) error {
	return s.write(func(tx *txStore) error { return tx.CreateRun(r) })
}

// GetRun implements Store.
func (s *SQLStore) GetRun(id types.RunID) (RunRecord, error) {
	var out RunRecord
	return out, s.read(func(tx *txStore) error {
		var err error
		out, err = tx.GetRun(id)
		return err
	})
}

// UpdateRun implements Store.
func (s *SQLStore) UpdateRun(r RunRecord) error {
	return s.write(func(tx *txStore) error { return tx.UpdateRun(r) })
}

// ListRuns implements Store.
func (s *SQLStore) ListRuns() []RunRecord {
	var out []RunRecord
	_ = s.read(func(tx *txStore) error {
		out = tx.ListRuns()
		return nil
	})
	return out
}

// RunsByState implements Store.
func (s *SQLStore) RunsByState(st types.RunState) []RunRecord {
	var out []RunRecord
	_ = s.read(func(tx *txStore) error {
		out = tx.RunsByState(st)
		return nil
	})
	return out
}

// AppendMessage implements Store.
func (s *SQLStore) AppendMessage(m Message) error {
	return s.write(func(tx *txStore) error { return tx.AppendMessage(m) })
}

// GetMessage implements Store.
func (s *SQLStore) GetMessage(id types.MessageID) (Message, error) {
	var out Message
	return out, s.read(func(tx *txStore) error {
		var err error
		out, err = tx.GetMessage(id)
		return err
	})
}

// PendingMessages implements Store.
func (s *SQLStore) PendingMessages(to types.AgentID) []Message {
	var out []Message
	_ = s.read(func(tx *txStore) error {
		out = tx.PendingMessages(to)
		return nil
	})
	return out
}

// UnackedDirectives implements Store.
func (s *SQLStore) UnackedDirectives(to types.AgentID) []Message {
	var out []Message
	_ = s.read(func(tx *txStore) error {
		out = tx.UnackedDirectives(to)
		return nil
	})
	return out
}

// AckMessage implements Store.
func (s *SQLStore) AckMessage(id types.MessageID, note string) error {
	return s.write(func(tx *txStore) error { return tx.AckMessage(id, note) })
}

// SaveInstruction implements Store.
func (s *SQLStore) SaveInstruction(ins types.Instruction) error {
	return s.write(func(tx *txStore) error { return tx.SaveInstruction(ins) })
}

// LatestInstruction implements Store.
func (s *SQLStore) LatestInstruction(scope types.Scope) (types.Instruction, bool) {
	var out types.Instruction
	var ok bool
	_ = s.read(func(tx *txStore) error {
		out, ok = tx.LatestInstruction(scope)
		return nil
	})
	return out, ok
}

// InstructionText implements Store.
func (s *SQLStore) InstructionText(scope types.Scope, rev int) (string, bool) {
	var out string
	var ok bool
	_ = s.read(func(tx *txStore) error {
		out, ok = tx.InstructionText(scope, rev)
		return nil
	})
	return out, ok
}

// SaveQuestion implements Store.
func (s *SQLStore) SaveQuestion(q types.Question) error {
	return s.write(func(tx *txStore) error { return tx.SaveQuestion(q) })
}

// GetQuestion implements Store.
func (s *SQLStore) GetQuestion(id string) (types.Question, error) {
	var out types.Question
	return out, s.read(func(tx *txStore) error {
		var err error
		out, err = tx.GetQuestion(id)
		return err
	})
}

// PendingQuestions implements Store.
func (s *SQLStore) PendingQuestions(run types.RunID) []types.Question {
	var out []types.Question
	_ = s.read(func(tx *txStore) error {
		out = tx.PendingQuestions(run)
		return nil
	})
	return out
}

// AnswerQuestion implements Store.
func (s *SQLStore) AnswerQuestion(id, answer string) error {
	return s.write(func(tx *txStore) error { return tx.AnswerQuestion(id, answer) })
}

// SaveReport implements Store.
func (s *SQLStore) SaveReport(rep DeliveryReport) error {
	return s.write(func(tx *txStore) error { return tx.SaveReport(rep) })
}

// LatestReport implements Store.
func (s *SQLStore) LatestReport(run types.RunID) (DeliveryReport, bool) {
	var out DeliveryReport
	var ok bool
	_ = s.read(func(tx *txStore) error {
		out, ok = tx.LatestReport(run)
		return nil
	})
	return out, ok
}

// RecordUsage implements Store.
func (s *SQLStore) RecordUsage(run types.RunID, u Usage) error {
	return s.write(func(tx *txStore) error { return tx.RecordUsage(run, u) })
}

// GetUsage implements Store.
func (s *SQLStore) GetUsage(run types.RunID) Usage {
	var out Usage
	_ = s.read(func(tx *txStore) error {
		out = tx.GetUsage(run)
		return nil
	})
	return out
}

// Compile-time check: SQLStore and txStore both satisfy Store.
var (
	_ Store = (*SQLStore)(nil)
	_ Store = (*txStore)(nil)
)
