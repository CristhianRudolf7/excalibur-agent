package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TaskService implements the task.* gate family through the daemon
// (Section 3.6): tasks created at the gate stay visible to the master
// and every client because each task is an orchestrator run. The CLI
// and the MCP server call this surface when the daemon is active and
// fall back to embedded file-backed execution otherwise.
type TaskService struct {
	orch   *orchestrator.Orchestrator
	store  orchestrator.Store
	parent types.AgentID

	mu    sync.Mutex
	tasks map[types.TaskID]*taskRecord
	now   func() time.Time
}

type taskRecord struct {
	id         types.TaskID
	runID      types.RunID
	request    string
	projectDir string
	profile    string
	created    time.Time
	log        []string
}

// NewTaskService returns the daemon task surface. It registers a
// daemon-owned parent agent once; tasks spawn as its children.
func NewTaskService(orch *orchestrator.Orchestrator, store orchestrator.Store) (*TaskService, error) {
	if orch == nil {
		return nil, fmt.Errorf("daemon: task service needs an orchestrator")
	}
	agent, err := orch.RegisterMaster("tasks", types.GeneralScope())
	if err != nil {
		return nil, fmt.Errorf("daemon: register task parent: %w", err)
	}
	return &TaskService{
		orch: orch, store: store, parent: agent.ID,
		tasks: map[types.TaskID]*taskRecord{},
		now:   time.Now,
	}, nil
}

// Start creates a delegated task and returns immediately (Section
// 3.6): the task is a queued coordinator run carrying the request.
func (s *TaskService) Start(p api.TaskStartParams) (api.TaskStartResult, error) {
	if p.Request == "" {
		return api.TaskStartResult{}, &api.Error{Code: api.CodeInvalidParams, Message: "missing request"}
	}
	profile := p.Profile
	if profile == "" {
		profile = "delegate"
	}
	id := types.NewTaskID()
	maxSteps := 0
	if p.MaxSteps != nil {
		maxSteps = *p.MaxSteps
	}
	_ = maxSteps
	card := orchestrator.DelegationCard{
		Name:        "task-" + string(id),
		Description: "delegated task",
		Goal:        p.Request,
		Deliverable: "delivery report: summary, files, criteria with evidence, pending",
		Scope:       types.GeneralScope(),
		Done:        []string{"delivery report recorded"},
		Report:      "what ran, what changed, what is pending",
		Deadline:    s.now().Add(4 * time.Hour),
	}
	_, rec, err := s.orch.Spawn(s.parent, card)
	if err != nil {
		return api.TaskStartResult{}, &api.Error{Code: api.CodeInternal, Message: err.Error()}
	}
	s.mu.Lock()
	s.tasks[id] = &taskRecord{
		id: id, runID: rec.Run.ID,
		request: p.Request, projectDir: p.ProjectDir, profile: profile,
		created: s.now().UTC(),
		log:     []string{"created " + s.now().UTC().Format(time.RFC3339)},
	}
	s.mu.Unlock()
	return api.TaskStartResult{TaskID: id, Status: types.DeriveTaskState(rec.Run.State)}, nil
}

// lookup resolves a task id or returns an unknown-target error.
func (s *TaskService) lookup(id types.TaskID) (*taskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.tasks[id]
	if !ok {
		return nil, &api.Error{Code: api.CodeUnknownTarget, Message: fmt.Sprintf("unknown task %q", id)}
	}
	return rec, nil
}

// RunOf resolves the orchestrator run carrying a task.
func (s *TaskService) RunOf(id types.TaskID) (types.RunID, error) {
	rec, err := s.lookup(id)
	if err != nil {
		return "", err
	}
	return rec.runID, nil
}

// snapshot builds the status view of one task.
func (s *TaskService) snapshot(rec *taskRecord) (map[string]any, error) {
	st, err := s.orch.Status(rec.runID)
	if err != nil {
		return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
	}
	snap := st.ToAPI()
	out := map[string]any{
		"task_id":     string(rec.id),
		"status":      string(types.DeriveTaskState(snap.State)),
		"run_id":      snap.RunID,
		"run_state":   string(snap.State),
		"last_line":   snap.LiveOutput,
		"progress":    snap.LastProgress,
		"plan_done":   snap.PlanDone,
		"plan_total":  snap.PlanTotal,
		"requested":   rec.request,
		"project_dir": rec.projectDir,
	}
	if qs := s.store.PendingQuestions(rec.runID); len(qs) > 0 {
		out["question"] = qs[0].Content
		out["question_id"] = qs[0].ID
		out["status"] = string(types.TaskBlocked)
	}
	return out, nil
}

// Status implements task.status: the 15.6 snapshot plus the pending
// question when the task waits on the user.
func (s *TaskService) Status(id types.TaskID) (map[string]any, error) {
	rec, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	return s.snapshot(rec)
}

// Wait implements task.wait: it returns when the run terminates, when
// a question needs an answer, or when the timeout expires. A question
// returns early with the question attached (Section 3.6).
func (s *TaskService) Wait(ctx context.Context, id types.TaskID, timeout time.Duration) (map[string]any, error) {
	rec, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	if timeout <= 0 {
		deadline = time.Now().Add(10 * time.Minute)
	}
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		snap, serr := s.snapshot(rec)
		if serr != nil {
			return nil, serr
		}
		if _, hasQ := snap["question"]; hasQ {
			return snap, nil
		}
		state, _ := snap["run_state"].(string)
		if types.RunState(state).IsTerminal() {
			return snap, nil
		}
		select {
		case <-ctx.Done():
			return snap, nil
		case <-t.C:
			if time.Now().After(deadline) {
				snap["timeout"] = true
				return snap, nil
			}
		}
	}
}

// Result implements task.result: the delivery report (Section 7.6)
// assembled from the run record and its snapshot.
func (s *TaskService) Result(id types.TaskID) (map[string]any, error) {
	rec, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	snap, err := s.snapshot(rec)
	if err != nil {
		return nil, err
	}
	state, _ := snap["run_state"].(string)
	done := types.RunState(state).IsTerminal()
	return map[string]any{
		"task_id":    string(rec.id),
		"done":       done,
		"summary":    snap["progress"],
		"run_state":  state,
		"last_line":  snap["last_line"],
		"pending":    snap["question"],
		"requested":  rec.request,
		"created_at": rec.created.UTC().Format(time.RFC3339),
	}, nil
}

// Reply implements task.reply: a pending question is answered, other
// text arrives as a redirect directive (Section 15.7).
func (s *TaskService) Reply(id types.TaskID, text string) (map[string]any, error) {
	rec, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	if qs := s.store.PendingQuestions(rec.runID); len(qs) > 0 {
		if err := s.store.AnswerQuestion(qs[0].ID, text); err != nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
		}
		s.appendLog(rec, "answered "+qs[0].ID)
		return map[string]any{"task_id": string(id), "ok": true, "answered": qs[0].ID}, nil
	}
	if err := s.orch.Redirect(rec.runID, text, orchestrator.ActionReplan, false); err != nil {
		return nil, &api.Error{Code: api.CodeConflict, Message: err.Error()}
	}
	s.appendLog(rec, "redirect: "+text)
	return map[string]any{"task_id": string(id), "ok": true, "redirected": true}, nil
}

// Pause implements task.pause.
func (s *TaskService) Pause(id types.TaskID) (map[string]any, error) {
	return s.control(id, "paused", func(run types.RunID) error {
		return s.orch.Pause(run, types.PauseUser, "")
	})
}

// Resume implements task.resume.
func (s *TaskService) Resume(id types.TaskID) (map[string]any, error) {
	return s.control(id, "resumed", func(run types.RunID) error {
		return s.orch.Resume(run, "")
	})
}

// Cancel implements task.cancel: bottom-up with confirmation semantics
// inherited from the orchestrator (Section 15.8).
func (s *TaskService) Cancel(id types.TaskID) (map[string]any, error) {
	return s.control(id, "canceled", s.orch.Cancel)
}

func (s *TaskService) control(id types.TaskID, verb string, fn func(types.RunID) error) (map[string]any, error) {
	rec, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	if err := fn(rec.runID); err != nil {
		return nil, &api.Error{Code: api.CodeConflict, Message: err.Error()}
	}
	s.appendLog(rec, verb)
	return s.snapshot(rec)
}

// List implements task.list: every known task with its live status.
func (s *TaskService) List() ([]map[string]any, error) {
	s.mu.Lock()
	ids := make([]types.TaskID, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rec, err := s.lookup(id)
		if err != nil {
			continue
		}
		snap, err := s.snapshot(rec)
		if err != nil {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

// Logs implements task.logs: the daemon-side event lines of one task
// as JSONL-ready maps.
func (s *TaskService) Logs(id types.TaskID) ([]map[string]any, error) {
	rec, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(rec.log))
	for _, line := range rec.log {
		out = append(out, map[string]any{"task_id": string(id), "line": line})
	}
	return out, nil
}

func (s *TaskService) appendLog(rec *taskRecord, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.log = append(rec.log, s.now().UTC().Format(time.RFC3339)+" "+line)
}

// TaskOp routes one task.* gate call to the service. Result is
// already JSON-marshalable.
func (s *TaskService) TaskOp(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case api.MethodTaskStart:
		var p api.TaskStartParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad task.start params"}
			}
		}
		return s.Start(p)
	case api.MethodTaskList:
		return s.List()
	}
	id, timeout, text, err := taskParams(method, params)
	if err != nil {
		return nil, err
	}
	switch method {
	case api.MethodTaskStatus:
		return s.Status(id)
	case api.MethodTaskWait:
		return s.Wait(ctx, id, timeout)
	case api.MethodTaskResult:
		return s.Result(id)
	case api.MethodTaskReply:
		return s.Reply(id, text)
	case api.MethodTaskPause:
		return s.Pause(id)
	case api.MethodTaskResume:
		return s.Resume(id)
	case api.MethodTaskCancel:
		return s.Cancel(id)
	case api.MethodTaskLogs:
		return s.Logs(id)
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

// taskParams extracts the task id plus wait timeout or reply text
// from the Section 3.6 shapes.
func taskParams(method string, params json.RawMessage) (types.TaskID, time.Duration, string, error) {
	switch method {
	case api.MethodTaskWait:
		var p api.TaskWaitParams
		if err := json.Unmarshal(params, &p); err != nil {
			return "", 0, "", &api.Error{Code: api.CodeInvalidParams, Message: "bad task.wait params"}
		}
		if p.TaskID == "" {
			return "", 0, "", &api.Error{Code: api.CodeInvalidParams, Message: "missing task_id"}
		}
		timeout := time.Duration(p.TimeoutSecs) * time.Second
		if p.TimeoutSecs <= 0 {
			timeout = 10 * time.Minute
		}
		if timeout > 10*time.Minute {
			timeout = 10 * time.Minute
		}
		return p.TaskID, timeout, "", nil
	case api.MethodTaskReply:
		var p api.TaskReplyParams
		if err := json.Unmarshal(params, &p); err != nil {
			return "", 0, "", &api.Error{Code: api.CodeInvalidParams, Message: "bad task.reply params"}
		}
		if p.TaskID == "" {
			return "", 0, "", &api.Error{Code: api.CodeInvalidParams, Message: "missing task_id"}
		}
		return p.TaskID, 0, p.Text, nil
	default:
		var probe struct {
			TaskID types.TaskID `json:"task_id"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &probe); err != nil {
				return "", 0, "", &api.Error{Code: api.CodeInvalidParams, Message: "bad params"}
			}
		}
		if probe.TaskID == "" {
			return "", 0, "", &api.Error{Code: api.CodeInvalidParams, Message: "missing task_id"}
		}
		return probe.TaskID, 0, "", nil
	}
}
