package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TaskRecord is the persisted form of one delegated task (Section
// 3.6). Without a daemon, every task runs as a background
// `excalibur exec` process with its database in the task directory;
// status, wait and result read the same files, so delegation works
// from H8, before the daemon (H9) exists. When the daemon starts it
// adopts these directories with the 17.3 startup algorithm; the file
// layout is the adoption contract.
type TaskRecord struct {
	TaskID     types.TaskID    `json:"task_id"`
	Short      string          `json:"short"`
	Request    string          `json:"request"`
	Profile    ProfileName     `json:"profile"`
	ProjectDir string          `json:"project_dir,omitempty"`
	NoMemory   bool            `json:"no_memory,omitempty"`
	Worktree   bool            `json:"worktree"`
	Branch     string          `json:"branch,omitempty"`
	BudgetUSD  *float64        `json:"budget_usd,omitempty"`
	MaxSteps   int             `json:"max_steps,omitempty"`
	TimeoutS   int64           `json:"timeout_s,omitempty"`
	NeverAsk   bool            `json:"never_ask,omitempty"`
	Budget     types.Budget    `json:"budget"`
	State      types.TaskState `json:"state"`
	RunID      types.RunID     `json:"run_id"`
	Reason     string          `json:"reason,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	Question   string          `json:"question,omitempty"`
	Answer     string          `json:"answer,omitempty"`
	Assumed    []string        `json:"assumed,omitempty"`
	Files      []string        `json:"files_changed,omitempty"`
	DiffStat   string          `json:"diff_stat,omitempty"`
	Result     map[string]any  `json:"result,omitempty"`
	PID        int             `json:"pid,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// Store is the file-backed task directory: <base>/tasks/<task_id>/.
// <base> is <project>/.excalibur for project tasks or the user state
// directory otherwise (Section 3.6).
type Store struct {
	Base string
}

// TasksDir returns the task container directory, creating it.
func (s Store) TasksDir() (string, error) {
	dir := filepath.Join(s.Base, "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cli: tasks dir: %w", err)
	}
	return dir, nil
}

// TaskPath returns the directory of one task.
func (s Store) TaskPath(id types.TaskID) string {
	return filepath.Join(s.Base, "tasks", string(id))
}

// Save persists a record atomically (temp + rename).
func (s Store) Save(rec *TaskRecord) error {
	rec.UpdatedAt = time.Now().UTC()
	dir := s.TaskPath(rec.TaskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cli: task dir: %w", err)
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("cli: marshal task: %w", err)
	}
	tmp := filepath.Join(dir, "task.json.tmp")
	if err := os.WriteFile(tmp, append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("cli: write task: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "task.json")); err != nil {
		return fmt.Errorf("cli: rename task: %w", err)
	}
	return nil
}

// Load reads a record by full id or short code (T45).
func (s Store) Load(id string) (*TaskRecord, error) {
	if strings.HasPrefix(id, "T") && !strings.Contains(id, "_") {
		rec, err := s.loadByShort(id)
		if err == nil {
			return rec, nil
		}
	}
	raw, err := os.ReadFile(filepath.Join(s.TaskPath(types.TaskID(id)), "task.json"))
	if err != nil {
		return nil, fmt.Errorf("cli: unknown task %q", id)
	}
	var rec TaskRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("cli: parse task %q: %w", id, err)
	}
	return &rec, nil
}

func (s Store) loadByShort(short string) (*TaskRecord, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, rec := range all {
		if rec.Short == short {
			return rec, nil
		}
	}
	return nil, fmt.Errorf("cli: unknown task %q", short)
}

// List returns all records in short-code order.
func (s Store) List() ([]*TaskRecord, error) {
	dir, err := s.TasksDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cli: list tasks: %w", err)
	}
	var out []*TaskRecord
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rec, err := s.Load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Short < out[j].Short })
	return out, nil
}

// NextShort allocates the next T-numbered short code.
func (s Store) NextShort() (string, error) {
	all, err := s.List()
	if err != nil {
		return "", err
	}
	max := 0
	for _, rec := range all {
		var n int
		if _, err := fmt.Sscanf(rec.Short, "T%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("T%d", max+1), nil
}

// AppendEvent appends one JSONL event to the task log.
func (s Store) AppendEvent(id types.TaskID, event map[string]any) error {
	dir := s.TaskPath(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cli: task dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("cli: open task log: %w", err)
	}
	defer f.Close()
	buf, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("cli: marshal event: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%s\n", buf); err != nil {
		return fmt.Errorf("cli: write event: %w", err)
	}
	return f.Close()
}

// ReadEvents returns the task event stream in order.
func (s Store) ReadEvents(id types.TaskID) ([]map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(s.TaskPath(id), "events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cli: read task log: %w", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("cli: parse task log: %w", err)
		}
		out = append(out, ev)
	}
	return out, nil
}

// ResolveStore picks the task store for a command: the project store
// (<project>/.excalibur) when a project directory is given, else the
// user state directory. The daemon adopts the same directories, so
// tasks stay visible across no-daemon and daemon modes (3.6).
func ResolveStore(projectDir string) (Store, error) {
	if projectDir != "" {
		abs, err := filepath.Abs(projectDir)
		if err != nil {
			return Store{}, fmt.Errorf("cli: project dir: %w", err)
		}
		return Store{Base: filepath.Join(abs, ".excalibur")}, nil
	}
	base, err := userStateDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Base: base}, nil
}
