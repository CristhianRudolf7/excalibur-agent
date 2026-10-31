package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// InlineBackground forces Start to run the task in-process instead of
// spawning a background child. Tests set it; the CLI always spawns so
// `start` returns immediately while work continues in the background.
var InlineBackground bool

// StartOptions mirrors `excalibur task start` (Section 3.6).
type StartOptions struct {
	Request    string
	ProjectDir string
	Profile    string
	NoMemory   bool
	Worktree   *bool
	BudgetUSD  *float64
	MaxSteps   *int
	TimeoutS   *int64
	NeverAsk   bool
	// Inline runs the task in-process instead of spawning a
	// background child. Tests use it; the CLI always spawns so
	// `start` returns immediately.
	Inline bool
	// Binary overrides the re-executed binary for the background
	// child (tests point at a helper).
	Binary string
}

// StartResult is the immediate `task start` receipt.
type StartResult struct {
	TaskID types.TaskID    `json:"task_id"`
	Short  string          `json:"short"`
	Status types.TaskState `json:"status"`
}

// Start creates a task and returns immediately. Without a daemon the
// task runs as a detached background `excalibur` process with its
// database in the task directory; status, wait and result read the
// same files (Section 3.6: delegation works from H8, before H9).
func Start(opts StartOptions) (*StartResult, error) {
	profile, err := ParseProfile(opts.Profile)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if profile == "" {
		profile = ProfileDefault
	}
	if profile == ProfileDefault {
		profile = ProfileDelegate
	}
	store, err := ResolveStore(opts.ProjectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	short, err := store.NextShort()
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	projectDir := opts.ProjectDir
	if projectDir != "" {
		abs, err := filepath.Abs(projectDir)
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: project dir: %w", err)}
		}
		projectDir = abs
	}
	worktree := shouldWorktree(opts.Worktree, projectDir)
	rec := &TaskRecord{
		TaskID:     types.NewTaskID(),
		Short:      short,
		Request:    opts.Request,
		Profile:    profile,
		ProjectDir: projectDir,
		NoMemory:   opts.NoMemory,
		Worktree:   worktree,
		State:      types.TaskOpen,
		RunID:      types.NewRunID(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if opts.BudgetUSD != nil {
		v := *opts.BudgetUSD
		rec.BudgetUSD = &v
		rec.Budget.MaxCostUSD = v
	}
	if opts.MaxSteps != nil {
		rec.MaxSteps = *opts.MaxSteps
		rec.Budget.MaxSteps = *opts.MaxSteps
	}
	if opts.TimeoutS != nil {
		rec.TimeoutS = *opts.TimeoutS
		rec.Budget.MaxSeconds = *opts.TimeoutS
	}
	rec.NeverAsk = opts.NeverAsk
	if err := store.Save(rec); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if err := store.AppendEvent(rec.TaskID, map[string]any{
		"event": "task_started", "task_id": string(rec.TaskID),
		"short": short, "at": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if opts.Inline || InlineBackground {
		if err := runTaskRecord(store, rec.Short); err != nil {
			return nil, err
		}
		updated, err := store.Load(string(rec.TaskID))
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		return &StartResult{TaskID: updated.TaskID, Short: updated.Short, Status: updated.State}, nil
	}
	pid, err := spawnBackground(opts.Binary, store, rec)
	if err != nil {
		rec.State = types.TaskFailed
		rec.Reason = "spawn"
		rec.Summary = fmt.Sprintf("failed to start background task: %v", err)
		_ = store.Save(rec)
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	rec.PID = pid
	rec.State = types.TaskInProgress
	if err := store.Save(rec); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	return &StartResult{TaskID: rec.TaskID, Short: rec.Short, Status: rec.State}, nil
}

func shouldWorktree(flag *bool, projectDir string) bool {
	if flag != nil {
		return *flag
	}
	if projectDir == "" {
		return false
	}
	// Worktree is on by default inside a git repository so the
	// delegating tree never changes (Section 3.6).
	if st, err := os.Stat(filepath.Join(projectDir, ".git")); err == nil && st.IsDir() {
		return true
	}
	return false
}

func spawnBackground(binary string, store Store, rec *TaskRecord) (int, error) {
	if binary == "" {
		self, err := os.Executable()
		if err != nil {
			return 0, fmt.Errorf("cli: current binary: %w", err)
		}
		binary = self
	}
	taskFile := filepath.Join(store.TaskPath(rec.TaskID), "task.json")
	logFile := filepath.Join(store.TaskPath(rec.TaskID), "child.log")
	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("cli: child log: %w", err)
	}
	defer log.Close()
	cmd := exec.Command(binary, "__run-task", taskFile)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = newChildAttr()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("cli: spawn task: %w", err)
	}
	return cmd.Process.Pid, nil
}

// RunTaskFile executes the background child for one task file: the
// `__run-task` hidden command target. It is idempotent: a record that
// is already terminal, paused or canceled is left alone.
func RunTaskFile(taskFile string) error {
	raw, err := os.ReadFile(taskFile)
	if err != nil {
		return fmt.Errorf("cli: read task file: %w", err)
	}
	var rec TaskRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return fmt.Errorf("cli: parse task file: %w", err)
	}
	taskFileDir := filepath.Dir(taskFile)
	// taskFile is <base>/tasks/<task_id>/task.json: three levels up
	// is the store base the daemon adopts too.
	store := Store{Base: filepath.Dir(filepath.Dir(taskFileDir))}
	return runTaskRecord(store, string(rec.TaskID))
}

func runTaskRecord(store Store, id string) error {
	rec, err := store.Load(id)
	if err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}
	if terminalTask(rec.State) || rec.State == types.TaskPaused {
		return nil
	}
	// A parked question answered while the child was away resumes
	// below; a fresh run starts in progress.
	if rec.State == types.TaskOpen || rec.State == types.TaskInProgress || rec.State == types.TaskBlocked {
		rec.State = types.TaskInProgress
	}
	if err := store.Save(rec); err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}
	if err := store.AppendEvent(rec.TaskID, map[string]any{
		"event": "run_started", "task_id": string(rec.TaskID),
		"at": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}

	workDir, branch, err := prepareWorktree(store, rec)
	if err != nil {
		return failTask(store, rec, "worktree", fmt.Sprintf("worktree setup failed: %v", err))
	}

	timeout := rec.TimeoutS
	ctx := context.Background()
	if rec.Answer != "" {
		// Resume after a reply: the pending question is answered.
	}
	execOpts := ExecOptions{
		Request:     rec.Request,
		ProjectDir:  workDir,
		Profile:     rec.Profile,
		NoMemory:    rec.NoMemory,
		RunDir:      store.TaskPath(rec.TaskID),
		TaskID:      rec.TaskID,
		RunID:       rec.RunID,
		AllowAsk:    true,
		NeverAsk:    rec.NeverAsk,
		MaxSteps:    rec.MaxSteps,
		BudgetUSD:   rec.BudgetUSD,
		TimeoutSecs: timeout,
	}
	outcome, err := RunExec(ctx, execOpts)
	if err != nil {
		return failTask(store, rec, "harness", err.Error())
	}
	rec.Assumed = outcome.Assumptions
	if branch != "" {
		rec.Branch = branch
		rec.DiffStat, rec.Files = worktreeDiff(workDir, rec)
	}
	switch outcome.State {
	case types.RunCompleted, types.RunCompletedPending,
		types.RunBlocked, types.RunFailed, types.RunCanceled, types.RunPaused:
		rec.State = types.DeriveTaskState(outcome.State)
		rec.Reason = outcome.Reason
		rec.Summary = outcome.Summary
		rec.Question = ""
	default:
		// Waiting: keep the question for wait/reply.
		rec.State = types.TaskInProgress
		rec.Question = outcome.Question
		rec.Summary = outcome.Summary
	}
	if outcome.State == types.RunWaiting {
		rec.Question = outcome.Question
	}
	rec.Result = map[string]any{
		"state":   string(outcome.State),
		"summary": outcome.Summary,
		"branch":  rec.Branch,
	}
	if err := store.Save(rec); err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}
	ev := "run_finished"
	if outcome.State == types.RunWaiting {
		ev = "question_asked"
	}
	return store.AppendEvent(rec.TaskID, map[string]any{
		"event": ev, "task_id": string(rec.TaskID),
		"state": string(outcome.State), "summary": outcome.Summary,
		"question": outcome.Question,
		"at":       time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func terminalTask(s types.TaskState) bool {
	switch s {
	case types.TaskCompleted, types.TaskCompletedPending,
		types.TaskFailed, types.TaskCanceled:
		return true
	default:
		return false
	}
}

func failTask(store Store, rec *TaskRecord, reason, summary string) error {
	rec.State = types.TaskFailed
	rec.Reason = reason
	rec.Summary = summary
	if err := store.Save(rec); err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}
	_ = store.AppendEvent(rec.TaskID, map[string]any{
		"event": "run_finished", "task_id": string(rec.TaskID),
		"state": string(rec.State), "summary": summary,
		"at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	return &CodedError{Code: ExitFailed, Err: fmt.Errorf("cli: task %s: %s", rec.Short, summary)}
}

// TaskStatus is the Section 15.6 snapshot served by `task status`.
type TaskStatus struct {
	TaskID    types.TaskID    `json:"task_id"`
	Short     string          `json:"short"`
	State     types.TaskState `json:"state"`
	Reason    string          `json:"reason,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Question  string          `json:"question,omitempty"`
	Branch    string          `json:"branch,omitempty"`
	Profile   ProfileName     `json:"profile"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Status returns the snapshot of one task.
func Status(projectDir, id string) (*TaskStatus, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	rec, err := store.Load(id)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	return &TaskStatus{
		TaskID: rec.TaskID, Short: rec.Short, State: rec.State,
		Reason: rec.Reason, Summary: rec.Summary, Question: rec.Question,
		Branch: rec.Branch, Profile: rec.Profile, UpdatedAt: rec.UpdatedAt,
	}, nil
}

// Wait blocks until the task is terminal or asks a question, or the
// timeout expires (Section 3.6: wait returns the state; the caller
// maps it to the exec exit code).
func Wait(projectDir, id string, timeoutSecs int64) (*TaskStatus, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)
	if timeoutSecs <= 0 {
		deadline = time.Now().Add(10 * time.Second)
	}
	for {
		rec, err := store.Load(id)
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		st := &TaskStatus{
			TaskID: rec.TaskID, Short: rec.Short, State: rec.State,
			Reason: rec.Reason, Summary: rec.Summary, Question: rec.Question,
			Branch: rec.Branch, Profile: rec.Profile, UpdatedAt: rec.UpdatedAt,
		}
		if terminalTask(rec.State) || rec.Question != "" {
			return st, nil
		}
		if time.Now().After(deadline) {
			return st, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TaskResult is the Section 7.6 delivery report served by
// `task result`: summary, files changed, branch or diff, criteria
// with evidence, and pending items.
type TaskResult struct {
	TaskID       types.TaskID    `json:"task_id"`
	Short        string          `json:"short"`
	State        types.TaskState `json:"state"`
	Summary      string          `json:"summary"`
	FilesChanged []string        `json:"files_changed,omitempty"`
	Branch       string          `json:"branch,omitempty"`
	DiffStat     string          `json:"diff_stat,omitempty"`
	Assumptions  []string        `json:"assumptions,omitempty"`
	Reason       string          `json:"reason,omitempty"`
}

// Result returns the delivery report of one task.
func Result(projectDir, id string) (*TaskResult, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	rec, err := store.Load(id)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	return &TaskResult{
		TaskID: rec.TaskID, Short: rec.Short, State: rec.State,
		Summary: rec.Summary, FilesChanged: rec.Files, Branch: rec.Branch,
		DiffStat: rec.DiffStat, Assumptions: rec.Assumed, Reason: rec.Reason,
	}, nil
}

// Reply answers a pending question or adds a directive (Section
// 3.6/15.7): the answer is stored and the task resumes in the
// background. With no pending question the text is recorded as a
// directive for the next step.
func Reply(projectDir, id, text string) (*TaskStatus, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	rec, err := store.Load(id)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if terminalTask(rec.State) {
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: task %s is %s", rec.Short, rec.State)}
	}
	if strings.TrimSpace(text) == "" {
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: reply text must not be empty")}
	}
	rec.Answer = text
	wasQuestion := rec.Question != ""
	rec.Question = ""
	if wasQuestion {
		// The answer resolves the open question: resume the run
		// inline in a follow-up pass that assumes the answer. The
		// answered marker is consumed so the resume does not park
		// on the same question again.
		rec.Request = stripFirstAsk(rec.Request) + "\nAnswer: " + text
		rec.State = types.TaskInProgress
		if err := store.Save(rec); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		_ = store.AppendEvent(rec.TaskID, map[string]any{
			"event": "answer", "task_id": string(rec.TaskID),
			"answer": text, "at": time.Now().UTC().Format(time.RFC3339Nano),
		})
		if err := runTaskRecord(store, string(rec.TaskID)); err != nil {
			return nil, err
		}
	} else {
		// A directive for the next step (Section 15.7): recorded
		// and applied on resume.
		rec.Request = rec.Request + "\nDirective: " + text
		if err := store.Save(rec); err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		_ = store.AppendEvent(rec.TaskID, map[string]any{
			"event": "directive", "task_id": string(rec.TaskID),
			"text": text, "at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	updated, err := store.Load(string(rec.TaskID))
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	return &TaskStatus{
		TaskID: updated.TaskID, Short: updated.Short, State: updated.State,
		Reason: updated.Reason, Summary: updated.Summary, Question: updated.Question,
		Branch: updated.Branch, Profile: updated.Profile, UpdatedAt: updated.UpdatedAt,
	}, nil
}

// Cancel cancels a task and its background child (Section 15.8).
func Cancel(projectDir, id string) (*TaskStatus, error) {
	return control(projectDir, id, types.TaskCanceled, "canceled", "canceled by user")
}

// Pause pauses a task (Section 15.8).
func Pause(projectDir, id string) (*TaskStatus, error) {
	return control(projectDir, id, types.TaskPaused, "paused", "paused by user")
}

// Resume resumes what pause paused (Section 15.8).
func Resume(projectDir, id string) (*TaskStatus, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	rec, err := store.Load(id)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if rec.State != types.TaskPaused && rec.State != types.TaskBlocked {
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: task %s is %s, only paused or blocked tasks resume", rec.Short, rec.State)}
	}
	killChild(rec)
	rec.State = types.TaskInProgress
	rec.Reason = ""
	if err := store.Save(rec); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	_ = store.AppendEvent(rec.TaskID, map[string]any{
		"event": "resumed", "task_id": string(rec.TaskID),
		"at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := runTaskRecord(store, string(rec.TaskID)); err != nil {
		return nil, err
	}
	return Status(projectDir, string(rec.TaskID))
}

func control(projectDir, id string, state types.TaskState, reason, summary string) (*TaskStatus, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	rec, err := store.Load(id)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	if terminalTask(rec.State) {
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf("cli: task %s is already %s", rec.Short, rec.State)}
	}
	killChild(rec)
	rec.State = state
	rec.Reason = reason
	rec.Summary = summary
	rec.Question = ""
	if err := store.Save(rec); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	_ = store.AppendEvent(rec.TaskID, map[string]any{
		"event": string(state), "task_id": string(rec.TaskID),
		"at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	return Status(projectDir, string(rec.TaskID))
}

// killChild stops the background process of a task, if it is still
// running. A missing process is not an error: the child may have
// finished between status and control.
func killChild(rec *TaskRecord) {
	if rec.PID <= 0 {
		return
	}
	proc, err := os.FindProcess(rec.PID)
	if err != nil {
		return
	}
	_ = proc.Kill()
}

// ListTasks lists tasks, optionally filtered by project.
func ListTasks(projectDir string) ([]*TaskStatus, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	recs, err := store.List()
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	out := make([]*TaskStatus, 0, len(recs))
	for _, rec := range recs {
		out = append(out, &TaskStatus{
			TaskID: rec.TaskID, Short: rec.Short, State: rec.State,
			Reason: rec.Reason, Summary: rec.Summary, Question: rec.Question,
			Branch: rec.Branch, Profile: rec.Profile, UpdatedAt: rec.UpdatedAt,
		})
	}
	return out, nil
}

// TaskLogs returns the live event stream of a task as JSONL records.
func TaskLogs(projectDir, id string) ([]map[string]any, error) {
	store, err := ResolveStore(projectDir)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	rec, err := store.Load(id)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	return store.ReadEvents(rec.TaskID)
}
